package ioctl

import (
	"unsafe"

	"github.com/kisun-bit/drpkg/xutil"
	"github.com/pkg/errors"
	"golang.org/x/sys/windows"
)

// platformState 持有 Windows 平台特定的资源。
type platformState struct {
	eventHandle windows.Handle // 由用户层创建的共享内存事件句柄
}

// NewShmRing 创建并初始化共享内存环形缓冲区。
//
// size 为请求的共享内存大小（字节），maxReadLen 为单次读取的最大字节数。
//
// 内部先创建事件对象，再调用 CreateShm 向驱动请求创建共享内存并返回映射地址。
func NewShmRing(size uint32, maxReadLen uint64) (*ShmRing, error) {
	h, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "CreateEvent")
	}

	cfg, err := CreateShm(size, uint64(h))
	if err != nil {
		windows.CloseHandle(h)
		return nil, errors.Wrapf(err, "CreateShm")
	}

	if cfg.Address == 0 {
		windows.CloseHandle(h)
		return nil, errors.New("SHMAddress is 0 from driver")
	}

	ringHeader := (*RingHeader)(unsafe.Add(unsafe.Pointer(nil), cfg.Address))
	ringSize := ringHeader.RingSize
	dataSize := ringSize - RingHeaderWireSize
	ringData := (*byte)(unsafe.Add(unsafe.Pointer(nil), cfg.Address+RingHeaderWireSize))

	r := &ShmRing{
		ringHeader:  ringHeader,
		ringData:    ringData,
		ringSize:    ringSize,
		dataSize:    dataSize,
		nextReadPos: -1,
		maxReadLen:  maxReadLen,
		platform: platformState{
			eventHandle: h,
		},
	}

	return r, nil
}

// Close 销毁共享内存环形缓冲区。
//
// 关闭事件句柄并删除共享内存。
func (r *ShmRing) Close() error {
	if r.platform.eventHandle != windows.InvalidHandle {
		windows.CloseHandle(r.platform.eventHandle)
		r.platform.eventHandle = windows.InvalidHandle
	}

	return DeleteShm()
}

// Wait 等待共享内存事件，timeoutMs 为超时毫秒数。
//
// timeoutMs 为 0 时立即返回，为 windows.INFINITE 时无限等待。
func (r *ShmRing) Wait(timeoutMs uint32) error {
	if r.platform.eventHandle == windows.InvalidHandle {
		return errors.New("invalid event handle")
	}

	result, err := windows.WaitForSingleObject(r.platform.eventHandle, timeoutMs)
	if err != nil {
		return errors.Wrapf(err, "WaitForSingleObject")
	}

	if result == windows.WAIT_OBJECT_0 {
		return nil
	}
	return nil
}

// normalizeIoTimestamp 将 Windows 平台下驱动写入的 Microsoft 时间戳
// 转换为 Unix 微秒时间戳。
func normalizeIoTimestamp(h *IoHeader) {
	if h.Consistency == 1 {
		h.Timestamp = uint64(xutil.TimeByMicrosoftTimestamp(h.Timestamp).UnixMicro())
	}
}
