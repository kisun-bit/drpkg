package klog

import (
	"github.com/pkg/errors"
	"golang.org/x/sys/windows"
)

func LogReadWait() (err error) {
	if eventHandle == windows.InvalidHandle {
		return errors.New("invalid handle")
	}

	result, err := windows.WaitForSingleObject(eventHandle, EventWaitTimeout)

	if err != nil {
		return errors.Wrap(err, "WaitForSingleObject failed")
	}

	if result == windows.WAIT_ABANDONED {
		return errors.New("wait abandoned")
	}

	if result == uint32(windows.WAIT_TIMEOUT) {
		return nil
	}

	if result == windows.WAIT_OBJECT_0 {
		return nil
	}

	return nil
}
