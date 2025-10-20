package main

import (
	"io"
	"os"

	"github.com/kisun-bit/drpkg/disk/image"
	"github.com/kisun-bit/drpkg/logger"
)

func main() {
	if err := image.QemuToolDirSetup(os.Args[1]); err != nil {
		logger.Fatal("QemuToolDirSetup: ", err)
	}

	img, err := image.Open(os.Args[2])
	if err != nil {
		logger.Fatal("Open: ", err)
	}
	defer img.Close()

	bufLen := 4 << 20
	buf := make([]byte, bufLen)
	off := int64(0)
	for {
		nr, er := img.ReadAt(buf, off)
		if er != nil && er != io.EOF {
			logger.Fatal("ReadAt: ", er)
		}
		if er == io.EOF || nr == 0 {
			break
		}
		off += int64(nr)
	}

	logger.Debugf("Read: %d", off)
}
