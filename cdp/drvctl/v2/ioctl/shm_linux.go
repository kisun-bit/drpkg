package ioctl

import (
	"os"
	"unsafe"

	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
)

// platformState 持有 Linux 平台特定的资源。
type platformState struct {
	eventFd  int            // eventfd 文件描述符
	devFd    int            // 驱动设备文件描述符
	mmapAddr unsafe.Pointer // mmap 映射地址，用于 munmap
	mmapSize uintptr        // mmap 映射大小
}

// NewShmRing 创建并初始化共享内存环形缓冲区。
//
// size 为请求的共享内存大小（字节），maxReadLen 为单次读取的最大字节数。
//
// Linux 平台下的初始化流程：
//  1. 打开驱动设备文件。
//  2. 调用 CreateShm 创建共享内存。
//  3. 通过 mmap 映射共享内存到用户空间。
func NewShmRing(size uint32, maxReadLen uint64) (*ShmRing, error) {
	f, err := os.OpenFile(deviceName, os.O_RDWR, 0)
	if err != nil {
		return nil, errors.Wrapf(err, "open device")
	}
	devFd := int(f.Fd())

	cfg, err := CreateShm(size)
	if err != nil {
		f.Close()
		return nil, errors.Wrapf(err, "CreateShm")
	}

	// mmap 映射共享内存
	mmapSize := uintptr(size)
	addr, err := unix.Mmap(devFd, 0, int(mmapSize), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		f.Close()
		err2 := DeleteShm()
		_ = err2
		return nil, errors.Wrapf(err, "mmap")
	}

	ringHeader := (*RingHeader)(unsafe.Pointer(&addr[0]))
	ringSize := ringHeader.RingSize
	dataSize := ringSize - RingHeaderWireSize

	r := &ShmRing{
		ringHeader:  ringHeader,
		ringData:    (*byte)(unsafe.Pointer(&addr[RingHeaderWireSize])),
		ringSize:    ringSize,
		dataSize:    dataSize,
		nextReadPos: -1,
		maxReadLen:  maxReadLen,
		platform: platformState{
			eventFd:  int(cfg.EventHandle),
			devFd:    devFd,
			mmapAddr: unsafe.Pointer(&addr[0]),
			mmapSize: mmapSize,
		},
	}

	return r, nil
}

// Close 销毁共享内存环形缓冲区。
//
// 关闭事件 fd、解除 mmap 映射、关闭设备 fd，并删除共享内存。
func (r *ShmRing) Close() error {
	var lastErr error

	if r.platform.eventFd != 0 {
		if err := unix.Close(r.platform.eventFd); err != nil {
			lastErr = err
		}
		r.platform.eventFd = 0
	}

	if r.platform.mmapAddr != nil && r.platform.mmapSize > 0 {
		if err := unix.Munmap(unsafe.Slice((*byte)(r.platform.mmapAddr), r.platform.mmapSize)); err != nil {
			if lastErr == nil {
				lastErr = err
			}
		}
		r.platform.mmapAddr = nil
		r.platform.mmapSize = 0
	}

	if err := DeleteShm(); err != nil && lastErr == nil {
		lastErr = err
	}

	r.ringHeader = nil
	r.ringData = nil

	return lastErr
}

// Wait 等待共享内存事件，timeoutMs 为超时毫秒数。
//
// timeoutMs 为 -1 时无限等待，为 0 时立即返回。
func (r *ShmRing) Wait(timeoutMs int) error {
	if r.platform.eventFd == 0 {
		return errors.New("invalid event fd")
	}

	pollFds := []unix.PollFd{
		{Fd: int32(r.platform.eventFd), Events: unix.POLLIN},
	}

	for {
		n, err := unix.Poll(pollFds, timeoutMs)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return errors.Wrapf(err, "poll")
		}

		if n == 0 {
			return nil // 超时
		}

		var b [8]byte
		if _, err := unix.Read(r.platform.eventFd, b[:]); err != nil {
			if err == unix.EINTR {
				continue
			}
			return errors.Wrapf(err, "read eventfd")
		}

		return nil
	}
}

// normalizeIoTimestamp 在 Linux 上为 no-op。
func normalizeIoTimestamp(_ *IoHeader) {}
