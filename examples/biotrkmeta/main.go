package main

import (
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log"
	"os"

	"github.com/kisun-bit/drpkg/extend"
)

type Header struct {
	Signature            [16]byte
	MaxProtectedDisks    uint32
	MaxBitCountPerBitmap uint32
	BytesPerBit          uint32
	FirstBitmapStart     uint32
	DriverErrorCode      uint32
}

func writeHeaderToMeta(metaFile string) error {
	var hdr Header
	copy(hdr.Signature[:], "BIOTRKMETA      ")
	hdr.MaxProtectedDisks = 64
	hdr.MaxBitCountPerBitmap = 16777216
	hdr.BytesPerBit = 4194304
	hdr.FirstBitmapStart = 1048576
	hdr.DriverErrorCode = 0

	f, err := os.OpenFile(metaFile, os.O_RDWR|os.O_SYNC, 0666)
	if err != nil {
		return fmt.Errorf("open meta file failed: %v", err)
	}
	defer f.Close()
	defer f.Sync()

	if err = binary.Write(f, binary.LittleEndian, &hdr); err != nil {
		return fmt.Errorf("write header failed: %v", err)
	}
	return nil
}

func main() {
	metaFile := os.Args[1]
	metaFileSize := 129 << 20

	if err := extend.CreateHiddenFile(metaFile, int64(metaFileSize), true); err != nil {
		log.Fatalf("create hidden file failed: %v", err)
	}
	if err := writeHeaderToMeta(metaFile); err != nil {
		log.Fatalf("write header failed: %v", err)
	}

	f, err := os.OpenFile(metaFile, os.O_RDONLY, 0)
	if err != nil {
		log.Fatalf("open meta file failed: %v", err)
	}
	defer f.Close()
	originMd5sum, err := extend.Md5sum(f)
	if err != nil {
		log.Fatalf("md5sum origin file: %v", err)
	}
	fmt.Printf("originMd5sum:%s\n", originMd5sum)

	h := md5.New()
	n, err := extend.CopyFileByDiskExtents(metaFile, h)
	if err != nil {
		log.Fatalf("copy file failed: %v", err)
	}

	fmt.Printf("targetMd5sum:%s, size: %v\n", hex.EncodeToString(h.Sum(nil)), n)
	fmt.Println("success")
}
