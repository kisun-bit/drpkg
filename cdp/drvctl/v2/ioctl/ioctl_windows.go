package ioctl

import (
	"syscall"

	"github.com/pkg/errors"
	"golang.org/x/sys/windows"
)

// doIoctl 是平台无关的 IOCTL 内部入口。
//
// 在 Windows 平台上，outBufSize 指定期望的输出缓冲区大小（DeviceIoControl 的 nOutBufferSize）。
// 若不需要输出数据则传 0。
func doIoctl(code uint, inBuf []byte, outBufSize int) ([]byte, error) {
	return ioctlCall(ctlCode(uint32(code)), inBuf, outBufSize)
}

// ioctlCall 是 Windows 平台下 IOCTL 通信的底层实现。
//
// 通过 CreateFile 打开驱动设备，调用 DeviceIoControl 发送控制码和输入缓冲区，
// 从输出缓冲区读取驱动返回数据后关闭设备句柄。
//
// 参数：
//   - code: CTL_CODE 控制码。
//   - inBuf: 请求数据缓冲区（可为空）。
//   - outBufSize: 期望的输出缓冲区大小（可为 0，表示不关心输出数据）。
//
// 返回值：
//   - outBuf: 驱动返回的实际数据（长度 = DeviceIoControl 返回的字节数）。
//   - err: 错误信息。
func ioctlCall(code uint32, inBuf []byte, outBufSize int) (outBuf []byte, err error) {
	utf16Name, err := windows.UTF16PtrFromString(deviceName)
	if err != nil {
		return nil, errors.Wrapf(err, "UTF16PtrFromString")
	}

	hDevice, err := windows.CreateFile(
		utf16Name,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, errors.Wrapf(err, "CreateFile")
	}
	defer windows.CloseHandle(hDevice)

	var inPtr *byte
	inSize := uint32(0)
	if len(inBuf) > 0 {
		inPtr = &inBuf[0]
		inSize = uint32(len(inBuf))
	}

	outBuf = make([]byte, outBufSize)
	var outPtr *byte
	outSize := uint32(0)
	if outBufSize > 0 {
		outPtr = &outBuf[0]
		outSize = uint32(outBufSize)
	}

	var bytesReturned uint32
	err = windows.DeviceIoControl(
		hDevice,
		code,
		inPtr,
		inSize,
		outPtr,
		outSize,
		&bytesReturned,
		nil,
	)
	if err != nil {
		return nil, errors.Wrapf(decodeDriverError(err), "DeviceIoControl")
	}

	return outBuf[:bytesReturned], nil
}

// decodeDriverError 将驱动返回的错误码转换为 Go error。
func decodeDriverError(err error) error {
	if err == nil {
		return nil
	}

	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return err
	}

	switch uint32(errno) {
	case uint32(windows.ERROR_BUSY): // STATUS_WDF_BUSY(0xC0200204, Windows)
		return ErrorOverflow
	case uint32(windows.ERROR_NO_SYSTEM_RESOURCES): // STATUS_INSUFFICIENT_RESOURCES(0xC000009A, Windows)
		return ErrorInsufficientMemSpace
	default:
		return err
	}
}
