package klog

import (
	"github.com/kisun-bit/drpkg/cdp/drvctl/v1/ioctl"

	ioctl2 "github.com/kisun-bit/drpkg/cdp/drvctl/v1/ioctl"

	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
)

func LogSetEvent() (err error) {
	var request ioctl.DRVReqSetLogEvent

	if eventHandle == 0 {
		eventHandle, err = unix.Eventfd(0, unix.EFD_CLOEXEC)
		if err != nil {
			return errors.Wrapf(err, "failed to open event handle")
		}
	}

	request.Handle = uint64(eventHandle)

	err = ioctl2.ReqDrvSetLogEvent(&request)
	if err != nil {
		return errors.Wrapf(err, "failed to set log event")
	}

	return nil
}
