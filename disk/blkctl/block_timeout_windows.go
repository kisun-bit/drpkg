//go:build windows

package blkctl

import (
	"io"
	"os"
	"runtime"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

var (
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procCancelSynchronousIo = kernel32.NewProc("CancelSynchronousIo")
	procCancelIoEx          = kernel32.NewProc("CancelIoEx")
)

// cancelSynchronousIo 取消指定线程上所有正在进行的同步 I/O。
func cancelSynchronousIo(threadHandle windows.Handle) error {
	r1, _, e1 := syscall.SyscallN(procCancelSynchronousIo.Addr(), uintptr(threadHandle))
	if r1 == 0 {
		return e1
	}
	return nil
}

// cancelIoEx 取消指定文件句柄上的所有 I/O（包括其他线程发出的）。
func cancelIoEx(fileHandle windows.Handle) error {
	r1, _, e1 := syscall.SyscallN(procCancelIoEx.Addr(), uintptr(fileHandle), 0)
	if r1 == 0 {
		return e1
	}
	return nil
}

// readFileAtWithTimeout 使用 Windows CancelSynchronousIo 取消阻塞的同步 I/O。
func readFileAtWithTimeout(f *os.File, p []byte, off int64, timeout time.Duration) (n int, err error) {
	type result struct {
		n   int
		err error
	}

	ch := make(chan result, 1)
	threadIdCh := make(chan uint32, 1)

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		threadIdCh <- windows.GetCurrentThreadId()

		nn, rerr := f.ReadAt(p, off)
		ch <- result{nn, rerr}
	}()

	threadId := <-threadIdCh

	select {
	case res := <-ch:
		return res.n, res.err
	case <-time.After(timeout):
		threadHandle, openErr := windows.OpenThread(
			windows.THREAD_TERMINATE,
			false,
			threadId,
		)
		if openErr == nil {
			_ = cancelSynchronousIo(threadHandle)
			windows.CloseHandle(threadHandle)
		} else {
			_ = cancelIoEx(windows.Handle(f.Fd()))
		}

		<-ch
		return 0, &timeoutError{offset: off, duration: timeout}
	}
}

// readAtWithTimeout 带超时的 ReadAt。
func readAtWithTimeout(r io.ReaderAt, p []byte, off int64, timeout time.Duration) (n int, err error) {
	if f, ok := r.(*os.File); ok {
		return readFileAtWithTimeout(f, p, off, timeout)
	}
	return readAtWithTimeoutFallback(r, p, off, timeout)
}
