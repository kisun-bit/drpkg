package transfer

import (
	"github.com/kisun-bit/drpkg/disk/cdp/drvctl/ioctl"

	"golang.org/x/sys/unix"
)

func TransferDestroy() error {
	// 关闭事件
	if eventHandle != 0 {
		unix.Close(eventHandle)
		eventHandle = 0
	}

	// 取消映射
	if mapAddr != nil {
		err := unix.MunmapPtr(mapAddr, ShareMemSize)
		if err != nil {
			return err
		}
		mapAddr = nil
		sharedMemAddr = nil
	}

	// 删除共享缓存
	err := ioctl.ReqDrvDeleteRingBuffer()
	if err != nil {
		return err
	}

	MaxReadLen = 0

	ShareMemSize = 0

	nextReadPos = -1

	return nil
}
