// Package core implements protocol-agnostic server-side logic for generic
// "aio"-style RPC services: dynamic method invocation, command execution and
// handle-based file access.
//
// Concrete services (for example the minirpc gRPC service in rpc/aio/minirpc)
// hold a *Core and add a thin adapter that translates between their wire
// messages and the request/response types defined here. New services should
// reuse Core instead of re-implementing this logic.
package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

// PayloadType identifies how invocation arguments and results are encoded.
// Values intentionally match the PayloadType enum of the minirpc proto, so
// adapters can convert with a plain cast.
type PayloadType int32

// Payload type values.
const (
	PayloadTypeUnspecified PayloadType = 0
	PayloadTypeJSON        PayloadType = 1
	PayloadTypeProtobuf    PayloadType = 2
	PayloadTypeBinary      PayloadType = 3
	PayloadTypeRaw         PayloadType = 4
)

// Code is a business error code reported through invocation responses.
// Values intentionally match the error codes used by the minirpc proto.
type Code int32

// Error codes for failed invocations.
const (
	// CodeOK indicates the invocation succeeded.
	CodeOK Code = 0
	// CodeInvalidArgument indicates the request is malformed.
	CodeInvalidArgument Code = 400
	// CodeMethodNotFound indicates the requested method is not registered.
	CodeMethodNotFound Code = 404
	// CodeTimeout indicates the invocation exceeded its deadline.
	CodeTimeout Code = 408
	// CodeCanceled indicates the invocation was canceled by the caller.
	CodeCanceled Code = 499
	// CodeInternal indicates the handler failed with an internal error.
	CodeInternal Code = 500
)

// InvokeHandler handles a dynamically registered method.
//
// argsType and args carry the request argument encoding and payload. The
// returned PayloadType declares how result is encoded.
//
// Handlers should respect the context deadline and cancellation. Invoke does
// not forcibly interrupt a handler after the deadline; it only reports the
// timeout through the response when the handler returns an error.
type InvokeHandler func(ctx context.Context, argsType PayloadType, args []byte) (resultType PayloadType, result []byte, err error)

// InvokeRequest describes one dynamic method invocation.
type InvokeRequest struct {
	Method    string
	ArgsType  PayloadType
	Args      []byte
	TimeoutMs uint64
}

// InvokeResult is the outcome of one dynamic method invocation. Code is
// CodeOK on success; ErrMessage describes the failure otherwise.
type InvokeResult struct {
	ResultType PayloadType
	Result     []byte
	Code       Code
	ErrMessage string
}

// Core bundles the shared state of an aio-style RPC server: registered
// methods, open file handles and command tasks.
//
// The zero value is not usable; create instances with New.
type Core struct {
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

// Option configures a Core.
type Option func(*Core)

// WithMaxTasks limits how many command tasks (including finished ones) the
// core keeps for status queries. When the limit is exceeded, the oldest
// finished tasks are evicted. Defaults to 1024.
func WithMaxTasks(n int) Option {
	return func(c *Core) {
		if n > 0 {
			c.maxTasks = n
		}
	}
}

// defaultMaxTasks is the default limit of command tasks kept by a Core.
const defaultMaxTasks = 1024

// New creates a Core ready for use.
func New(opts ...Option) *Core {
	c := &Core{
		handlers: make(map[string]InvokeHandler),
		files:    make(map[uint64]*os.File),
		tasks:    make(map[uint64]*cmdTask),
		maxTasks: defaultMaxTasks,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// RegisterMethod registers a method that can be invoked through Invoke.
// Registering a method with an existing name returns an error.
func (c *Core) RegisterMethod(name string, handler InvokeHandler) error {
	if name == "" {
		return errors.New("method name is empty")
	}
	if handler == nil {
		return errors.New("method handler is nil")
	}

	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()

	if _, ok := c.handlers[name]; ok {
		return fmt.Errorf("method %q is already registered", name)
	}
	c.handlers[name] = handler
	return nil
}

// Invoke executes a dynamically registered method. Business failures,
// including handler panics, are reported through InvokeResult.Code and
// InvokeResult.ErrMessage; Invoke itself never returns an error.
func (c *Core) Invoke(ctx context.Context, req *InvokeRequest) *InvokeResult {
	if req.Method == "" {
		return &InvokeResult{Code: CodeInvalidArgument, ErrMessage: "method name is empty"}
	}

	c.handlersMu.RLock()
	handler, ok := c.handlers[req.Method]
	c.handlersMu.RUnlock()
	if !ok {
		return &InvokeResult{Code: CodeMethodNotFound, ErrMessage: fmt.Sprintf("method %q is not registered", req.Method)}
	}

	callCtx := ctx
	if req.TimeoutMs > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, time.Duration(req.TimeoutMs)*time.Millisecond)
		defer cancel()
	}

	resultType, result, callErr := safeCall(handler, callCtx, req.ArgsType, req.Args)
	if callErr != nil {
		switch {
		case errors.Is(callErr, context.DeadlineExceeded):
			return &InvokeResult{Code: CodeTimeout, ErrMessage: callErr.Error()}
		case errors.Is(callErr, context.Canceled):
			return &InvokeResult{Code: CodeCanceled, ErrMessage: callErr.Error()}
		default:
			return &InvokeResult{Code: CodeInternal, ErrMessage: callErr.Error()}
		}
	}

	return &InvokeResult{ResultType: resultType, Result: result}
}

// safeCall invokes handler and converts a panic into an error.
func safeCall(handler InvokeHandler, ctx context.Context, argsType PayloadType, args []byte) (resultType PayloadType, result []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			resultType = PayloadTypeUnspecified
			result = nil
			err = fmt.Errorf("handler panicked: %v", r)
		}
	}()
	return handler(ctx, argsType, args)
}

// Close releases every resource held by the core: running command tasks are
// killed and all open file handles are closed.
//
// The core must not be used after Close. Calling Close more than once is
// safe.
func (c *Core) Close() error {
	c.tasksMu.Lock()
	tasks := make([]*cmdTask, 0, len(c.tasks))
	for _, t := range c.tasks {
		tasks = append(tasks, t)
	}
	c.tasks = make(map[uint64]*cmdTask)
	c.taskOrder = nil
	c.tasksMu.Unlock()

	for _, t := range tasks {
		t.markKilled()
		if t.isRunning() && t.cmd != nil && t.cmd.Process != nil {
			_ = t.cmd.Process.Kill()
		}
	}

	c.filesMu.Lock()
	files := c.files
	c.files = nil
	c.filesMu.Unlock()

	for _, f := range files {
		_ = f.Close()
	}
	return nil
}
