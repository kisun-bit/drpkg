package transfer

import (
	"os"

	"github.com/kisun-bit/drpkg/cdp/drvctl/v1/ioctl"
	"github.com/kisun-bit/drpkg/logger"

	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
)

func TransferInit(maxReadLenP uint64, ringBufferSize uintptr) (err error) {
	var req ioctl.DRVReqCreateRingBuffer

	// 打开驱动设备
	f, err := os.OpenFile(ioctl.DriverSymbolName, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	devFd := int(f.Fd())

	// 创建事件句柄
	if eventHandle == 0 {
		eventHandle, err = unix.Eventfd(0, unix.EFD_CLOEXEC)
		if err != nil {
			return errors.Errorf("CreateEvent Failed :%v", err)
		}
	}

	req.EventFd = uint64(eventHandle)

	err = ioctl.ReqDrvCreateRingBuffer(&req)

	if err != nil {
		// 关闭事件
		unix.Close(eventHandle)
		eventHandle = 0
		return err
	}

	// 调用mmap进行内存映射
	logger.Debugf("fd:%d, size:%d", devFd, ringBufferSize)
	mapAddr, err = unix.MmapPtr(devFd, 0, nil, ringBufferSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		logger.Errorf("TransferInit: MmapPtr: %v", err)

		// 关闭事件
		unix.Close(eventHandle)
		eventHandle = 0

		// 删除共享内存
		_ = ioctl.ReqDrvDeleteRingBuffer()
		return err
	}

	if mapAddr == nil {
		// 关闭事件
		unix.Close(eventHandle)
		eventHandle = 0

		// 取消映射
		_ = unix.MunmapPtr(mapAddr, ShareMemSize)
		mapAddr = nil

		// 删除共享内存
		_ = ioctl.ReqDrvDeleteRingBuffer()
		return errors.Errorf("IOCTLTransferInit: The SharedMemAddr is nil")
	}

	sharedMemAddr = (*byte)(mapAddr)

	MaxReadLen = maxReadLenP

	ShareMemSize = ringBufferSize

	nextReadPos = -1

	return nil
}
