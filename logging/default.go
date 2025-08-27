package logger

import "go.uber.org/zap"

var (
	defaultLogger = NewLogger("drpkg", zap.DebugLevel)
)

func SetupDefaultLogger(l *zap.SugaredLogger) {
	defaultLogger = l
}

func Debug(args ...interface{}) {
	defaultLogger.Debug(args...)
}

func Debugf(template string, args ...interface{}) {
	defaultLogger.Debugf(template, args...)
}

func Info(args ...interface{}) {
	defaultLogger.Info(args...)
}

func Infof(template string, args ...interface{}) {
	defaultLogger.Infof(template, args...)
}

func Warn(args ...interface{}) {
	defaultLogger.Warn(args...)
}

func Warnf(template string, args ...interface{}) {
	defaultLogger.Warnf(template, args...)
}

func Error(args ...interface{}) {
	defaultLogger.Error(args...)
}

func Errorf(template string, args ...interface{}) {
	defaultLogger.Errorf(template, args...)
}

func Fatal(args ...interface{}) {
	defaultLogger.Fatal(args...)
}

func Fatalf(template string, args ...interface{}) {
	defaultLogger.Fatalf(template, args...)
}
