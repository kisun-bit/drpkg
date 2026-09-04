//go:build !windows && !linux

package xutil

import (
	"io"
	"os"
	"time"
)

// readFileAtWithTimeout 非 Windows/Linux 平台使用 goroutine 回退方案。
func readFileAtWithTimeout(f *os.File, p []byte, off int64, timeout time.Duration) (n int, err error) {
	return readAtWithTimeoutFallback(f, p, off, timeout)
}

// readAtWithTimeout 带超时的 ReadAt。
func readAtWithTimeout(r io.ReaderAt, p []byte, off int64, timeout time.Duration) (n int, err error) {
	if f, ok := r.(*os.File); ok {
		return readFileAtWithTimeout(f, p, off, timeout)
	}
	return readAtWithTimeoutFallback(r, p, off, timeout)
}
