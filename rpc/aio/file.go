package aio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync/atomic"

	"github.com/kisun-bit/drpkg/rpc/aio/proto"
)

// maxReadSize limits how much data one ReadFile call may return.
const maxReadSize = 8 * 1024 * 1024

// maxWriteSize limits how much data one WriteFile call may accept.
const maxWriteSize = 8 * 1024 * 1024

// OpenFile opens a file on the server and returns a handle for subsequent
// ReadFile / WriteFile / CloseFile calls.
//
// Modes:
//   - READ_ONLY: the file must exist.
//   - WRITE_ONLY: the file is created when missing and truncated when it
//     exists.
//   - READ_WRITE: the file is created when missing and kept otherwise.
//
// The handle is valid until CloseFile is called or the server is closed.
func (s *Server) OpenFile(ctx context.Context, req *proto.OpenFileRequest) (*proto.OpenFileResponse, error) {
	path := req.GetPath()
	if path == "" {
		return nil, errors.New("file path is empty")
	}

	var flag int
	switch req.GetMode() {
	case proto.OpenFileRequest_READ_ONLY:
		flag = os.O_RDONLY
	case proto.OpenFileRequest_WRITE_ONLY:
		flag = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	case proto.OpenFileRequest_READ_WRITE:
		flag = os.O_RDWR | os.O_CREATE
	default:
		return nil, fmt.Errorf("unsupported open mode: %v", req.GetMode())
	}

	f, err := os.OpenFile(path, flag, 0o644)
	if err != nil {
		return nil, err
	}

	handle := atomic.AddUint64(&s.nextHandle, 1)

	s.filesMu.Lock()
	if s.files == nil {
		s.filesMu.Unlock()
		f.Close()
		return nil, errors.New("server is closed")
	}
	s.files[handle] = f
	s.filesMu.Unlock()

	return &proto.OpenFileResponse{Handle: handle}, nil
}

// ReadFile reads up to size bytes at the given offset of an open file.
//
// Eof is true when the requested range reaches or goes beyond the file end.
// A read past the file end returns an empty result with Eof=true.
func (s *Server) ReadFile(ctx context.Context, req *proto.ReadFileRequest) (*proto.ReadFileResponse, error) {
	f, err := s.lookupFile(req.GetHandle())
	if err != nil {
		return nil, err
	}

	size := req.GetSize()
	if size == 0 {
		return nil, errors.New("read size is zero")
	}
	if size > maxReadSize {
		size = maxReadSize
	}

	buf := make([]byte, size)
	n, readErr := f.ReadAt(buf, int64(req.GetOffset()))
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return nil, fmt.Errorf("read at offset %d: %v", req.GetOffset(), readErr)
	}

	return &proto.ReadFileResponse{
		Data: buf[:n],
		Eof:  errors.Is(readErr, io.EOF),
	}, nil
}

// WriteFile writes data at the given offset of an open file and returns the
// number of bytes written.
//
// Writing past the current file end extends the file with a hole (sparse
// region) on file systems that support it.
func (s *Server) WriteFile(ctx context.Context, req *proto.WriteFileRequest) (*proto.WriteFileResponse, error) {
	f, err := s.lookupFile(req.GetHandle())
	if err != nil {
		return nil, err
	}

	data := req.GetData()
	if len(data) == 0 {
		return &proto.WriteFileResponse{Size: 0}, nil
	}
	if len(data) > maxWriteSize {
		return nil, fmt.Errorf("write size %d exceeds limit %d", len(data), maxWriteSize)
	}

	n, err := f.WriteAt(data, int64(req.GetOffset()))
	if err != nil {
		return nil, fmt.Errorf("write at offset %d: %v", req.GetOffset(), err)
	}

	return &proto.WriteFileResponse{Size: uint32(n)}, nil
}

// CloseFile releases an open file handle. Closing an unknown handle returns
// an error.
func (s *Server) CloseFile(ctx context.Context, req *proto.CloseFileRequest) (*proto.CloseFileResponse, error) {
	handle := req.GetHandle()

	s.filesMu.Lock()
	f, ok := s.files[handle]
	if ok {
		delete(s.files, handle)
	}
	s.filesMu.Unlock()

	if !ok {
		return nil, fmt.Errorf("file handle %d not found", handle)
	}

	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("close file handle %d: %v", handle, err)
	}

	return &proto.CloseFileResponse{Success: true}, nil
}

// lookupFile returns the open file registered under the handle.
func (s *Server) lookupFile(handle uint64) (*os.File, error) {
	s.filesMu.Lock()
	defer s.filesMu.Unlock()

	if s.files == nil {
		return nil, errors.New("server is closed")
	}
	f, ok := s.files[handle]
	if !ok {
		return nil, fmt.Errorf("file handle %d not found", handle)
	}
	return f, nil
}
