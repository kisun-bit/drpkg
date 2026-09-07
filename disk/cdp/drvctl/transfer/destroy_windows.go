package transfer

import (
	"github.com/kisun-bit/drpkg/disk/cdp/drvctl/ioctl"

	"github.com/pkg/errors"
	"golang.org/x/sys/windows"
)

func TransferDestroy() error {
	var err error

	if eventHandle != windows.InvalidHandle {
		err = windows.CloseHandle(eventHandle)
		if err != nil {
			return err
		}
		eventHandle = windows.InvalidHandle
	}

	err = ioctl.ReqDrvDeleteRingBuffer()
	if err != nil {
		return errors.Wrapf(err, "ReqDrvDeleteRingBuffer")
	}

	MaxReadLen = 0
	ShareMemSize = 0
	nextReadPos = -1

	return nil
}
