package core

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
)

// TaskStatus is the lifecycle state of a command task. Values intentionally
// match the GetStatusResponse.Status enum of the minirpc proto, so adapters
// can convert with a plain cast.
type TaskStatus int32

// Command task states.
const (
	TaskStatusUnspecified TaskStatus = 0
	TaskStatusRunning     TaskStatus = 1
	TaskStatusFinished    TaskStatus = 2
	TaskStatusFailed      TaskStatus = 3
	TaskStatusKilled      TaskStatus = 4
)

// ExecuteRequest describes one synchronous command execution.
type ExecuteRequest struct {
	Executable string
	Args       []string
	WorkDir    string
	Env        map[string]string
	TimeoutMs  uint64
}

// ExecuteResult reports the outcome of a synchronous command execution.
//
// When the command exits, Started is true even for non-zero exit codes;
// Started=false is reserved for start failures and timeouts.
type ExecuteResult struct {
	Started    bool
	ExitCode   int32
	Stdout     []byte
	Stderr     []byte
	ErrMessage string
}

// StartTaskRequest describes one background command task.
type StartTaskRequest struct {
	Executable string
	Args       []string
	WorkDir    string
	Env        map[string]string
}

// TaskState is a snapshot of one command task.
type TaskState struct {
	Status   TaskStatus
	ExitCode int32
	Stdout   []byte
	Stderr   []byte
}

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

// status maps the internal task state to a TaskStatus.
func (t *cmdTask) status() TaskStatus {
	if t.isRunning() {
		return TaskStatusRunning
	}
	if t.wasKilled() {
		return TaskStatusKilled
	}
	if t.waitErr != nil {
		return TaskStatusFailed
	}
	return TaskStatusFinished
}

// ExecuteCmd runs a short-lived command synchronously and returns its output.
//
// The Privileged concept is not modeled here; the command runs with the
// server process privileges.
func (c *Core) ExecuteCmd(ctx context.Context, req *ExecuteRequest) (*ExecuteResult, error) {
	if req.Executable == "" {
		return &ExecuteResult{ErrMessage: "executable is empty"}, nil
	}

	runCtx := ctx
	if req.TimeoutMs > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(req.TimeoutMs)*time.Millisecond)
		defer cancel()
	}

	cmd, err := newCommand(runCtx, req.Executable, req.Args, req.WorkDir, req.Env)
	if err != nil {
		return &ExecuteResult{ErrMessage: err.Error()}, nil
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err = cmd.Start(); err != nil {
		return &ExecuteResult{ErrMessage: fmt.Sprintf("start command: %v", err)}, nil
	}

	res := &ExecuteResult{
		Started:  true,
		ExitCode: int32(waitCmd(cmd)),
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
	}

	if runCtx.Err() == context.DeadlineExceeded {
		res.Started = false
		res.ErrMessage = "command timed out"
	}

	return res, nil
}

// StartCmdTask starts a long-running command in the background and returns a
// task id. Task state and output can be queried through GetCmdTaskState
// until the task is evicted or the core is closed.
//
// The command is not bound to the request context, so it keeps running after
// the caller returns.
func (c *Core) StartCmdTask(ctx context.Context, req *StartTaskRequest) (uint64, error) {
	if req.Executable == "" {
		return 0, errors.New("executable is empty")
	}

	cmd, err := newCommand(context.Background(), req.Executable, req.Args, req.WorkDir, req.Env)
	if err != nil {
		return 0, err
	}

	task := newCmdTask()
	cmd.Stdout = &syncWriter{buf: task.stdout, mu: &task.bufMu}
	cmd.Stderr = &syncWriter{buf: task.stderr, mu: &task.bufMu}
	task.cmd = cmd

	if err = cmd.Start(); err != nil {
		return 0, fmt.Errorf("start command: %v", err)
	}

	id := c.addTask(task)

	go func() {
		err := cmd.Wait()
		task.waitErr = err
		task.exit = int32(cmd.ProcessState.ExitCode())
		close(task.done)
	}()

	return id, nil
}

// GetCmdTaskState reports the current state of a command task. It returns an
// error if the task is unknown.
func (c *Core) GetCmdTaskState(ctx context.Context, id uint64) (*TaskState, error) {
	task, ok := c.lookupTask(id)
	if !ok {
		return nil, fmt.Errorf("task %d not found", id)
	}

	stdout, stderr := task.snapshot()
	return &TaskState{
		Status:   task.status(),
		ExitCode: atomic.LoadInt32(&task.exit),
		Stdout:   stdout,
		Stderr:   stderr,
	}, nil
}

// KillCmdTask terminates a running command task. Killing an already finished
// task succeeds without changing its recorded status. It returns an error if
// the task is unknown.
func (c *Core) KillCmdTask(ctx context.Context, id uint64) error {
	task, ok := c.lookupTask(id)
	if !ok {
		return fmt.Errorf("task %d not found", id)
	}

	task.markKilled()
	if task.isRunning() && task.cmd != nil && task.cmd.Process != nil {
		if err := task.cmd.Process.Kill(); err != nil {
			return fmt.Errorf("kill process of task %d: %v", id, err)
		}
	}

	return nil
}

// newCommand builds an exec.Cmd with the given working directory and extra
// environment entries.
func newCommand(ctx context.Context, executable string, args []string, workDir string, env map[string]string) (*exec.Cmd, error) {
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
func (c *Core) addTask(task *cmdTask) uint64 {
	c.tasksMu.Lock()
	defer c.tasksMu.Unlock()

	c.nextTask++
	id := c.nextTask
	c.tasks[id] = task
	c.taskOrder = append(c.taskOrder, id)

	// Evict the oldest finished tasks while the limit is exceeded. Running
	// tasks are never evicted, so the limit may be exceeded temporarily.
	for len(c.tasks) > c.maxTasks && len(c.taskOrder) > 1 {
		oldest := c.taskOrder[0]
		t := c.tasks[oldest]
		if t == nil || t.isRunning() {
			break
		}
		delete(c.tasks, oldest)
		c.taskOrder = c.taskOrder[1:]
	}

	return id
}

func (c *Core) lookupTask(id uint64) (*cmdTask, bool) {
	c.tasksMu.Lock()
	defer c.tasksMu.Unlock()
	t, ok := c.tasks[id]
	return t, ok
}

// syncWriter serializes writes into a shared buffer so cmd.Wait never races
// with output snapshots taken by GetCmdTaskState.
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
