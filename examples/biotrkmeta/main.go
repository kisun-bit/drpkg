package main

import (
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"

	"github.com/kisun-bit/drpkg/extend"
	"github.com/lunixbochs/struc"
	"github.com/pkg/errors"
)

const (
	SIZEOFMETADATA = int64(129 << 20)
	MAGIC          = "BIOTRKMETA      "
)

type Header struct {
	Signature            [16]byte      `struc:"little"`
	State                uint32        `struc:"little"`
	MaxProtectedDisks    uint32        `struc:"little"`
	MaxBitCountPerBitmap uint32        `struc:"little"`
	BytesPerBit          uint32        `struc:"little"`
	FirstBitmapStart     uint64        `struc:"little"`
	DriverErrorCode      uint32        `struc:"little"`
	Reversed             [1048712]byte `struc:"little"`
}

type MetadataRegions struct {
	Count   uint32           `struc:"little,sizeof=Regions"`
	Regions []MetadataRegion `struc:"little"`
}

type MetadataRegion struct {
	DiskID uint32 `struc:"little"`
	Start  uint64 `struc:"little"`
	Size   uint64 `struc:"little"`
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
	rand.Read(buf)

	header := DefaultHeader()
	headerBuf := new(bytes.Buffer)

	_ = os.Remove(path)
	f, err := os.OpenFile(path, os.O_CREATE|extend.W_DSYNC_MODE, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if err = struc.Pack(headerBuf, &header); err != nil {
		return err
	}
	headerBytes := headerBuf.Bytes()

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
		nw, er := f.Write(b[:toWrite])
		if er != nil {
			return er
		}
		remaining -= int64(nw)
	}

	if err = f.Sync(); err != nil {
		return err
	}

	if err = customFile(path); err != nil {
		return err
	}

	return nil
}

func Validate(metaFile string) (err error) {
	f, err := os.Open(metaFile)
	if err != nil {
		return err
	}
	defer f.Close()

	fileMd5, err := extend.FileMd5sum(f)
	if err != nil {
		return err
	}
	diskMd5Hasher := md5.New()

	if _, err = extend.CopyFileByDiskExtents(metaFile, diskMd5Hasher); err != nil {
		return err
	}
	diskMd5 := hex.EncodeToString(diskMd5Hasher.Sum(nil))

	fmt.Println("file-hash: ", fileMd5)
	fmt.Println("disk-hash: ", diskMd5)

	if fileMd5 != diskMd5 {
		return errors.New("file md5 does not match disk md5 hash")
	}
	return nil
}

func main() {
	metaFile := os.Args[1]

	fmt.Printf("Disable VSS...\n")
	if err := DisableVSS(); err != nil {
		log.Fatal("Failed to disable VSS: ", err)
	}

	fmt.Printf("Initialize cdp meta file...\n")
	if err := InitializeCdpMetaFile(metaFile); err != nil {
		log.Fatalf("Failed to create hidden file: %v", err)
	}

	fmt.Printf("Validate cdp meta file...\n")
	if err := Validate(metaFile); err != nil {
		log.Fatalf("Failed to validate cdp meta file: %v", err)
	}

	fmt.Printf("Configure registry...\n")
	if err := ConfigRegistry(metaFile); err != nil {
		log.Fatal("Failed to configure registry: ", err)
	}

	fmt.Print("success")
}
