package recovery

import "time"

type LogLevel int

const (
	LogDebug LogLevel = iota
	LogInfo
	LogWarn
	LogError
)

type LogEntry struct {
	Time    time.Time
	Level   LogLevel
	Message string
}
