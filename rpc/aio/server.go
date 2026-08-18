// Package aio implements the server side of the minirpc generic RPC service
// (aio.GenericService).
//
// The service provides three groups of capabilities:
//
//   - Dynamic method invocation (Invoke): business methods can be registered
//     at runtime via RegisterMethod and invoked by name.
//   - Command execution (ExecuteCmd / StartCmdTask / GetStatusOfCmdTask /
//     KillCmdTask): synchronous execution of short-lived commands and full
//     lifecycle management of long-running command tasks.
//   - File access (OpenFile / ReadFile / WriteFile / CloseFile): handle-based
//     random read/write of files on the server host.
//
// Note: this service exposes command execution and arbitrary file access.
// Production deployments must protect it with transport security and access
// control (for example TLS plus an authentication interceptor).
package aio

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/kisun-bit/drpkg/rpc/aio/proto"
	"google.golang.org/grpc"
)

// defaultMaxTasks is the default limit of command tasks kept by the server.
const defaultMaxTasks = 1024

// Error codes reported through GenericResponse.error_code by Invoke.
const (
	// CodeOK indicates the invocation succeeded.
	CodeOK int32 = 0
	// CodeInvalidArgument indicates the request is malformed.
	CodeInvalidArgument int32 = 400
	// CodeMethodNotFound indicates the requested method is not registered.
	CodeMethodNotFound int32 = 404
	// CodeTimeout indicates the invocation exceeded its deadline.
	CodeTimeout int32 = 408
	// CodeCanceled indicates the invocation was canceled by the caller.
	CodeCanceled int32 = 499
	// CodeInternal indicates the handler failed with an internal error.
	CodeInternal int32 = 500
)

// InvokeHandler handles a dynamically registered method.
//
// argsType and args carry the request argument encoding and payload. The
// returned PayloadType declares how result is encoded.
//
// Handlers should respect the context deadline and cancellation. Invoke does
// not forcibly interrupt a handler after the deadline; it only reports the
// timeout through the response when the handler returns an error.
type InvokeHandler func(ctx context.Context, argsType proto.PayloadType, args []byte) (resultType proto.PayloadType, result []byte, err error)

// Server implements the aio.GenericService server API.
//
// The zero value is not usable; create instances with NewServer.
type Server struct {
	proto.UnimplementedGenericServiceServer

	handlersMu sync.RWMutex
	handlers   map[string]InvokeHandler

	filesMu    sync.Mutex
	files      map[uint64]*os.File
	nextHandle uint64

	tasksMu   sync.Mutex
	tasks     map[uint64]*cmdTask
	taskOrder []uint64
	nextTask  uint64

	maxTasks int
}

// Option configures a Server.
type Option func(*Server)

// WithMaxTasks limits how many command tasks (including finished ones) the
// server keeps for status queries. When the limit is exceeded, the oldest
// finished tasks are evicted. Defaults to 1024.
func WithMaxTasks(n int) Option {
	return func(s *Server) {
		if n > 0 {
			s.maxTasks = n
		}
	}
}

// NewServer creates a minirpc service implementation.
func NewServer(opts ...Option) *Server {
	s := &Server{
		handlers: make(map[string]InvokeHandler),
		files:    make(map[uint64]*os.File),
		tasks:    make(map[uint64]*cmdTask),
		maxTasks: defaultMaxTasks,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// NewGRPCServer creates a grpc.Server with the given minirpc server
// registered, ready to be served on a listener.
func NewGRPCServer(srv *Server, opts ...grpc.ServerOption) *grpc.Server {
	gs := grpc.NewServer(opts...)
	proto.RegisterGenericServiceServer(gs, srv)
	return gs
}

// RegisterMethod registers a method that can be invoked through Invoke.
// Registering a method with an existing name returns an error.
func (s *Server) RegisterMethod(name string, handler InvokeHandler) error {
	if name == "" {
		return errors.New("method name is empty")
	}
	if handler == nil {
		return errors.New("method handler is nil")
	}

	s.handlersMu.Lock()
	defer s.handlersMu.Unlock()

	if _, ok := s.handlers[name]; ok {
		return fmt.Errorf("method %q is already registered", name)
	}
	s.handlers[name] = handler
	return nil
}

// Invoke executes a dynamically registered method.
//
// Business failures are reported through the GenericResponse success /
// error_code / error_message fields; the returned gRPC error is nil unless
// the request itself cannot be processed.
func (s *Server) Invoke(ctx context.Context, req *proto.GenericRequest) (resp *proto.GenericResponse, err error) {
	resp = &proto.GenericResponse{RequestId: req.GetRequestId()}

	defer func() {
		if r := recover(); r != nil {
			resp.Success = false
			resp.ErrorCode = CodeInternal
			resp.ErrorMessage = fmt.Sprintf("handler panicked: %v", r)
			resp.Result = nil
			err = nil
		}
	}()

	name := req.GetMethod()
	if name == "" {
		return failInvoke(resp, CodeInvalidArgument, "method name is empty"), nil
	}

	s.handlersMu.RLock()
	handler, ok := s.handlers[name]
	s.handlersMu.RUnlock()
	if !ok {
		return failInvoke(resp, CodeMethodNotFound, fmt.Sprintf("method %q is not registered", name)), nil
	}

	callCtx := ctx
	if ms := req.GetTimeoutMs(); ms > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, time.Duration(ms)*time.Millisecond)
		defer cancel()
	}

	resultType, result, callErr := handler(callCtx, req.GetArgsType(), req.GetArgs())
	if callErr != nil {
		switch {
		case errors.Is(callErr, context.DeadlineExceeded):
			return failInvoke(resp, CodeTimeout, callErr.Error()), nil
		case errors.Is(callErr, context.Canceled):
			return failInvoke(resp, CodeCanceled, callErr.Error()), nil
		default:
			return failInvoke(resp, CodeInternal, callErr.Error()), nil
		}
	}

	resp.Success = true
	resp.ErrorCode = CodeOK
	resp.ReturnType = resultType
	resp.Result = result
	return resp, nil
}

// Close releases every resource held by the server: running command tasks are
// killed and all open file handles are closed.
//
// The server must not be used after Close. Calling Close more than once is
// safe.
func (s *Server) Close() error {
	s.tasksMu.Lock()
	tasks := make([]*cmdTask, 0, len(s.tasks))
	for _, t := range s.tasks {
		tasks = append(tasks, t)
	}
	s.tasks = make(map[uint64]*cmdTask)
	s.taskOrder = nil
	s.tasksMu.Unlock()

	for _, t := range tasks {
		t.markKilled()
		if t.isRunning() && t.cmd != nil && t.cmd.Process != nil {
			_ = t.cmd.Process.Kill()
		}
	}

	s.filesMu.Lock()
	files := s.files
	s.files = nil
	s.filesMu.Unlock()

	for _, f := range files {
		_ = f.Close()
	}
	return nil
}

func failInvoke(resp *proto.GenericResponse, code int32, message string) *proto.GenericResponse {
	resp.Success = false
	resp.ErrorCode = code
	resp.ErrorMessage = message
	return resp
}
