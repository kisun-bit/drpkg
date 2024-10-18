package main

import (
	"context"
	"fmt"
	virtdisk "github.com/kisun-bit/drpkg/disk/image/qcow2"
	"github.com/kisun-bit/drpkg/util/logger"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"io"
	"os"
	"runtime"
	"sync"
	"time"
)

func NewLogger(writers ...io.Writer) *zap.SugaredLogger {
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
		cores = append(cores, zapcore.NewCore(zapcore.NewConsoleEncoder(cfg), w, zapcore.DebugLevel))
	}
	teeCore := zapcore.NewTee(cores...)
	return zap.New(teeCore, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel)).Sugar()
}

func main() {
	virtdisk.QemuEnvSetup("", "/home/kisun/qemu-7.2.0/build/qemu-iow")

	q, err := virtdisk.NewQemuIOManager(context.Background(),
		"/home/kisun/qemu-7.2.0/full_1.qcow2",
		"qcow2",
		virtdisk.Direct, virtdisk.EnableRWSerialAccess())
	if err != nil {
		logger.Fatalf(">NewQemuIOManager(...): %v", err)
	}
	if err = q.Open(); err != nil {
		logger.Fatalf("Open(...), image=%s, err=%v", q.Image, err)
	}
	defer func() {
		if err == nil {
			err = q.Error()
		}
	}()
	defer func() {
		_ = q.Close()
	}()

	var wg sync.WaitGroup
	addrWithLen := map[int64]int{
		0:        4096,
		1048576:  4096,
		55574528: 4096,
		1347584:  4096,
		9117696:  4096,
		1351680:  4096,
		10485760: 4096,
		10424320: 20480,
		10444800: 16384,
		10219520: 20480,
		34603008: 69632,
		47185920: 69632,
	}

	for addr, len_ := range addrWithLen {
		wg.Add(1)
		go func(_addr int64, _len int) {
			defer wg.Done()
			_buf := make([]byte, _len)
			_, er := q.ReadAt(_buf, _addr)
			if er != nil {
				logger.Fatalf("ReadAt(off=%v): %v", _addr, er)
			}
			logger.Infof("read %v bytes from %v ok", _len, _addr)
		}(addr, len_)

		wg.Add(1)
		go func(_addr int64, _len int) {
			defer wg.Done()
			_buf := make([]byte, _len)
			_, er := q.WriteAt(_buf, _addr)
			if er != nil {
				logger.Fatalf("WriteAt(off=%v): %v", _addr, er)
			}
			logger.Infof("write %v bytes at %v ok", _len, _addr)
		}(addr, len_)
	}

	wg.Wait()
	logger.Infof("finished")
}
