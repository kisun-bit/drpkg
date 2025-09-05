package extend

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"github.com/lunixbochs/struc"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
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

func MatchDevLinkName(_ string, _ string) string {
	return ""
}

// VolumeMountpoints 获取所有的卷装入点（格式如："C:"）.
func VolumeMountpoints() (volumeMountpoints []string, err error) {
	buf := make([]uint16, 254)
	n, e := windows.GetLogicalDriveStrings(254, &buf[0])
	if err != nil {
		return nil, e
	}
	for _, v := range buf[:n] {
		letter := string(rune(v))
		if len(letter) == 0 {
			continue
		}
		if letter[0] <= 'A' || letter[0] > 'Z' {
			continue
		}
		volumeMountpoints = append(volumeMountpoints, letter+":")
	}
	return volumeMountpoints, nil
}

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
	err = syscall.DeviceIoControl(drive, IOCTL_VOLUME_GET_VOLUME_DISK_EXTENTS, nil, 0,
		(*byte)(unsafe.Pointer(&extents)), uint32(unsafe.Sizeof(extents)), &bytesreturned, nil)
	runtime.KeepAlive(bytesreturned)

	if err != nil {
		return []DiskExtent{}, err
	}
	return extents.Extents[0:extents.NumberOfExtents], nil
}

// VolumeUsageInfo 通过卷名查询其磁盘使用情况.
func VolumeUsageInfo(volumeMountpoint string) (available, total, free uint64, err error) {
	err = windows.GetDiskFreeSpaceEx(windows.StringToUTF16Ptr(volumeMountpoint+"\\"), &available, &total, &free)
	return
}

type PARTITION_INFORMATION_EX struct {
	PartitionStyle  uint32 `struc:"uint64,little"`
	StartingOffset  int64  `struc:"little"`
	PartitionLength int64  `struc:"little"`
	PartitionNumber uint32 `struc:"uint64,little"`
}

// PartitionInformationByVolume 获取指定卷的PARTITION_INFORMATION_EX信息
func PartitionInformationByVolume(volumeMountpoint string) (pi PARTITION_INFORMATION_EX, err error) {
	handle, err := windows.CreateFile(
		windows.StringToUTF16Ptr(VolumeMountpointUNC(volumeMountpoint)),
		windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		0,
		0)
	if err != nil {
		return PARTITION_INFORMATION_EX{}, err
	}
	defer func() {
		_ = windows.CloseHandle(handle)
	}()

	var dgSize uint32
	tmpLen := 16 << 10
	ioctlBuf := make([]byte, tmpLen)
	err = windows.DeviceIoControl(
		handle,
		IOCTL_DISK_GET_PARTITION_INFO_EX,
		nil,
		0,
		&ioctlBuf[0],
		uint32(tmpLen),
		&dgSize,
		nil)
	if err != nil {
		return PARTITION_INFORMATION_EX{}, fmt.Errorf("failed to call IOCTL_DISK_GET_PARTITION_INFO_EX for %s, %v", volumeMountpoint, err)
	}
	err = struc.UnpackWithOptions(bytes.NewReader(ioctlBuf), &pi, &struc.Options{Order: binary.LittleEndian})
	if err != nil {
		err = fmt.Errorf("failed to unpack partition info raw, %v", err)
	}
	return pi, err
}

func VolumeMountpointUNC(volumeMountpoint string) string {
	return `\\.\` + volumeMountpoint
}

func VolumeName(volumeMountpoint string) (string, error) {
	name, err := windows.UTF16PtrFromString(volumeMountpoint + "\\")
	if err != nil {
		return "", err
	}
	buf := make([]uint16, 1<<10)
	if err = windows.GetVolumeNameForVolumeMountPoint(name, &buf[0], uint32(len(buf))); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buf), nil
}

// VolumeExtraInfo 获取卷的名称、唯一编号及文件系统类型.
func VolumeExtraInfo(volumeMountpoint string) (label string, fs_ string, uuid string, err error) {
	lpVolumeSerialNumber := uint32(0)
	lpMaximumComponentLength := uint32(0)
	lpFileSystemFlags := uint32(0)

	lpVolumeNameBuffer := make([]uint16, windows.MAX_PATH+1)
	lpFileSystemNameBuffer := make([]uint16, windows.MAX_PATH+1)

	path, err := windows.UTF16PtrFromString(volumeMountpoint + "\\")
	if err != nil {
		return "", "", "", err
	}

	err = windows.GetVolumeInformation(
		path,
		&lpVolumeNameBuffer[0],
		uint32(len(lpVolumeNameBuffer)),
		&lpVolumeSerialNumber,
		&lpMaximumComponentLength,
		&lpFileSystemFlags,
		&lpFileSystemNameBuffer[0],
		uint32(len(lpFileSystemNameBuffer)))

	uniqID := fmt.Sprintf("%X", lpVolumeSerialNumber)
	volName, _ := VolumeName(volumeMountpoint) // volName 形如: `\\?\Volume{e3b9397c-0000-0000-0000-100000000000}\`
	if IsWindowsVolume(volName) {
		uniqID = strings.TrimSuffix(strings.TrimPrefix(volName, `\\?\Volume{`), `}\`)
	}
	if err != nil {
		if uniqID != "" {
			err = nil
		}
		return "", "RAW", uniqID, err
	}

	label = syscall.UTF16ToString(lpVolumeNameBuffer)
	fs_ = syscall.UTF16ToString(lpFileSystemNameBuffer)
	return label, fs_, uniqID, nil
}

type WIN_STORAGE_BUS_TYPE byte

func (busType WIN_STORAGE_BUS_TYPE) String() string {
	switch busType {
	case BusTypeUnknown:
		return "Unknown"
	case BusTypeScsi:
		return "Scsi"
	case BusTypeAtapi:
		return "Atapi" // 光学设备如CD
	case BusTypeAta:
		return "Ata"
	case BusType1394:
		return "1394"
	case BusTypeSsa:
		return "Ssa"
	case BusTypeFibre:
		return "Fibre"
	case BusTypeUsb:
		return "Usb"
	case BusTypeRAID:
		return "RAID"
	case BusTypeiScsi:
		return "iScsi"
	case BusTypeSas:
		// SCSI 设备的一种类型，其中 SAS（Serial Attached SCSI，串行连接 SCSI）是其中之一,
		// SAS 是一种用于连接服务器和存储设备的高速、可靠的接口标准。它是 SCSI 标准的一种延伸，
		// 采用了串行连接的方式，提供了更高的性能和可靠性.
		return "Sas"
	case BusTypeSata:
		return "Sata"
	case BusTypeSd:
		return "Sd"
	case BusTypeMmc:
		return "Mmc"
	case BusTypeVirtual:
		return "Virtual"
	case BusTypeFileBackedVirtual:
		return "FileBackedVirtual"
	case BusTypeSpaces:
		return "Spaces"
	case BusTypeNvme:
		return "Nvme"
	case BusTypeSCM:
		return "SCM"
	case BusTypeUfs:
		return "Ufs"
	default:
		return ""
	}
}

type StorageDeviceDescription struct {
	DeviceType         byte
	DeviceTypeModifier byte
	RemovableMedia     bool
	VendorId           string
	ProductId          string
	ProductRevision    string
	SerialNumber       string
	BusType            WIN_STORAGE_BUS_TYPE
}

type STORAGE_DEVICE_DESCRIPTOR struct {
	Version               uint32
	Size                  uint32
	DeviceType            byte
	DeviceTypeModifier    byte
	RemovableMedia        bool
	CommandQueueing       bool
	VendorIdOffset        uint32
	ProductIdOffset       uint32
	ProductRevisionOffset uint32
	SerialNumberOffset    uint32
	BusType               WIN_STORAGE_BUS_TYPE
	RawPropertiesLength   uint32
}

type STORAGE_PROPERTY_QUERY_WITH_DUMMY struct {
	// PropertyId 对应winioctl.h中的STORAGE_PROPERTY_ID.
	PropertyId uint32
	// QueryType 对应winioctl.h中的STORAGE_QUERY_TYPE. 各个枚举值见 https://learn.microsoft.com/en-us/windows/win32/api/winioctl/ne-winioctl-storage_property_id
	QueryType            uint32
	AdditionalParameters [1]byte
}

// DiskProperty 查询硬盘配置空间特征信息.
func DiskProperty(physicalDrivePath string) (*StorageDeviceDescription, error) {
	handle, err := OpenDevice(physicalDrivePath)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = windows.CloseHandle(handle)
	}()

	// 参考: https://learn.microsoft.com/en-us/windows/win32/api/winioctl/ns-winioctl-storage_property_query
	query := STORAGE_PROPERTY_QUERY_WITH_DUMMY{}
	query.QueryType = 0
	query.PropertyId = 0

	buffer, err := ReadDeviceIoControl(
		handle,
		IOCTL_STORAGE_QUERY_PROPERTY,
		(*byte)(unsafe.Pointer(&query)),
		uint32(unsafe.Sizeof(query)),
	)
	if err != nil {
		return nil, err
	}

	resp := (*STORAGE_DEVICE_DESCRIPTOR)(unsafe.Pointer(&buffer[0]))

	result := &StorageDeviceDescription{
		DeviceType:         resp.DeviceType,
		DeviceTypeModifier: resp.DeviceTypeModifier,
		RemovableMedia:     resp.RemovableMedia,
		BusType:            resp.BusType,
	}

	result.VendorId = strings.Trim(ReadNullTerminatedAscii(buffer, int(resp.VendorIdOffset)), " ")
	result.ProductId = strings.Trim(ReadNullTerminatedAscii(buffer, int(resp.ProductIdOffset)), " ")
	result.SerialNumber = strings.Trim(ReadNullTerminatedAscii(buffer, int(resp.SerialNumberOffset)), " ")
	result.ProductRevision = strings.Trim(ReadNullTerminatedAscii(buffer, int(resp.ProductRevisionOffset)), " ")

	return result, nil
}

func ReadDeviceIoControl(handle windows.Handle, ioctl uint32, inBuffer *byte, inSize uint32) ([]byte, error) {
	var bytesReturned uint32

	buffer := make([]byte, 4096)
	err := windows.DeviceIoControl(handle, ioctl, inBuffer, inSize, &buffer[0], uint32(len(buffer)), &bytesReturned, nil)
	var errno syscall.Errno
	ok := errors.As(err, &errno)
	if ok && errors.Is(errno, windows.ERROR_INSUFFICIENT_BUFFER) {
		buffer = make([]byte, bytesReturned)
		err = windows.DeviceIoControl(handle, ioctl, inBuffer, inSize, &buffer[0], uint32(len(buffer)), &bytesReturned, nil)
	}
	if err == nil {
		return buffer[:bytesReturned], nil
	}

	return nil, errno
}
