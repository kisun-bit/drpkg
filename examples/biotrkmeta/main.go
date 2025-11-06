package main

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"io"
	"log"
	"os"
	"syscall"

	"github.com/kisun-bit/drpkg/extend"
	"github.com/pkg/errors"
)

const (
	SIZEOFMETADATA = int64(129 << 20)
	MAGIC          = "BIOTRKMETA      "
)

type Header struct {
	Signature            [16]byte
	MaxProtectedDisks    uint32
	MaxBitCountPerBitmap uint32
	BytesPerBit          uint32
	FirstBitmapStart     uint64
	DriverErrorCode      uint32
	Reversed             [1048716]byte
}

func DefaultHeader() (hdr Header) {
	copy(hdr.Signature[:], MAGIC)
	hdr.MaxProtectedDisks = 1 << 6
	hdr.MaxBitCountPerBitmap = 16 << 20
	hdr.BytesPerBit = 4 << 20
	hdr.FirstBitmapStart = 1 << 20
	copy(hdr.Reversed[:], make([]byte, len(hdr.Reversed)))
	return hdr
}

func InitializeCdpMetaFile(path string) (err error) {
	chunkSize := int64(1 << 20)
	buf := make([]byte, chunkSize)

	header := DefaultHeader()
	headerBuf := new(bytes.Buffer)

	fileHasher := md5.New()
	diskHasher := md5.New()

	_ = os.Remove(path)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_TRUNC|os.O_SYNC, 0666)
	if err != nil {
		return err
	}
	defer f.Close()

	if err = binary.Write(headerBuf, binary.LittleEndian, &header); err != nil {
		return err
	}
	headerBytes := headerBuf.Bytes()

	fileHashReader := io.MultiWriter(f, fileHasher)
	remaining := SIZEOFMETADATA
	for remaining > 0 {
		b := buf
		if remaining == SIZEOFMETADATA {
			b = headerBytes
		}
		toWrite := len(b)
		if remaining < chunkSize {
			toWrite = int(remaining)
		}
		nw, er := fileHashReader.Write(b[:toWrite])
		if er != nil {
			return er
		}
		remaining -= int64(nw)
	}

	if err = f.Sync(); err != nil {
		return err
	}

	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	if err = syscall.SetFileAttributes(ptr, syscall.FILE_ATTRIBUTE_SYSTEM|syscall.FILE_ATTRIBUTE_HIDDEN); err != nil {
		return err
	}

	if _, err = extend.CopyFileByDiskExtents(path, diskHasher); err != nil {
		return err
	}
	if !bytes.Equal(fileHasher.Sum(nil), diskHasher.Sum(nil)) {
		return errors.New("hash mismatch")
	}

	return nil
}

func main() {
	metaFile := os.Args[1]

	if err := InitializeCdpMetaFile(metaFile); err != nil {
		log.Fatalf("create hidden file failed: %v", err)
	}
}
