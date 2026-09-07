package meta

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"github.com/pkg/errors"
)

const (
	W_DSYNC_MODE = os.O_WRONLY | os.O_SYNC
	R_DSYNC_MODE = os.O_RDONLY | os.O_SYNC
)

const (
	windowsDiskObjPrefix         = `\\.\PHYSICALDRIVE`
	windowsVolumeShadowObjPrefix = `\\?\GLOBALROOT\DEVICE\HARDDISKVOLUMESHADOWCOPY`
	windowsVolumeObjPrefix       = `\\?\VOLUME`
)

type DiskExtent struct {
	DiskNumber     uint32
	StartingOffset uint64
	ExtentLength   uint64
}

type DiskExtents struct {
	NumberOfExtents uint32
	Padding         uint32
	Extents         [128]DiskExtent
}

func customFileAttr(path string) error {
	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	if err = syscall.SetFileAttributes(ptr, syscall.FILE_ATTRIBUTE_SYSTEM|syscall.FILE_ATTRIBUTE_HIDDEN); err != nil {
		return err
	}
	return nil
}

func WindowsDiskPathFromID(id uint32) string {
	return fmt.Sprintf("%s%v", windowsDiskObjPrefix, id)
}

func IsWindowsVolume(path string) bool {
	upperPath := strings.ToUpper(path)
	return strings.HasSuffix(path, `:\`) || strings.HasPrefix(upperPath, windowsVolumeObjPrefix)
}

func VolumeMountpointUNC(volumeMountpoint string) string {
	if IsWindowsVolume(volumeMountpoint) {
		return strings.TrimSuffix(volumeMountpoint, "\\")
	}
	return `\\.\` + volumeMountpoint
}

func GetClusterSize(drive string) (int64, error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	procGetDiskFreeSpaceW := kernel32.NewProc("GetDiskFreeSpaceW")

	var sectorsPerCluster, bytesPerSector, numberOfFreeClusters, totalNumberOfClusters uint32

	// Drive字符串需要以null结尾，例如"C:\\"
	drivePtr, err := syscall.UTF16PtrFromString(drive)
	if err != nil {
		return 0, err
	}

	r1, _, err := procGetDiskFreeSpaceW.Call(
		uintptr(unsafe.Pointer(drivePtr)),
		uintptr(unsafe.Pointer(&sectorsPerCluster)),
		uintptr(unsafe.Pointer(&bytesPerSector)),
		uintptr(unsafe.Pointer(&numberOfFreeClusters)),
		uintptr(unsafe.Pointer(&totalNumberOfClusters)),
	)

	if r1 == 0 {
		return 0, fmt.Errorf("GetDiskFreeSpaceW failed: %v", err)
	}

	clusterSize := int64(sectorsPerCluster) * int64(bytesPerSector)
	return clusterSize, nil
}

func VolumeMountpointToExtents(volumeMountpoint string) ([]DiskExtent, error) {
	name, err := syscall.UTF16PtrFromString(VolumeMountpointUNC(volumeMountpoint))
	if err != nil {
		return nil, err
	}
	drive, err := syscall.CreateFile(name,
		syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil,
		syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		return []DiskExtent{}, err
	}
	var bytesreturned uint32
	var extents DiskExtents
	err = syscall.DeviceIoControl(drive, 0x00560000, nil, 0,
		(*byte)(unsafe.Pointer(&extents)), uint32(unsafe.Sizeof(extents)), &bytesreturned, nil)
	runtime.KeepAlive(bytesreturned)

	if err != nil {
		return []DiskExtent{}, err
	}
	return extents.Extents[0:extents.NumberOfExtents], nil
}

func DeviceMajorTable(filter ...string) (map[DevMajor]string, error) {
	_ = filter
	return nil, errors.New("device major table is not yet supported")
}
