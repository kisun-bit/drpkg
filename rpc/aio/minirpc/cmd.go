package minirpc

import (
	"context"

	"github.com/kisun-bit/drpkg/rpc/aio/core"
	"github.com/kisun-bit/drpkg/rpc/aio/proto"
)

// ExecuteCmd runs a short-lived command synchronously and returns its output.
//
// When the command exits, the response reports success=true even for non-zero
// exit codes; success=false is reserved for start failures and timeouts.
//
// The Privileged field is accepted but not enforced; the command runs with
// the server process privileges.
func (s *Server) ExecuteCmd(ctx context.Context, req *proto.ExecuteRequest) (*proto.ExecuteResponse, error) {
	res, err := s.core.ExecuteCmd(ctx, &core.ExecuteRequest{
		Executable: req.GetExecutable(),
		Args:       req.GetArgs(),
		WorkDir:    req.GetWorkDir(),
		Env:        req.GetEnvironment(),
		TimeoutMs:  req.GetTimeoutMs(),
	})
	if err != nil {
		return nil, err
	}

	return &proto.ExecuteResponse{
		Success:      res.Started,
		ExitCode:     res.ExitCode,
		Stdout:       res.Stdout,
		Stderr:       res.Stderr,
		ErrorMessage: res.ErrMessage,
	}, nil
}

// StartCmdTask starts a long-running command in the background and returns a
// task id. Task state and output can be queried through GetStatusOfCmdTask
// until the task is evicted or the server is closed.
func (s *Server) StartCmdTask(ctx context.Context, req *proto.StartRequest) (*proto.StartResponse, error) {
	id, err := s.core.StartCmdTask(ctx, &core.StartTaskRequest{
		Executable: req.GetExecutable(),
		Args:       req.GetArgs(),
		WorkDir:    req.GetWorkDir(),
		Env:        req.GetEnvironment(),
	})
	if err != nil {
		return nil, err
	}
	return &proto.StartResponse{TaskId: id}, nil
}

// GetStatusOfCmdTask reports the status of a command task.
func (s *Server) GetStatusOfCmdTask(ctx context.Context, req *proto.GetStatusRequest) (*proto.GetStatusResponse, error) {
	state, err := s.core.GetCmdTaskState(ctx, req.GetTaskId())
	if err != nil {
		return nil, err
	}
	return &proto.GetStatusResponse{
		Status:   proto.GetStatusResponse_Status(state.Status),
		ExitCode: state.ExitCode,
		Stdout:   state.Stdout,
		Stderr:   state.Stderr,
	}, nil
}

// KillCmdTask terminates a running command task. Killing an already finished
// task succeeds without changing its recorded status.
func (s *Server) KillCmdTask(ctx context.Context, req *proto.KillRequest) (*proto.KillResponse, error) {
	if err := s.core.KillCmdTask(ctx, req.GetTaskId()); err != nil {
		return nil, err
	}
	return &proto.KillResponse{Success: true}, nil
}
