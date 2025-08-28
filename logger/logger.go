package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

func NewFileLogWriter(path string, maxSize, maxAge, maxBackups int) *lumberjack.Logger {
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	return &lumberjack.Logger{
		Filename:   path,
		MaxSize:    maxSize,
		MaxAge:     maxAge,
		MaxBackups: maxBackups,
		LocalTime:  true,
		Compress:   false,
	}
}

func NewLogger(name string, level zapcore.Level, writers ...io.Writer) *zap.SugaredLogger {
	if len(writers) == 0 {
		writers = append(writers, os.Stdout)
	}
	cfg := zapcore.EncoderConfig{
		LineEnding: zapcore.DefaultLineEnding,
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
	if runtime.GOOS == "windows" {
		cfg.LineEnding = "\r\n"
	}

	var cores []zapcore.Core
	for _, w := range writers {
		ws := zapcore.AddSync(w)
		core := zapcore.NewCore(zapcore.NewConsoleEncoder(cfg), ws, level)
		cores = append(cores, core)
	}
	teeCore := zapcore.NewTee(cores...)
	return zap.New(teeCore, zap.AddStacktrace(zapcore.FatalLevel)).Sugar()
}
