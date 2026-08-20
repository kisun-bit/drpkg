package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync/atomic"
)

// OpenMode selects how OpenFile opens a file. Values intentionally match the
// OpenFileRequest.Mode enum of the minirpc proto, so adapters can convert
// with a plain cast.
type OpenMode int32

// File open modes.
const (
	OpenModeReadOnly OpenMode = 0
	OpenModeWriteOnly OpenMode = 1
	OpenModeReadWrite OpenMode = 2
)

// maxReadSize limits how much data one ReadFile call may return.
const maxReadSize = 8 * 1024 * 1024

// maxWriteSize limits how much data one WriteFile call may accept.
const maxWriteSize = 8 * 1024 * 1024

// OpenFile opens a file on the server and returns a handle for subsequent
// ReadFile / WriteFile / CloseFile calls.
//
// Modes:
//   - OpenModeReadOnly: the file must exist.
//   - OpenModeWriteOnly: the file is created when missing and truncated when
//     it exists.
//   - OpenModeReadWrite: the file is created when missing and kept otherwise.
//
// The handle is valid until CloseFile is called or the core is closed.
func (c *Core) OpenFile(ctx context.Context, path string, mode OpenMode) (uint64, error) {
	if path == "" {
		return 0, errors.New("file path is empty")
	}

	var flag int
	switch mode {
	case OpenModeReadOnly:
		flag = os.O_RDONLY
	case OpenModeWriteOnly:
		flag = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	case OpenModeReadWrite:
		flag = os.O_RDWR | os.O_CREATE
	default:
		return 0, fmt.Errorf("unsupported open mode: %v", int32(mode))
	}

	f, err := os.OpenFile(path, flag, 0o644)
	if err != nil {
		return 0, err
	}

	handle := atomic.AddUint64(&c.nextHandle, 1)

	c.filesMu.Lock()
	if c.files == nil {
		c.filesMu.Unlock()
		f.Close()
		return 0, errors.New("server is closed")
	}
	c.files[handle] = f
	c.filesMu.Unlock()

	return handle, nil
}

// ReadResult carries the outcome of one ReadFile call. Eof is true when the
// requested range reaches or goes beyond the file end.
type ReadResult struct {
	Data []byte
	Eof  bool
}

// ReadFile reads up to size bytes at the given offset of an open file.
//
// A read past the file end returns an empty result with Eof=true.
func (c *Core) ReadFile(ctx context.Context, handle uint64, offset uint64, size uint32) (*ReadResult, error) {
	f, err := c.lookupFile(handle)
	if err != nil {
		return nil, err
	}

	if size == 0 {
		return nil, errors.New("read size is zero")
	}
	if size > maxReadSize {
		size = maxReadSize
	}

	buf := make([]byte, size)
	n, readErr := f.ReadAt(buf, int64(offset))
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return nil, fmt.Errorf("read at offset %d: %v", offset, readErr)
	}

	return &ReadResult{
		Data: buf[:n],
		Eof:  errors.Is(readErr, io.EOF),
	}, nil
}

// WriteFile writes data at the given offset of an open file and returns the
// number of bytes written.
//
// Writing past the current file end extends the file with a hole (sparse
// region) on file systems that support it.
func (c *Core) WriteFile(ctx context.Context, handle uint64, offset uint64, data []byte) (int, error) {
	f, err := c.lookupFile(handle)
	if err != nil {
		return 0, err
	}

	if len(data) == 0 {
		return 0, nil
	}
	if len(data) > maxWriteSize {
		return 0, fmt.Errorf("write size %d exceeds limit %d", len(data), maxWriteSize)
	}

	n, err := f.WriteAt(data, int64(offset))
	if err != nil {
		return 0, fmt.Errorf("write at offset %d: %v", offset, err)
	}

	return n, nil
}

// CloseFile releases an open file handle. Closing an unknown handle returns
// an error.
func (c *Core) CloseFile(ctx context.Context, handle uint64) error {
	c.filesMu.Lock()
	f, ok := c.files[handle]
	if ok {
		delete(c.files, handle)
	}
	c.filesMu.Unlock()

	if !ok {
		return fmt.Errorf("file handle %d not found", handle)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("close file handle %d: %v", handle, err)
	}

	return nil
}

// lookupFile returns the open file registered under the handle.
func (c *Core) lookupFile(handle uint64) (*os.File, error) {
	c.filesMu.Lock()
	defer c.filesMu.Unlock()

	if c.files == nil {
		return nil, errors.New("server is closed")
	}
	f, ok := c.files[handle]
	if !ok {
		return nil, fmt.Errorf("file handle %d not found", handle)
	}
	return f, nil
}
