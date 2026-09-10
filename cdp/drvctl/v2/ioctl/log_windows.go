package ioctl

import (
	"github.com/kisun-bit/drpkg/xutil"
	"github.com/pkg/errors"
	"golang.org/x/sys/windows"
)

// LogEvent 封装 Windows 平台的日志事件句柄。
//
// 创建后必须调用 SetLogEvent 将句柄注册到驱动，然后可通过 Wait 等待事件。
type LogEvent struct {
	handle windows.Handle
}

// NewLogEvent 创建日志事件对象并向驱动注册。
//
// 内部调用 CreateEvent 创建自动重置事件，然后通过 SetLogEvent 注册到驱动。
func NewLogEvent() (*LogEvent, error) {
	h, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "CreateEvent")
	}

	if err := SetLogEvent(uint64(h)); err != nil {
		windows.CloseHandle(h)
		return nil, errors.Wrapf(err, "SetLogEvent")
	}

	return &LogEvent{handle: h}, nil
}

// Close 关闭事件句柄。
func (e *LogEvent) Close() error {
	if e.handle == windows.InvalidHandle {
		return nil
	}
	if err := windows.CloseHandle(e.handle); err != nil {
		return errors.Wrapf(err, "CloseHandle")
	}
	e.handle = windows.InvalidHandle
	return nil
}

// Wait 等待日志事件，timeoutMs 为超时毫秒数。
//
// timeoutMs 为 0 时立即返回，为 windows.INFINITE 时无限等待。
func (e *LogEvent) Wait(timeoutMs uint32) error {
	if e.handle == windows.InvalidHandle {
		return errors.New("invalid handle")
	}

	result, err := windows.WaitForSingleObject(e.handle, timeoutMs)
	if err != nil {
		return errors.Wrapf(err, "WaitForSingleObject")
	}

	if result == windows.WAIT_OBJECT_0 {
		return nil
	}
	return nil
}

// Handle 返回内部事件句柄，供需要原始句柄的场景使用。
func (e *LogEvent) Handle() uint64 {
	return uint64(e.handle)
}

// normalizeLogTimestamp 将 Windows 平台下驱动返回的 Microsoft 时间戳
// 转换为 Unix 微秒时间戳。
func normalizeLogTimestamp(entry *LogEntry) {
	entry.Timestamp = uint64(xutil.TimeByMicrosoftTimestamp(entry.Timestamp).UnixMicro())
}
