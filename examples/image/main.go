package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/dustin/go-humanize"
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
	go func() {
		du := 5 * time.Second
		tik := time.NewTicker(du)
		lastBytes := int64(0)
		defer tik.Stop()
		for range tik.C {
			curBytes := off
			duRBytes := curBytes - lastBytes
			curSpeed := uint64(float64(duRBytes) * 1000 / float64(du.Milliseconds()))
			lastBytes = curBytes
			fmt.Printf("%vB (%s), read %vB (%s) in %s, speed: %v/s\n", curBytes, humanize.IBytes(uint64(curBytes)),
				duRBytes, humanize.IBytes(uint64(duRBytes)), du.String(), humanize.IBytes(curSpeed))
		}
	}()
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
