package extend

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

func GetFileSize(fileName string) (size uint64, err error) {
	var errno syscall.Errno
	info, err := os.Stat(fileName)
	if err != nil {
		return 0, err
	}
	fm := info.Mode()
	if fm&os.ModeDevice != 0 {
		f, err := os.Open(fileName)
		if err != nil {
			return 0, err
		}
		defer f.Close()

		if runtime.GOARCH == "386" {
			_, _, errno = unix.Syscall(unix.SYS_IOCTL, f.Fd(), LinuxIOCTLGetBlockSize, uintptr(unsafe.Pointer(&size)))
			size <<= 9
		} else {
			_, _, errno = unix.Syscall(unix.SYS_IOCTL, f.Fd(), LinuxIOCTLGetBlockSize64, uintptr(unsafe.Pointer(&size)))
		}
		if errno != 0 {
			return 0, errno
		}
		return size, nil
	} else {
		return uint64(info.Size()), nil
	}
}

func MatchDevLinkName(base string, deviceName string) string {
	if IsExisted(base) {
		files, err := os.ReadDir(base)
		if err != nil {
			return ""
		}
		for _, file := range files {
			filename := file.Name()
			path := filepath.Join(base, filename)
			linkTarget, err := filepath.EvalSymlinks(path)
			if err != nil {
				return ""
			}
			if linkTarget == filepath.Join("/dev", deviceName) {
				return filename
			}
		}
	}
	return ""
}

func DevBlockSize(dev string) (uint32, error) {
	fd, err := os.OpenFile(dev, os.O_RDONLY, 0600)
	if err != nil {
		return 0, fmt.Errorf("fail to open %s: %w", dev, err)
	}
	defer fd.Close()

	var blksize uint32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd.Fd(), uintptr(LinuxIOCTLGetBLKBSZ), uintptr(unsafe.Pointer(&blksize)))
	if errno != 0 {
		return 0, os.NewSyscallError("ioctl", errno)
	}
	return blksize, nil
}

func DevPhysicalBlockSize(dev string) (uint32, error) {
	fd, err := os.OpenFile(dev, os.O_RDONLY, 0600)
	if err != nil {
		return 0, fmt.Errorf("fail to open %s: %w", dev, err)
	}
	defer fd.Close()

	var physicalBlksize uint32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd.Fd(), uintptr(LinuxIOCTLGetBLKPBSZ), uintptr(unsafe.Pointer(&physicalBlksize)))
	if errno != 0 {
		return 0, os.NewSyscallError("ioctl", errno)
	}
	return physicalBlksize, nil
}
