package klog

import (
	"github.com/kisun-bit/drpkg/disk/cdp/drvctl/ioctl"

	"github.com/pkg/errors"
	"golang.org/x/sys/windows"
)

func LogSetEvent() (err error) {
	var request ioctl.DRVReqSetLogEvent

	// 创建命名事件
	if eventHandle == windows.InvalidHandle {
		eventHandle, err = windows.CreateEvent(nil, 0, 0, nil)
		if err != nil {
			return errors.Wrapf(err, "CreateEvent")
		}
	}

	request.Handle = uint64(eventHandle)

	err = ioctl.ReqDrvSetLogEvent(&request)
	if err != nil {
		return errors.Wrapf(err, "ReqDrvSetLogEvent")
	}

	return nil
}
