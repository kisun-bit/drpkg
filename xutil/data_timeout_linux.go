//go:build linux

package xutil

import (
	"io"
	"os"
	"time"
)

// readFileAtWithTimeout Linux 上使用 goroutine+channel 方案。
// Linux 内核的磁盘 I/O 超时（通常 30s）由 SCSI 命令定时器控制，
// 超时后 syscall 会返回 EIO，goroutine 正常退出，不会永久泄漏。
// 若需更激进的超时控制，可使用 O_NONBLOCK + poll，但 Linux 对
// 常规文件/块设备不支持 O_NONBLOCK，故此处保持 goroutine 方案。
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
