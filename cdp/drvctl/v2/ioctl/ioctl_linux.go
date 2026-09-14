package ioctl

import (
	"encoding/binary"
	"os"
	"unsafe"

	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
)

const (
	// ringbuffer未正常工作 在设置一致性标记时，但共享缓存已经满了就会返回该错误码
	ERROR_RINGBUFFER_NOT_WORKING_FORBID_CLEAR_BIT = 140

	// 内存分配失败 任何接口都有可能返回该错误码
	ERR_NO_MEMORY = 102
)

// doIoctl 是平台无关的 IOCTL 内部入口。
//
// 在 Linux 平台上，ioctl 系统调用复用输入缓冲区存储应答数据，
// 因此缓冲区大小必须不小于实际响应大小。outBufSize 确保分配的缓冲区足够大：
//   - 若 outBufSize > len(inBuf)，缓冲区将扩展到 outBufSize。
//   - 若两者均为 0，缓冲区至少保留 8 字节以容纳长度前缀。
func doIoctl(code uint, inBuf []byte, outBufSize int) ([]byte, error) {
	bufSize := len(inBuf)
	if outBufSize > bufSize {
		bufSize = outBufSize
	}
	if bufSize < 8 {
		bufSize = 8
	}

	padded := make([]byte, bufSize)
	copy(padded, inBuf)

	return ioctlCall(getIoctlCode(code), padded)
}

// ioctlCall 是 Linux 平台下 IOCTL 通信的底层实现。
//
// 打开驱动设备文件，将请求数据按 [长度前缀(8字节) + 请求数据] 格式打包，
// 通过 ioctl 系统调用发送。驱动会重用输入缓冲区存放返回数据。
//
// 参数：
//   - code: IOC 命令码（由 getIoctlCode 生成）。
//   - inBuf: 请求数据缓冲区。
//
// 返回值：
//   - outBuf: 驱动返回的数据（复用输入缓冲区，去掉 8 字节长度前缀）。
//   - err: 错误信息。
func ioctlCall(code uint, inBuf []byte) (outBuf []byte, err error) {
	f, err := os.OpenFile(deviceName, os.O_RDWR, 0)
	if err != nil {
		return nil, errors.Wrapf(err, "OpenFile")
	}
	defer f.Close()

	fd := int(f.Fd())

	// 按 [长度前缀(8字节) + 请求数据] 格式构造缓冲区
	reqSize := uint64(len(inBuf))
	totalLen := uint64(unsafe.Sizeof(reqSize)) + reqSize
	buf := make([]byte, totalLen)
	binary.LittleEndian.PutUint64(buf[:8], reqSize)
	copy(buf[8:], inBuf)

	drvErrCode, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		uintptr(fd),
		uintptr(code),
		uintptr(unsafe.Pointer(&buf[0])),
	)
	if errno != 0 {
		return nil, errors.Errorf("ioctl syscall: %v", errno)
	}

	if drvErrCode != 0 {
		return nil, decodeDriverError(int32(drvErrCode))
	}

	// 驱动将返回数据写入同一缓冲区（覆盖长度前缀之后的区域）
	return buf[8:], nil
}

// decodeDriverError 将驱动返回的错误码转换为 Go error。
func decodeDriverError(code int32) error {
	switch code {
	case ERROR_RINGBUFFER_NOT_WORKING_FORBID_CLEAR_BIT:
		return ErrorOverflow
	case ERR_NO_MEMORY:
		return ErrorInsufficientMemSpace
	}

	if code > 0 {
		return errors.Errorf("driver internal error (code=%d)", code)
	}
	return errors.Errorf("ioctl system error (code=%d)", code)
}
