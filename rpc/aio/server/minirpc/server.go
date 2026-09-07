// Package minirpc implements the minirpc gRPC service (aio.GenericService)
// as a thin adapter over the protocol-agnostic core in rpc/aio/core.
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
//
// Future aio-style gRPC services should follow the same pattern: keep all
// logic in rpc/aio/core and add a small adapter package like this one.
package minirpc

import (
	"context"

	"github.com/kisun-bit/drpkg/rpc/aio/core"
	"github.com/kisun-bit/drpkg/rpc/aio/proto"
	"google.golang.org/grpc"
)

// Error codes reported through GenericResponse.error_code by Invoke. They are
// re-exported from core so callers do not need to import core directly.
const (
	// CodeOK indicates the invocation succeeded.
	CodeOK = int32(core.CodeOK)
	// CodeInvalidArgument indicates the request is malformed.
	CodeInvalidArgument = int32(core.CodeInvalidArgument)
	// CodeMethodNotFound indicates the requested method is not registered.
	CodeMethodNotFound = int32(core.CodeMethodNotFound)
	// CodeTimeout indicates the invocation exceeded its deadline.
	CodeTimeout = int32(core.CodeTimeout)
	// CodeCanceled indicates the invocation was canceled by the caller.
	CodeCanceled = int32(core.CodeCanceled)
	// CodeInternal indicates the handler failed with an internal error.
	CodeInternal = int32(core.CodeInternal)
)

// InvokeHandler handles a dynamically registered method.
//
// argsType and args carry the request argument encoding and payload. The
// returned PayloadType declares how result is encoded.
type InvokeHandler func(ctx context.Context, argsType proto.PayloadType, args []byte) (resultType proto.PayloadType, result []byte, err error)

// Server implements the aio.GenericService server API.
//
// The zero value is not usable; create instances with NewServer.
type Server struct {
	proto.UnimplementedGenericServiceServer

	core *core.Core

	// maxTasks is collected from options before the core is built.
	maxTasks int
}

// Option configures a Server.
type Option func(*Server)

// WithMaxTasks limits how many command tasks (including finished ones) the
// server keeps for status queries. When the limit is exceeded, the oldest
// finished tasks are evicted. Defaults to 1024.
func WithMaxTasks(n int) Option {
	return func(s *Server) {
		s.maxTasks = n
	}
}

// NewServer creates a minirpc service implementation.
func NewServer(opts ...Option) *Server {
	s := &Server{}
	for _, opt := range opts {
		opt(s)
	}

	coreOpts := []core.Option(nil)
	if s.maxTasks > 0 {
		coreOpts = append(coreOpts, core.WithMaxTasks(s.maxTasks))
	}
	s.core = core.New(coreOpts...)
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
	return s.core.RegisterMethod(name, func(ctx context.Context, argsType core.PayloadType, args []byte) (core.PayloadType, []byte, error) {
		rt, result, err := handler(ctx, proto.PayloadType(argsType), args)
		return core.PayloadType(rt), result, err
	})
}

// Invoke executes a dynamically registered method.
//
// Business failures are reported through the GenericResponse success /
// error_code / error_message fields; the returned gRPC error is nil unless
// the request itself cannot be processed.
func (s *Server) Invoke(ctx context.Context, req *proto.GenericRequest) (*proto.GenericResponse, error) {
	res := s.core.Invoke(ctx, &core.InvokeRequest{
		Method:    req.GetMethod(),
		ArgsType:  core.PayloadType(req.GetArgsType()),
		Args:      req.GetArgs(),
		TimeoutMs: req.GetTimeoutMs(),
	})

	resp := &proto.GenericResponse{RequestId: req.GetRequestId()}
	if res.Code == core.CodeOK {
		resp.Success = true
		resp.ErrorCode = CodeOK
		resp.ReturnType = proto.PayloadType(res.ResultType)
		resp.Result = res.Result
	} else {
		resp.Success = false
		resp.ErrorCode = int32(res.Code)
		resp.ErrorMessage = res.ErrMessage
	}
	return resp, nil
}

// Close releases every resource held by the server: running command tasks are
// killed and all open file handles are closed.
//
// The server must not be used after Close. Calling Close more than once is
// safe.
func (s *Server) Close() error {
	return s.core.Close()
}
