//go:build linux

package extend

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"github.com/pkg/errors"
)

//
// 见：https://www.kernel.org/doc/Documentation/filesystems/fiemap.txt
//

const (
	IOCTL_FIEMAP = 0xc020660b

	sizeOfFiemapStruc       = 32
	sizeOfFiemapExtentStruc = 56

	FILEMAP_FLAG_SYNC  = 0x0001
	FIEMAP_EXTENT_LAST = 0x0001
)

type fiemap struct {
	start         uint64
	length        uint64
	flags         uint32
	mappedExtents uint32
	extentCount   uint32
}

type fiemapExtent struct {
	Logical   uint64 // fe_logical
	Physical  uint64 // fe_physical
	Length    uint64 // fe_length
	reserved1 uint64
	reserved2 uint64
	Flags     uint32 // fe_flags, FIEMAP_EXTENT_* flags for this extent
}

func ioctlFileMap(file *os.File, start uint64, length uint64) ([]fiemapExtent, bool, error) {
	if length == 0 {
		return nil, true, nil
	}

	extentCount := uint32(50)
	buf := make([]byte, sizeOfFiemapStruc+extentCount*sizeOfFiemapExtentStruc)
	fm := (*fiemap)(unsafe.Pointer(&buf[0]))
	fm.start = start
	fm.length = length
	fm.flags = FILEMAP_FLAG_SYNC
	fm.extentCount = extentCount
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), IOCTL_FIEMAP, uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return nil, true, fmt.Errorf("fiemap errno %v", errno)
	}

	extents := make([]fiemapExtent, fm.mappedExtents)
	done := fm.mappedExtents == 0
	lastOffs := start
	for i := uint32(0); i < fm.mappedExtents; i++ {
		rawinfo := (*fiemapExtent)(unsafe.Pointer(uintptr(unsafe.Pointer(&buf[0])) + uintptr(sizeOfFiemapStruc) + uintptr(i*sizeOfFiemapExtentStruc)))
		if rawinfo.Logical < lastOffs {
			return nil, true, fmt.Errorf("invalid order %v", rawinfo.Logical)
		}
		lastOffs = rawinfo.Logical
		extents[i].Logical = rawinfo.Logical
		extents[i].Physical = rawinfo.Physical
		extents[i].Length = rawinfo.Length
		extents[i].Flags = rawinfo.Flags
		done = rawinfo.Flags&FIEMAP_EXTENT_LAST != 0
	}

	return extents, done, nil
}

func getFileExtentsFp(file *os.File) ([]fiemapExtent, error) {
	fileInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}

	var all []fiemapExtent
	start := uint64(0)
	size := uint64(fileInfo.Size())
	for {
		part, done, err := ioctlFileMap(file, start, size-start)
		if err != nil {
			return nil, err
		}

		all = append(all, part...)
		if done {
			return all, nil
		}

		if len(part) == 0 {
			return nil, errors.New("unsupported")
		}
		last := part[len(part)-1]
		start = last.Logical + last.Length
	}
}
