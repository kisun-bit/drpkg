package logger

import (
	"fmt"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"io"
	"os"
	"runtime"
	"time"
)

func NewLogger(name string, level zapcore.Level, writers ...io.Writer) *zap.SugaredLogger {
	if len(writers) == 0 {
		writers = append(writers, os.Stdout)
	}
	cfg := zapcore.EncoderConfig{
		TimeKey:       "ts",
		LevelKey:      "level",
		NameKey:       "logger",
		CallerKey:     "caller",
		FunctionKey:   zapcore.OmitKey,
		MessageKey:    "msg",
		StacktraceKey: "stacktrace",
		LineEnding:    zapcore.DefaultLineEnding,
		EncodeLevel: func(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
			enc.AppendString(fmt.Sprintf("%-7s", "["+level.CapitalString()+"]"))
		},
		EncodeTime: func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
			if name != "" {
				enc.AppendString("[" + name + "]")
			}
			enc.AppendString("[" + t.Format("2006-01-02 15:04:05.000") + "]")
		},
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller: func(caller zapcore.EntryCaller, enc zapcore.PrimitiveArrayEncoder) {
			enc.AppendString("[" + caller.TrimmedPath() + "]")
		},
		ConsoleSeparator: " ",
	}
	switch runtime.GOOS {
	case "windows":
		cfg.LineEnding = "\r\n"
	}

	writerCores := make([]zapcore.WriteSyncer, 0)
	for _, w := range writers {
		writerCores = append(writerCores, zapcore.AddSync(w))
	}
	var cores []zapcore.Core
	for _, w := range writerCores {
		cores = append(cores, zapcore.NewCore(zapcore.NewConsoleEncoder(cfg), w, level))
	}
	teeCore := zapcore.NewTee(cores...)
	return zap.New(teeCore, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel)).Sugar()
}
