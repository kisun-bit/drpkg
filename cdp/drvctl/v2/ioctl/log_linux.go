package ioctl

import (
	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
)

// LogEvent 封装 Linux 平台的日志事件文件描述符。
//
// 创建后自动向驱动注册，可通过 Wait 等待事件。
type LogEvent struct {
	fd int
}

// NewLogEvent 创建日志事件对象并向驱动注册。
//
// 内部通过 eventfd 创建事件，然后通过 SetLogEvent 注册到驱动。
func NewLogEvent() (*LogEvent, error) {
	fd, err := unix.Eventfd(0, unix.EFD_CLOEXEC)
	if err != nil {
		return nil, errors.Wrapf(err, "eventfd")
	}

	if err := SetLogEvent(uint64(fd)); err != nil {
		unix.Close(fd)
		return nil, errors.Wrapf(err, "SetLogEvent")
	}

	return &LogEvent{fd: fd}, nil
}

// Close 关闭事件文件描述符。
func (e *LogEvent) Close() error {
	if e.fd == 0 {
		return nil
	}
	if err := unix.Close(e.fd); err != nil {
		return errors.Wrapf(err, "close")
	}
	e.fd = 0
	return nil
}

// Wait 等待日志事件，timeoutMs 为超时毫秒数。
//
// timeoutMs 为 -1 时无限等待，为 0 时立即返回。
func (e *LogEvent) Wait(timeoutMs int) error {
	if e.fd == 0 {
		return errors.New("invalid handle")
	}

	pollFds := []unix.PollFd{
		{Fd: int32(e.fd), Events: unix.POLLIN},
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
		if _, err := unix.Read(e.fd, b[:]); err != nil {
			if err == unix.EINTR {
				continue
			}
			return errors.Wrapf(err, "read eventfd")
		}

		return nil
	}
}

// Handle 返回内部事件文件描述符，供需要原始 fd 的场景使用。
func (e *LogEvent) Handle() uint64 {
	return uint64(e.fd)
}

// normalizeLogTimestamp 在 Linux 上为 no-op。
// Linux 驱动直接返回 Unix 时间戳，无需转换。
func normalizeLogTimestamp(_ *LogEntry) {}
