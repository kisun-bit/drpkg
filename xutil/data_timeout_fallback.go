package xutil

import (
	"io"
	"time"
)

// readAtWithTimeoutFallback goroutine+channel 回退方案。
// 仅用于 mock 等非 *os.File 场景或无法使用平台 I/O 取消的平台。
// 超时后 goroutine 会泄漏（阻塞在 syscall 中），但数量有限。
func readAtWithTimeoutFallback(r io.ReaderAt, p []byte, off int64, timeout time.Duration) (n int, err error) {
	type result struct {
		n   int
		err error
	}
	ch := make(chan result, 1)
	go func() {
		nn, rerr := r.ReadAt(p, off)
		ch <- result{nn, rerr}
	}()
	select {
	case res := <-ch:
		return res.n, res.err
	case <-time.After(timeout):
		return 0, &timeoutError{offset: off, duration: timeout}
	}
}
