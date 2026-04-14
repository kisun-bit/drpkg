package scan

import (
	"os"
	"runtime"

	"golang.org/x/sys/unix"
)

func fadvise(f *os.File) {
	if runtime.GOOS != "linux" {
		return
	}

	fd := int(f.Fd())

	err := unix.Fadvise(fd, 0, 0, unix.FADV_SEQUENTIAL)
	if err != nil {
		panic(err)
	}

	err = unix.Fadvise(fd, 0, 0, unix.FADV_NOREUSE)
}
