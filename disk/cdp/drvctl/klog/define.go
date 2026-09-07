package klog

const (
	// EventWaitTimeout 等待事件的超时时间
	EventWaitTimeout = 2000 // Milliseconds
)

type LogStatus uint8

const (
	LogSuccess LogStatus = iota

	LogError

	LogNoData
)
