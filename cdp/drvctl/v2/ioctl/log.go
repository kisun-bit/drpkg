package ioctl

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
