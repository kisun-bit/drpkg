package minirpc

import (
	"context"

	"github.com/kisun-bit/drpkg/rpc/aio/core"
	"github.com/kisun-bit/drpkg/rpc/aio/proto"
)

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
	handle, err := s.core.OpenFile(ctx, req.GetPath(), core.OpenMode(req.GetMode()))
	if err != nil {
		return nil, err
	}
	return &proto.OpenFileResponse{Handle: handle}, nil
}

// ReadFile reads up to size bytes at the given offset of an open file.
//
// Eof is true when the requested range reaches or goes beyond the file end.
// A read past the file end returns an empty result with Eof=true.
func (s *Server) ReadFile(ctx context.Context, req *proto.ReadFileRequest) (*proto.ReadFileResponse, error) {
	res, err := s.core.ReadFile(ctx, req.GetHandle(), req.GetOffset(), req.GetSize())
	if err != nil {
		return nil, err
	}
	return &proto.ReadFileResponse{
		Data: res.Data,
		Eof:  res.Eof,
	}, nil
}

// WriteFile writes data at the given offset of an open file and returns the
// number of bytes written.
//
// Writing past the current file end extends the file with a hole (sparse
// region) on file systems that support it.
func (s *Server) WriteFile(ctx context.Context, req *proto.WriteFileRequest) (*proto.WriteFileResponse, error) {
	n, err := s.core.WriteFile(ctx, req.GetHandle(), req.GetOffset(), req.GetData())
	if err != nil {
		return nil, err
	}
	return &proto.WriteFileResponse{Size: uint32(n)}, nil
}

// CloseFile releases an open file handle. Closing an unknown handle returns
// an error.
func (s *Server) CloseFile(ctx context.Context, req *proto.CloseFileRequest) (*proto.CloseFileResponse, error) {
	if err := s.core.CloseFile(ctx, req.GetHandle()); err != nil {
		return nil, err
	}
	return &proto.CloseFileResponse{Success: true}, nil
}
