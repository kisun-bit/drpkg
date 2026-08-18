package aio

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/kisun-bit/drpkg/rpc/aio/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// echoShell returns a short command that prints "hello" to stdout on the
// current platform.
func echoShell() (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd.exe", []string{"/c", "echo hello"}
	}
	return "/bin/sh", []string{"-c", "echo hello"}
}

// sleepShell returns a command that sleeps for the given seconds.
func sleepShell(seconds int) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd.exe", []string{"/c", "ping", "-n", itoa(seconds + 1), "127.0.0.1", ">", "nul"}
	}
	return "/bin/sh", []string{"-c", "sleep " + itoa(seconds)}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestInvoke_Success(t *testing.T) {
	s := NewServer()
	defer s.Close()

	err := s.RegisterMethod("test.echo", func(ctx context.Context, argsType proto.PayloadType, args []byte) (proto.PayloadType, []byte, error) {
		return argsType, args, nil
	})
	if err != nil {
		t.Fatalf("RegisterMethod: %v", err)
	}

	resp, err := s.Invoke(context.Background(), &proto.GenericRequest{
		RequestId: "req-1",
		Method:    "test.echo",
		ArgsType:  proto.PayloadType_RAW,
		Args:      []byte("payload"),
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !resp.GetSuccess() {
		t.Fatalf("expected success, got error: %s", resp.GetErrorMessage())
	}
	if resp.GetRequestId() != "req-1" {
		t.Fatalf("request id = %q, want req-1", resp.GetRequestId())
	}
	if string(resp.GetResult()) != "payload" {
		t.Fatalf("result = %q, want payload", string(resp.GetResult()))
	}
}

func TestInvoke_MethodNotFound(t *testing.T) {
	s := NewServer()
	defer s.Close()

	resp, err := s.Invoke(context.Background(), &proto.GenericRequest{Method: "no.such.method"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.GetSuccess() || resp.GetErrorCode() != CodeMethodNotFound {
		t.Fatalf("expected CodeMethodNotFound, got success=%v code=%d", resp.GetSuccess(), resp.GetErrorCode())
	}
}

func TestInvoke_EmptyMethod(t *testing.T) {
	s := NewServer()
	defer s.Close()

	resp, err := s.Invoke(context.Background(), &proto.GenericRequest{})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.GetSuccess() || resp.GetErrorCode() != CodeInvalidArgument {
		t.Fatalf("expected CodeInvalidArgument, got success=%v code=%d", resp.GetSuccess(), resp.GetErrorCode())
	}
}

func TestInvoke_HandlerError(t *testing.T) {
	s := NewServer()
	defer s.Close()

	_ = s.RegisterMethod("test.fail", func(ctx context.Context, argsType proto.PayloadType, args []byte) (proto.PayloadType, []byte, error) {
		return 0, nil, errors.New("boom")
	})

	resp, err := s.Invoke(context.Background(), &proto.GenericRequest{Method: "test.fail"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.GetSuccess() || resp.GetErrorCode() != CodeInternal || resp.GetErrorMessage() != "boom" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestInvoke_Timeout(t *testing.T) {
	s := NewServer()
	defer s.Close()

	_ = s.RegisterMethod("test.block", func(ctx context.Context, argsType proto.PayloadType, args []byte) (proto.PayloadType, []byte, error) {
		<-ctx.Done()
		return 0, nil, ctx.Err()
	})

	resp, err := s.Invoke(context.Background(), &proto.GenericRequest{
		Method:    "test.block",
		TimeoutMs: 50,
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.GetSuccess() || resp.GetErrorCode() != CodeTimeout {
		t.Fatalf("expected CodeTimeout, got success=%v code=%d", resp.GetSuccess(), resp.GetErrorCode())
	}
}

func TestInvoke_PanicRecovered(t *testing.T) {
	s := NewServer()
	defer s.Close()

	_ = s.RegisterMethod("test.panic", func(ctx context.Context, argsType proto.PayloadType, args []byte) (proto.PayloadType, []byte, error) {
		panic("test panic")
	})

	resp, err := s.Invoke(context.Background(), &proto.GenericRequest{Method: "test.panic"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.GetSuccess() || resp.GetErrorCode() != CodeInternal {
		t.Fatalf("expected CodeInternal, got success=%v code=%d", resp.GetSuccess(), resp.GetErrorCode())
	}
}

func TestRegisterMethod_Duplicate(t *testing.T) {
	s := NewServer()
	defer s.Close()

	h := func(ctx context.Context, argsType proto.PayloadType, args []byte) (proto.PayloadType, []byte, error) {
		return proto.PayloadType_RAW, nil, nil
	}
	if err := s.RegisterMethod("dup", h); err != nil {
		t.Fatalf("RegisterMethod: %v", err)
	}
	if err := s.RegisterMethod("dup", h); err == nil {
		t.Fatal("expected error on duplicate registration")
	}
}

func TestExecuteCmd_Success(t *testing.T) {
	s := NewServer()
	defer s.Close()

	exe, args := echoShell()
	resp, err := s.ExecuteCmd(context.Background(), &proto.ExecuteRequest{
		Executable: exe,
		Args:       args,
		TimeoutMs:  10000,
	})
	if err != nil {
		t.Fatalf("ExecuteCmd: %v", err)
	}
	if !resp.GetSuccess() {
		t.Fatalf("expected success, message: %s", resp.GetErrorMessage())
	}
	if resp.GetExitCode() != 0 {
		t.Fatalf("exit code = %d, want 0", resp.GetExitCode())
	}
	if len(resp.GetStdout()) == 0 {
		t.Fatal("expected non-empty stdout")
	}
}

func TestExecuteCmd_Timeout(t *testing.T) {
	s := NewServer()
	defer s.Close()

	exe, args := sleepShell(5)
	resp, err := s.ExecuteCmd(context.Background(), &proto.ExecuteRequest{
		Executable: exe,
		Args:       args,
		TimeoutMs:  300,
	})
	if err != nil {
		t.Fatalf("ExecuteCmd: %v", err)
	}
	if resp.GetSuccess() {
		t.Fatal("expected timeout failure")
	}
}

func TestExecuteCmd_EmptyExecutable(t *testing.T) {
	s := NewServer()
	defer s.Close()

	resp, err := s.ExecuteCmd(context.Background(), &proto.ExecuteRequest{})
	if err != nil {
		t.Fatalf("ExecuteCmd: %v", err)
	}
	if resp.GetSuccess() || resp.GetErrorMessage() == "" {
		t.Fatalf("expected failure with message, got %+v", resp)
	}
}

func TestCmdTask_Lifecycle(t *testing.T) {
	s := NewServer()
	defer s.Close()

	exe, args := echoShell()
	startResp, err := s.StartCmdTask(context.Background(), &proto.StartRequest{
		Executable: exe,
		Args:       args,
	})
	if err != nil {
		t.Fatalf("StartCmdTask: %v", err)
	}
	if startResp.GetTaskId() == 0 {
		t.Fatal("expected non-zero task id")
	}

	// Wait for completion.
	deadline := time.Now().Add(10 * time.Second)
	var statusResp *proto.GetStatusResponse
	for time.Now().Before(deadline) {
		statusResp, err = s.GetStatusOfCmdTask(context.Background(), &proto.GetStatusRequest{TaskId: startResp.GetTaskId()})
		if err != nil {
			t.Fatalf("GetStatusOfCmdTask: %v", err)
		}
		if statusResp.GetStatus() != proto.GetStatusResponse_RUNNING {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if statusResp.GetStatus() != proto.GetStatusResponse_FINISHED {
		t.Fatalf("status = %v, want FINISHED", statusResp.GetStatus())
	}
	if statusResp.GetExitCode() != 0 {
		t.Fatalf("exit code = %d, want 0", statusResp.GetExitCode())
	}
	if len(statusResp.GetStdout()) == 0 {
		t.Fatal("expected non-empty stdout")
	}
}

func TestCmdTask_Kill(t *testing.T) {
	s := NewServer()
	defer s.Close()

	exe, args := sleepShell(30)
	startResp, err := s.StartCmdTask(context.Background(), &proto.StartRequest{
		Executable: exe,
		Args:       args,
	})
	if err != nil {
		t.Fatalf("StartCmdTask: %v", err)
	}

	killResp, err := s.KillCmdTask(context.Background(), &proto.KillRequest{TaskId: startResp.GetTaskId()})
	if err != nil {
		t.Fatalf("KillCmdTask: %v", err)
	}
	if !killResp.GetSuccess() {
		t.Fatal("expected kill success")
	}

	// Wait for the task to observe the kill.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		statusResp, err := s.GetStatusOfCmdTask(context.Background(), &proto.GetStatusRequest{TaskId: startResp.GetTaskId()})
		if err != nil {
			t.Fatalf("GetStatusOfCmdTask: %v", err)
		}
		if statusResp.GetStatus() == proto.GetStatusResponse_KILLED {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("task did not reach KILLED state")
}

func TestCmdTask_NotFound(t *testing.T) {
	s := NewServer()
	defer s.Close()

	if _, err := s.GetStatusOfCmdTask(context.Background(), &proto.GetStatusRequest{TaskId: 999}); err == nil {
		t.Fatal("expected error for unknown task")
	}
	if _, err := s.KillCmdTask(context.Background(), &proto.KillRequest{TaskId: 999}); err == nil {
		t.Fatal("expected error for unknown task")
	}
}

func TestFile_ReadWrite(t *testing.T) {
	s := NewServer()
	defer s.Close()

	path := filepath.Join(t.TempDir(), "test.bin")

	// Create and write.
	openResp, err := s.OpenFile(context.Background(), &proto.OpenFileRequest{
		Path: path,
		Mode: proto.OpenFileRequest_WRITE_ONLY,
	})
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}

	writeResp, err := s.WriteFile(context.Background(), &proto.WriteFileRequest{
		Handle: openResp.GetHandle(),
		Offset: 0,
		Data:   []byte("hello world"),
	})
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if writeResp.GetSize() != 11 {
		t.Fatalf("written size = %d, want 11", writeResp.GetSize())
	}

	closeResp, err := s.CloseFile(context.Background(), &proto.CloseFileRequest{Handle: openResp.GetHandle()})
	if err != nil {
		t.Fatalf("CloseFile: %v", err)
	}
	if !closeResp.GetSuccess() {
		t.Fatal("expected close success")
	}

	// Reopen read-only and verify content.
	openResp, err = s.OpenFile(context.Background(), &proto.OpenFileRequest{
		Path: path,
		Mode: proto.OpenFileRequest_READ_ONLY,
	})
	if err != nil {
		t.Fatalf("OpenFile read-only: %v", err)
	}

	readResp, err := s.ReadFile(context.Background(), &proto.ReadFileRequest{
		Handle: openResp.GetHandle(),
		Offset: 0,
		Size:   64,
	})
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(readResp.GetData()) != "hello world" {
		t.Fatalf("data = %q, want %q", string(readResp.GetData()), "hello world")
	}
	if !readResp.GetEof() {
		t.Fatal("expected eof")
	}

	// Read at a middle offset without reaching the end.
	readResp, err = s.ReadFile(context.Background(), &proto.ReadFileRequest{
		Handle: openResp.GetHandle(),
		Offset: 0,
		Size:   5,
	})
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(readResp.GetData()) != "hello" || readResp.GetEof() {
		t.Fatalf("data = %q eof = %v, want hello / false", string(readResp.GetData()), readResp.GetEof())
	}

	// Read past the end.
	readResp, err = s.ReadFile(context.Background(), &proto.ReadFileRequest{
		Handle: openResp.GetHandle(),
		Offset: 1000,
		Size:   16,
	})
	if err != nil {
		t.Fatalf("ReadFile past end: %v", err)
	}
	if len(readResp.GetData()) != 0 || !readResp.GetEof() {
		t.Fatalf("expected empty data with eof, got %d bytes eof=%v", len(readResp.GetData()), readResp.GetEof())
	}

	if _, err := s.CloseFile(context.Background(), &proto.CloseFileRequest{Handle: openResp.GetHandle()}); err != nil {
		t.Fatalf("CloseFile: %v", err)
	}

	// Closing again must fail.
	if _, err := s.CloseFile(context.Background(), &proto.CloseFileRequest{Handle: openResp.GetHandle()}); err == nil {
		t.Fatal("expected error when closing an already closed handle")
	}
}

func TestFile_ReadOnlyMissingFile(t *testing.T) {
	s := NewServer()
	defer s.Close()

	path := filepath.Join(t.TempDir(), "missing.bin")
	if _, err := s.OpenFile(context.Background(), &proto.OpenFileRequest{
		Path: path,
		Mode: proto.OpenFileRequest_READ_ONLY,
	}); err == nil {
		t.Fatal("expected error opening a missing file read-only")
	}
}

func TestFile_InvalidHandle(t *testing.T) {
	s := NewServer()
	defer s.Close()

	if _, err := s.ReadFile(context.Background(), &proto.ReadFileRequest{Handle: 42, Size: 1}); err == nil {
		t.Fatal("expected error for invalid handle")
	}
	if _, err := s.WriteFile(context.Background(), &proto.WriteFileRequest{Handle: 42, Data: []byte("x")}); err == nil {
		t.Fatal("expected error for invalid handle")
	}
}

func TestServer_Close_ReleasesResources(t *testing.T) {
	s := NewServer()

	path := filepath.Join(t.TempDir(), "held.bin")
	openResp, err := s.OpenFile(context.Background(), &proto.OpenFileRequest{
		Path: path,
		Mode: proto.OpenFileRequest_WRITE_ONLY,
	})
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// All handles must be gone after Close.
	if _, err := s.ReadFile(context.Background(), &proto.ReadFileRequest{Handle: openResp.GetHandle(), Size: 1}); err == nil {
		t.Fatal("expected error after server close")
	}
	if _, err := s.OpenFile(context.Background(), &proto.OpenFileRequest{Path: path, Mode: proto.OpenFileRequest_READ_ONLY}); err == nil {
		t.Fatal("expected error opening file on a closed server")
	}
}

// TestGRPC_EndToEnd starts an in-memory gRPC server and drives the service
// through the generated client.
func TestGRPC_EndToEnd(t *testing.T) {
	srv := NewServer()
	_ = srv.RegisterMethod("test.ping", func(ctx context.Context, argsType proto.PayloadType, args []byte) (proto.PayloadType, []byte, error) {
		return proto.PayloadType_RAW, []byte("pong"), nil
	})

	lis := bufconn.Listen(1024 * 1024)
	gs := NewGRPCServer(srv)
	go func() { _ = gs.Serve(lis) }()
	defer gs.Stop()
	defer srv.Close()

	dialer := func(ctx context.Context, address string) (net.Conn, error) {
		return lis.DialContext(ctx)
	}
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	defer conn.Close()

	client := proto.NewGenericServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Invoke through the wire.
	invokeResp, err := client.Invoke(ctx, &proto.GenericRequest{Method: "test.ping"})
	if err != nil {
		t.Fatalf("client.Invoke: %v", err)
	}
	if !invokeResp.GetSuccess() || string(invokeResp.GetResult()) != "pong" {
		t.Fatalf("unexpected invoke response: %+v", invokeResp)
	}

	// File round trip through the wire.
	path := filepath.Join(t.TempDir(), "e2e.bin")
	openResp, err := client.OpenFile(ctx, &proto.OpenFileRequest{Path: path, Mode: proto.OpenFileRequest_READ_WRITE})
	if err != nil {
		t.Fatalf("client.OpenFile: %v", err)
	}
	if _, err := client.WriteFile(ctx, &proto.WriteFileRequest{
		Handle: openResp.GetHandle(),
		Data:   []byte("e2e"),
	}); err != nil {
		t.Fatalf("client.WriteFile: %v", err)
	}
	readResp, err := client.ReadFile(ctx, &proto.ReadFileRequest{Handle: openResp.GetHandle(), Size: 16})
	if err != nil {
		t.Fatalf("client.ReadFile: %v", err)
	}
	if string(readResp.GetData()) != "e2e" {
		t.Fatalf("data = %q, want e2e", string(readResp.GetData()))
	}
	if _, err := client.CloseFile(ctx, &proto.CloseFileRequest{Handle: openResp.GetHandle()}); err != nil {
		t.Fatalf("client.CloseFile: %v", err)
	}

	// Command execution through the wire.
	exe, args := echoShell()
	execResp, err := client.ExecuteCmd(ctx, &proto.ExecuteRequest{Executable: exe, Args: args})
	if err != nil {
		t.Fatalf("client.ExecuteCmd: %v", err)
	}
	if !execResp.GetSuccess() {
		t.Fatalf("expected execute success, message: %s", execResp.GetErrorMessage())
	}
}

var _ = os.PathSeparator // keep os import stable across platforms
