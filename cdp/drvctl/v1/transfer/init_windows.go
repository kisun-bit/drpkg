package transfer

import (
	"unsafe"

	"github.com/kisun-bit/drpkg/cdp/drvctl/v1/ioctl"

	"github.com/davecgh/go-spew/spew"
	"github.com/pkg/errors"
	"golang.org/x/sys/windows"
)

func TransferInit(maxReadLenP uint64, ringBufferSize uintptr) error {
	var err error

	if eventHandle == windows.InvalidHandle {
		eventHandle, err = windows.CreateEvent(nil, 0, 0, nil)
		if err != nil {
			return errors.Wrap(err, "CreateEvent failed")
		}
	}

	req := &ioctl.DRVReqCreateRingBuffer{
		EventFd: uint64(eventHandle),
	}

	if err = ioctl.ReqDrvCreateRingBuffer(req); err != nil {
		return errors.Wrap(err, "ReqDrvCreateRingBuffer failed")
	}

	if req.SHMAddress == 0 {
		return errors.New("SHMAddress is 0")
	}

	spew.Dump(req)

	sharedMemAddr = (*byte)(unsafe.Pointer(uintptr(req.SHMAddress)))
	if sharedMemAddr == nil {
		return errors.New("SHMAddress converted to pointer is nil")
	}

	MaxReadLen = maxReadLenP
	ShareMemSize = ringBufferSize
	nextReadPos = -1

	return nil
}
