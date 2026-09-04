//go:build windows

package xutil

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
// 调用后该线程上阻塞的 ReadFile 会立即返回 ERROR_OPERATION_ABORTED。
func cancelSynchronousIo(threadHandle windows.Handle) error {
	r1, _, e1 := syscall.SyscallN(procCancelSynchronousIo.Addr(), uintptr(threadHandle))
	if r1 == 0 {
		return e1
	}
	return nil
}

// cancelIoEx 取消指定文件句柄上的所有 I/O（包括其他线程发出的）。
// 作为 CancelSynchronousIo 的备选方案。
func cancelIoEx(fileHandle windows.Handle) error {
	r1, _, e1 := syscall.SyscallN(procCancelIoEx.Addr(), uintptr(fileHandle), 0)
	if r1 == 0 {
		return e1
	}
	return nil
}

// readFileAtWithTimeout 使用 Windows CancelSynchronousIo 取消阻塞的同步 I/O，
// 避免 goroutine 泄漏。若取消失败则回退到 goroutine+channel 方案。
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
		// 尝试 CancelSynchronousIo：打开线程句柄并取消 I/O
		threadHandle, openErr := windows.OpenThread(
			windows.THREAD_TERMINATE,
			false,
			threadId,
		)
		if openErr == nil {
			_ = cancelSynchronousIo(threadHandle)
			windows.CloseHandle(threadHandle)
		} else {
			// 备选：尝试 CancelIoEx（某些驱动支持）
			_ = cancelIoEx(windows.Handle(f.Fd()))
		}

		// 等待 goroutine 退出（被取消后 ReadAt 会立即返回错误）
		<-ch
		return 0, &timeoutError{offset: off, duration: timeout}
	}
}

// readAtWithTimeout 带超时的 ReadAt。
// 若 reader 是 *os.File 则使用平台 I/O 取消机制（无 goroutine 泄漏），
// 否则回退到 goroutine+channel 方案（仅用于 mock 等非文件场景）。
func readAtWithTimeout(r io.ReaderAt, p []byte, off int64, timeout time.Duration) (n int, err error) {
	if f, ok := r.(*os.File); ok {
		return readFileAtWithTimeout(f, p, off, timeout)
	}
	return readAtWithTimeoutFallback(r, p, off, timeout)
}
