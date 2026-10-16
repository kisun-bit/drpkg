package ioctl

import (
	"fmt"
	"strings"

	"github.com/kisun-bit/drpkg/xutil"
	"go.uber.org/zap"
)

//
// CDP 日志管理：
// * 设置日志事件
// * 获取日志
// * 事件等待
//

// LogStatus 表示日志读取操作的返回状态。
type LogStatus uint8

const (
	// LogSuccess 表示成功读取到一条日志。
	LogSuccess LogStatus = iota

	// LogError 表示读取过程中发生错误。
	LogError

	// LogNoData 表示当前没有可用日志。
	LogNoData
)

// LogEventSetRequest 表示设置日志事件的请求。
type LogEventSetRequest struct {
	// Event 为日志读写事件对象句柄。
	Event uint64
}

// LogEntry 表示日志条目的请求与响应。
type LogEntry struct {
	// Level 为日志级别（响应参数）。
	Level uint8

	// Timestamp 为日志时间戳（响应参数）。
	Timestamp uint64

	// DataLen 为日志内容实际长度（响应参数）。
	//
	// Data 是定长 512 字节缓冲区，DataLen 指示其中有效字节数。
	DataLen uint32

	// Data 为日志内容缓冲区（响应参数）。
	Data [512]byte
}

// ReadLog 从驱动读取一条日志条目。
//
// 返回值：
//   - entry: 日志条目（LogNoData 时为 nil）。
//   - status: 读取状态。
//   - err: 仅在 status == LogError 时非 nil。
func ReadLog() (*LogEntry, LogStatus, error) {
	entry, err := GetLog()
	if err != nil {
		return nil, LogError, err
	}

	if entry.Level == 0 {
		return nil, LogNoData, nil
	}

	normalizeLogTimestamp(entry)

	return entry, LogSuccess, nil
}

// DataBytes 返回 Data 中有效字节的切片。
// DataLen 异常（大于 512）时收敛到缓冲区长度，避免越界。
func (l *LogEntry) DataBytes() []byte {
	n := l.DataLen
	if n > uint32(len(l.Data)) {
		n = uint32(len(l.Data))
	}
	return l.Data[:n]
}

func (l *LogEntry) Message() string {
	ts := xutil.TimeByMicrosoftTimestamp(l.Timestamp)
	return fmt.Sprintf("[%s] %d %s", ts.String(), l.Level, strings.TrimSpace(xutil.TrimZeroString(l.DataBytes())))
}

// 驱动日志级别（与驱动 log.h 的 BIOTRK_LOG_LEVEL_* 保持一致）。
const (
	// DriverLogLevelTrace 对应 BIOTRK_LOG_LEVEL_TRACE(0x1)。
	DriverLogLevelTrace uint8 = 0x1

	// DriverLogLevelInfo 对应 BIOTRK_LOG_LEVEL_INFO(0x2)。
	DriverLogLevelInfo uint8 = 0x2

	// DriverLogLevelWarn 对应 BIOTRK_LOG_LEVEL_WARN(0x4)。
	DriverLogLevelWarn uint8 = 0x4

	// DriverLogLevelError 对应 BIOTRK_LOG_LEVEL_ERROR(0x8)。
	DriverLogLevelError uint8 = 0x8
)

// ForwardDriverLogs continuously reads driver logs and forwards them
// to the logger using the corresponding log levels.
//
// Level mapping:
//   - DriverLogLevelTrace (0x1) → Debug
//   - DriverLogLevelInfo  (0x2) → Info
//   - DriverLogLevelWarn  (0x4) → Warn
//   - DriverLogLevelError (0x8) → Error
//
// The function blocks until an error occurs and returns a non-nil error
// if reading logs or waiting for a log event fails.
func ForwardDriverLogs(logger *zap.SugaredLogger) error {
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}

	le, err := NewLogEvent()
	if err != nil {
		return err
	}
	defer le.Close()

	for {
		entry, status, err := ReadLog()
		if err != nil {
			return err
		}

		if status == LogNoData { // 无日志数据，等待
			if err := le.Wait(-1); err != nil {
				return err
			}
			continue
		}

		msg := strings.TrimSpace(xutil.TrimZeroString(entry.DataBytes()))

		switch entry.Level {
		case DriverLogLevelTrace:
			logger.Debug(msg)
		case DriverLogLevelInfo:
			logger.Info(msg)
		case DriverLogLevelWarn:
			logger.Warn(msg)
		case DriverLogLevelError:
			logger.Error(msg)
		default:
			logger.Infof("[unknown-level-%d] %s", entry.Level, msg)
		}
	}
}
