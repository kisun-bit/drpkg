//go:build windows

package xutil

import "golang.org/x/sys/windows"

// errCRC 在 Windows 上为 windows.ERROR_CRC，能被 IsDataCrcError 识别。
var errCRC error = windows.ERROR_CRC
