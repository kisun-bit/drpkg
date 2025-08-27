package others

import (
	"os"
	"path/filepath"
	"unsafe"

	"github.com/pkg/errors"
	"golang.org/x/sys/windows"
)

func GetFileSize(fileName string) (uint64, error) {
	switch {
	case IsWindowsVolumeShadow(fileName):
		// 注意：VSS 路径需要补两个分隔符，否则 GetDiskFreeSpaceEx 会失败
		vssAbsPath := fileName + string(filepath.Separator) + string(filepath.Separator)
		return getVolumeTotalSize(vssAbsPath)

	case IsWindowsDisk(fileName):
		return getPhysicalDiskSize(fileName)

	case IsWindowsVolume(fileName):
		return getVolumeTotalSize(fileName)

	default:
		// 普通文件
		stat, err := os.Lstat(fileName)
		if err != nil {
			return 0, errors.Wrapf(err, "os.Lstat %q", fileName)
		}
		return uint64(stat.Size()), nil
	}
}

// 获取卷（含 VSS/Volume）的总容量
func getVolumeTotalSize(path string) (uint64, error) {
	var total, free, ava uint64
	if err := windows.GetDiskFreeSpaceEx(
		windows.StringToUTF16Ptr(path),
		&ava, &total, &free,
	); err != nil {
		return 0, errors.Wrapf(err, "GetDiskFreeSpaceEx %q", path)
	}
	return total, nil
}

// 获取物理磁盘的总容量
func getPhysicalDiskSize(device string) (uint64, error) {
	handle, err := OpenDevice(device)
	if err != nil {
		return 0, errors.Wrapf(err, "OpenDevice %q", device)
	}
	defer windows.CloseHandle(handle)

	var lenSize uint32
	var info struct {
		Length int64
	}

	err = windows.DeviceIoControl(
		handle,
		IOCTL_DISK_GET_LENGTH_INFO,
		nil,
		0,
		(*byte)(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
		&lenSize,
		nil,
	)
	if err != nil {
		return 0, errors.Wrapf(err, "DeviceIoControl %q", device)
	}
	return uint64(info.Length), nil
}

func OpenDevice(path string) (windows.Handle, error) {
	sPath, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return windows.CreateFile(
		sPath,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		0,
		0,
	)
}
