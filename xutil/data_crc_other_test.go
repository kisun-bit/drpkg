//go:build !windows

package xutil

// errCRC 在非 Windows 平台上使用 timeoutError 作为替代，
// 因为 IsDataCrcError 在 Linux 上始终返回 false。
// 坏扇区处理通过超时机制触发，行为等价。
var errCRC error = &timeoutError{offset: -1, duration: 0}
