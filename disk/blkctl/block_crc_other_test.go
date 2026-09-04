//go:build !windows

package blkctl

// errCRC 在非 Windows 平台上使用 timeoutError 作为替代。
var errCRC error = &timeoutError{offset: -1, duration: 0}
