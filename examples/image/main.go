//go:build linux

package main

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/dustin/go-humanize"
	"github.com/kisun-bit/drpkg/disk/image"
	"github.com/kisun-bit/drpkg/logger"
)

//func DemoWrite(enableHash bool) {
//	hash := md5.New()
//
//	logger.Debugf("origin file: %s", os.Args[2])
//	logger.Debugf("target file: %s", os.Args[3])
//
//	origin, _ := os.Open(os.Args[2])
//	defer origin.Close()
//
//	img, err := image.Open(os.Args[3], image.EnableNoFlush())
//	if err != nil {
//		logger.Fatal("Open: ", err)
//	}
//	defer img.Close()
//
//	bufLen := 2 << 20
//	buf := make([]byte, bufLen)
//	off := int64(0)
//	go func() {
//		du := 5 * time.Second
//		tik := time.NewTicker(du)
//		lastBytes := int64(0)
//		defer tik.Stop()
//		for range tik.C {
//			curBytes := off
//			duRBytes := curBytes - lastBytes
//			curSpeed := uint64(float64(duRBytes) * 1000 / float64(du.Milliseconds()))
//			lastBytes = curBytes
//			fmt.Printf("%vB (%s), read %vB (%s) in %s, speed: %v/s\n", curBytes, humanize.IBytes(uint64(curBytes)),
//				duRBytes, humanize.IBytes(uint64(duRBytes)), du.String(), humanize.IBytes(curSpeed))
//		}
//	}()
//	for {
//		nr, er := origin.ReadAt(buf, off)
//		if er != nil && er != io.EOF {
//			logger.Fatal("ReadAt: ", er)
//		}
//		if nr > 0 {
//			if _, ew := img.WriteAt(buf[:nr], off); ew != nil {
//				logger.Error("WriteAt: ", ew)
//				break
//			}
//			if enableHash {
//				_, _ = hash.Write(buf[:nr])
//			}
//			off += int64(nr)
//		}
//		if er == io.EOF {
//			break
//		}
//	}
//
//	output := fmt.Sprintf("Written: %d", off)
//	if enableHash {
//		output += fmt.Sprintf(" md5: %v", hex.EncodeToString(hash.Sum(nil)))
//	}
//	logger.Debugf(output)
//}

func WriteProf(enableHash bool) {
	hash := md5.New()

	logger.Debugf("target file: %s", os.Args[2])

	img, err := image.Open(os.Args[2], image.EnableNoFlush())
	if err != nil {
		logger.Fatal("Open: ", err)
	}
	defer img.Close()

	bufLen := 2 << 20
	buf := make([]byte, bufLen)
	rand.Read(buf)
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
		if _, ew := img.WriteAt(buf, off); ew != nil {
			logger.Error("WriteAt: ", ew)
			break
		}
		if enableHash {
			_, _ = hash.Write(buf)
		}
		off += int64(len(buf))
	}

	output := fmt.Sprintf("Written: %d", off)
	if enableHash {
		output += fmt.Sprintf(" md5: %v", hex.EncodeToString(hash.Sum(nil)))
	}
	logger.Debugf(output)
}

//func DemoRead(enableHash bool) {
//	hash := md5.New()
//
//	logger.Debugf("origin file: %s", os.Args[2])
//
//	img, err := image.Open(os.Args[2])
//	if err != nil {
//		logger.Fatal("Open: ", err)
//	}
//	defer img.Close()
//
//	bufLen := 4 << 20
//	buf := make([]byte, bufLen)
//	off := int64(0)
//	go func() {
//		du := 5 * time.Second
//		tik := time.NewTicker(du)
//		lastBytes := int64(0)
//		defer tik.Stop()
//		for range tik.C {
//			curBytes := off
//			duRBytes := curBytes - lastBytes
//			curSpeed := uint64(float64(duRBytes) * 1000 / float64(du.Milliseconds()))
//			lastBytes = curBytes
//			fmt.Printf("%vB (%s), read %vB (%s) in %s, speed: %v/s\n", curBytes, humanize.IBytes(uint64(curBytes)),
//				duRBytes, humanize.IBytes(uint64(duRBytes)), du.String(), humanize.IBytes(curSpeed))
//		}
//	}()
//	for {
//		nr, er := img.ReadAt(buf, off)
//		if er != nil && er != io.EOF {
//			logger.Error("ReadAt: ", er)
//			return
//		}
//		if nr > 0 {
//			if enableHash {
//				_, _ = hash.Write(buf[:nr])
//			}
//			off += int64(nr)
//		}
//		if er == io.EOF || nr == 0 {
//			break
//		}
//	}
//
//	output := fmt.Sprintf("Read: %d", off)
//	if enableHash {
//		output += fmt.Sprintf(" md5: %v", hex.EncodeToString(hash.Sum(nil)))
//	}
//	logger.Debugf(output)
//}

func DemoImageMap() {
	imi, err := image.Map(context.Background(), os.Args[2])
	if err != nil {
		logger.Fatal(err)
	}
	spew.Dump(imi)
}

func main() {
	//defer time.Sleep(time.Hour)
	if err := image.QemuToolDirSetup(os.Args[1]); err != nil {
		logger.Error("QemuToolDirSetup: ", err)
	}
	//DemoRead(false)
	//DemoWrite(false)
	//DemoImageMap()

	WriteProf(false)
}
