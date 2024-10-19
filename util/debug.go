package util

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"
	"sync"

	"github.com/davecgh/go-spew/spew"
)

func PrintStack() {
	debug.PrintStack()
}

func Debug(arg interface{}) {
	spew.Dump(arg)
}

func DlvBreak() {
	if false {
		fmt.Printf("Break")
		PrintStack()
	}
}

var (
	debugToFileMu sync.Mutex
)

func DebugToFile(filename, format string, v ...interface{}) {
	debugToFileMu.Lock()
	defer debugToFileMu.Unlock()

	fd, err := os.OpenFile(filename, os.O_RDWR|os.O_CREATE, 0700)
	if err != nil {
		panic(err)
	}
	defer fd.Close()

	fd.Seek(0, os.SEEK_END)
	fd.Write([]byte(fmt.Sprintf(format, v...) + "\n"))
}

type DebugStringer interface {
	DebugString() string
}

func DebugString(v interface{}) string {
	switch t := v.(type) {
	case DebugStringer:
		return t.DebugString()

	default:
		return fmt.Sprintf("%T %v", v, v)
	}
}

func DebugCtx(ctx context.Context, name string) {
	select {
	case <-ctx.Done():
		fmt.Printf(name + ": Ctx is done!\n")
	default:
		fmt.Printf(name + ": Ctx is still valid!\n")
	}
}

func DebugLogWhenCtxDone(ctx context.Context, name string) {
	go func() {
		<-ctx.Done()
		fmt.Printf(name + ": Ctx done!\n")
	}()
}
