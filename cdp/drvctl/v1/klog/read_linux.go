package klog

import (
	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
)

func LogReadWait() error {
	if eventHandle == 0 {
		return errors.New("invalid handle")
	}

	pollFds := []unix.PollFd{
		{Fd: int32(eventHandle), Events: unix.POLLIN},
	}

	for {
		n, err := unix.Poll(pollFds, -1)
		if err != nil {
			if err == unix.EINTR {
				continue
			} else {
				return err
			}
		}

		if n == 0 {
			// 超时 直接返回成功
			return nil
		}

		var b [8]byte
		_, err = unix.Read(eventHandle, b[:])

		if err != nil {
			if err == unix.EINTR {
				continue
			} else {
				return err
			}
		}

		return nil
	}
}
