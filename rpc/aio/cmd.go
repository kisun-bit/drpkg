package aio

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kisun-bit/drpkg/rpc/aio/proto"
)

// cmdTask tracks the lifecycle of one long-running command started by
// StartCmdTask.
type cmdTask struct {
	cmd     *exec.Cmd
	stdout  *bytes.Buffer
	stderr  *bytes.Buffer
	bufMu   sync.Mutex
	done    chan struct{}
	exit    int32
	killed  int32
	waitErr error
}

func newCmdTask() *cmdTask {
	return &cmdTask{
		stdout: new(bytes.Buffer),
		stderr: new(bytes.Buffer),
		done:   make(chan struct{}),
	}
}

// snapshot copies the current output buffers.
func (t *cmdTask) snapshot() (stdout, stderr []byte) {
	t.bufMu.Lock()
	defer t.bufMu.Unlock()
	return append([]byte(nil), t.stdout.Bytes()...), append([]byte(nil), t.stderr.Bytes()...)
}

func (t *cmdTask) isRunning() bool {
	select {
	case <-t.done:
		return false
	default:
		return true
	}
}

func (t *cmdTask) markKilled() {
	atomic.StoreInt32(&t.killed, 1)
}

func (t *cmdTask) wasKilled() bool {
	return atomic.LoadInt32(&t.killed) == 1
}

// status maps the internal task state to the proto status enum.
func (t *cmdTask) status() proto.GetStatusResponse_Status {
	if t.isRunning() {
		return proto.GetStatusResponse_RUNNING
	}
	if t.wasKilled() {
		return proto.GetStatusResponse_KILLED
	}
	if t.waitErr != nil {
		return proto.GetStatusResponse_FAILED
	}
	return proto.GetStatusResponse_FINISHED
}

// ExecuteCmd runs a short-lived command synchronously and returns its output.
//
// When the command exits, the response reports success=true even for non-zero
// exit codes; success=false is reserved for start failures and timeouts.
//
// The Privileged field is accepted but not enforced; the command runs with
// the server process privileges.
func (s *Server) ExecuteCmd(ctx context.Context, req *proto.ExecuteRequest) (*proto.ExecuteResponse, error) {
	resp := new(proto.ExecuteResponse)

	if req.GetExecutable() == "" {
		resp.ErrorMessage = "executable is empty"
		return resp, nil
	}

	runCtx := ctx
	if ms := req.GetTimeoutMs(); ms > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(ms)*time.Millisecond)
		defer cancel()
	}

	cmd, err := s.newCommand(runCtx, req.GetExecutable(), req.GetArgs(), req.GetWorkDir(), req.GetEnvironment())
	if err != nil {
		resp.ErrorMessage = err.Error()
		return resp, nil
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err = cmd.Start(); err != nil {
		resp.ErrorMessage = fmt.Sprintf("start command: %v", err)
		return resp, nil
	}

	resp.ExitCode = int32(waitCmd(cmd))
	resp.Stdout = stdout.Bytes()
	resp.Stderr = stderr.Bytes()
	resp.Success = true

	if runCtx.Err() == context.DeadlineExceeded {
		resp.Success = false
		resp.ErrorMessage = "command timed out"
	}

	return resp, nil
}

// StartCmdTask starts a long-running command in the background and returns a
// task id. Task state and output can be queried through GetStatusOfCmdTask
// until the task is evicted or the server is closed.
func (s *Server) StartCmdTask(ctx context.Context, req *proto.StartRequest) (*proto.StartResponse, error) {
	if req.GetExecutable() == "" {
		return nil, errors.New("executable is empty")
	}

	// The command must keep running after this RPC returns, so it is not bound
	// to the request context.
	cmd, err := s.newCommand(context.Background(), req.GetExecutable(), req.GetArgs(), req.GetWorkDir(), req.GetEnvironment())
	if err != nil {
		return nil, err
	}

	task := newCmdTask()
	cmd.Stdout = &syncWriter{buf: task.stdout, mu: &task.bufMu}
	cmd.Stderr = &syncWriter{buf: task.stderr, mu: &task.bufMu}
	task.cmd = cmd

	if err = cmd.Start(); err != nil {
		return nil, fmt.Errorf("start command: %v", err)
	}

	id := s.addTask(task)

	go func() {
		err := cmd.Wait()
		task.waitErr = err
		task.exit = int32(cmd.ProcessState.ExitCode())
		close(task.done)
	}()

	return &proto.StartResponse{TaskId: id}, nil
}

// GetStatusOfCmdTask reports the status of a command task.
func (s *Server) GetStatusOfCmdTask(ctx context.Context, req *proto.GetStatusRequest) (*proto.GetStatusResponse, error) {
	task, ok := s.lookupTask(req.GetTaskId())
	if !ok {
		return nil, fmt.Errorf("task %d not found", req.GetTaskId())
	}

	stdout, stderr := task.snapshot()
	return &proto.GetStatusResponse{
		Status:   task.status(),
		ExitCode: atomic.LoadInt32(&task.exit),
		Stdout:   stdout,
		Stderr:   stderr,
	}, nil
}

// KillCmdTask terminates a running command task. Killing an already finished
// task succeeds without changing its recorded status.
func (s *Server) KillCmdTask(ctx context.Context, req *proto.KillRequest) (*proto.KillResponse, error) {
	task, ok := s.lookupTask(req.GetTaskId())
	if !ok {
		return nil, fmt.Errorf("task %d not found", req.GetTaskId())
	}

	task.markKilled()
	if task.isRunning() && task.cmd != nil && task.cmd.Process != nil {
		if err := task.cmd.Process.Kill(); err != nil {
			return nil, fmt.Errorf("kill process of task %d: %v", req.GetTaskId(), err)
		}
	}

	return &proto.KillResponse{Success: true}, nil
}

// newCommand builds an exec.Cmd with the given working directory and extra
// environment entries.
func (s *Server) newCommand(ctx context.Context, executable string, args []string, workDir string, env map[string]string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, executable, args...)

	if workDir != "" {
		info, err := os.Stat(workDir)
		if err != nil {
			return nil, fmt.Errorf("stat work dir %q: %v", workDir, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("work dir %q is not a directory", workDir)
		}
		cmd.Dir = workDir
	}

	if len(env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	return cmd, nil
}

// addTask stores a started task and evicts the oldest finished tasks once the
// task limit is exceeded.
func (s *Server) addTask(task *cmdTask) uint64 {
	s.tasksMu.Lock()
	defer s.tasksMu.Unlock()

	s.nextTask++
	id := s.nextTask
	s.tasks[id] = task
	s.taskOrder = append(s.taskOrder, id)

	// Evict the oldest finished tasks while the limit is exceeded. Running
	// tasks are never evicted, so the limit may be exceeded temporarily.
	for len(s.tasks) > s.maxTasks && len(s.taskOrder) > 1 {
		oldest := s.taskOrder[0]
		t := s.tasks[oldest]
		if t == nil || t.isRunning() {
			break
		}
		delete(s.tasks, oldest)
		s.taskOrder = s.taskOrder[1:]
	}

	return id
}

func (s *Server) lookupTask(id uint64) (*cmdTask, bool) {
	s.tasksMu.Lock()
	defer s.tasksMu.Unlock()
	t, ok := s.tasks[id]
	return t, ok
}

// syncWriter serializes writes into a shared buffer so cmd.Wait never races
// with output snapshots taken by GetStatusOfCmdTask.
type syncWriter struct {
	buf *bytes.Buffer
	mu  *sync.Mutex
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

// waitCmd waits for the command and derives its exit code.
func waitCmd(cmd *exec.Cmd) int {
	if err := cmd.Wait(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		return -1
	}
	return 0
}
