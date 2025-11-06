package main

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"syscall"

	"github.com/kisun-bit/drpkg/extend"
	"github.com/lunixbochs/struc"
	"github.com/pkg/errors"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
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

type MetadataRegions struct {
	Count   uint32 `struc:"sizeof=Regions"`
	Regions []MetadataRegion
}

type MetadataRegion struct {
	DiskID uint32
	Start  uint64
	Size   uint64
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

func DisableVSS() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	srv, err := m.OpenService("vss")
	if err != nil {
		return err
	}
	defer srv.Close()

	_, err = srv.Control(svc.Stop)
	if err != nil && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
		return err
	}

	oldCfg, err := srv.Config()
	if err != nil {
		return err
	}
	if oldCfg.StartType == windows.SERVICE_DISABLED {
		return nil
	}
	oldCfg.StartType = windows.SERVICE_DISABLED

	if err = srv.UpdateConfig(oldCfg); err != nil {
		return err
	}

	return nil
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

func ConfigRegistry(metaFile string) error {
	k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, "SYSTEM\\CurrentControlSet\\Services\\biotrk\\Parameters", registry.ALL_ACCESS)
	if err != nil {
		return err
	}
	defer k.Close()

	var meta MetadataRegions
	es, err := extend.FileDiskExtents(metaFile)
	if err != nil {
		return err
	}
	for _, v := range es {
		meta.Count++
		id, e := extend.WindowsDiskIDFromPath(v.Disk)
		if e != nil {
			return e
		}
		meta.Regions = append(meta.Regions, MetadataRegion{
			DiskID: id,
			Start:  uint64(v.Start),
			Size:   uint64(v.Size),
		})
	}

	var buf bytes.Buffer
	if err = struc.Pack(&buf, &meta); err != nil {
		return err
	}

	if err = k.SetBinaryValue("fragments", buf.Bytes()); err != nil {
		return err
	}

	return nil
}

func main() {
	metaFile := os.Args[1]

	fmt.Printf("[s1] Disable VSS\n")
	if err := DisableVSS(); err != nil {
		log.Fatal("Failed to disable VSS: ", err)
	}

	fmt.Printf("[s2] Initialize cdp meta file\n")
	if err := InitializeCdpMetaFile(metaFile); err != nil {
		log.Fatalf("Failed to create hidden file: %v", err)
	}

	fmt.Printf("[s3] Configure registry\n")
	if err := ConfigRegistry(metaFile); err != nil {
		log.Fatal("Failed to configure registry: ", err)
	}

	fmt.Println("success")
}
