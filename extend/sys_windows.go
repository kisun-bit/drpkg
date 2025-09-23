package extend

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/lunixbochs/struc"
	"github.com/pkg/errors"
	wmi_ "github.com/yusufpapurcu/wmi"
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
	if !strings.HasSuffix(path, "\\") {
		path += "\\"
	}
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
	if e != nil {
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

type Win32Volume struct {
	DeviceID     string
	Name         string
	BootVolume   bool
	SystemVolume bool
	DriveType    uint32
	Capacity     uint64
}

func ListWin32VolumeByWMI() ([]Win32Volume, error) {
	var vols []Win32Volume
	query := "SELECT DeviceID, Name, BootVolume, SystemVolume, DriveType, Capacity FROM Win32_Volume"
	if err := wmi_.Query(query, &vols); err != nil {
		return nil, err
	}
	return vols, nil
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
func VolumeUsageInfo(volumeMountpoint string) (total, used, free uint64, err error) {
	var available uint64
	if !strings.HasSuffix(volumeMountpoint, "\\") {
		volumeMountpoint += "\\"
	}
	err = windows.GetDiskFreeSpaceEx(windows.StringToUTF16Ptr(volumeMountpoint), &available, &total, &free)
	used = total - free
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
	if IsWindowsVolume(volumeMountpoint) {
		return strings.TrimSuffix(volumeMountpoint, "\\")
	}
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
		//uniqID = strings.TrimSuffix(strings.TrimPrefix(volName, `\\?\Volume{`), `}\`)
		uniqID = volName
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

func VolumeEnabledBitlocker(diskPath string, volumeStartOffset int64) (bool, error) {
	handle, err := OpenDevice(diskPath)
	if err != nil {
		return false, errors.Wrapf(err, "open device")
	}
	defer windows.CloseHandle(handle)

	newOff, err := windows.Seek(handle, volumeStartOffset, io.SeekStart)
	if err != nil {
		return false, errors.Wrapf(err, "seek device")
	}
	if newOff != volumeStartOffset {
		return false, errors.Errorf("can not seek device to %v", volumeStartOffset)
	}

	bitlockerBytes := make([]byte, 4096)
	var done uint32
	if err = windows.ReadFile(handle, bitlockerBytes, &done, nil); err != nil {
		return false, errors.Wrapf(err, "read device")
	}
	if string(bitlockerBytes[:11]) == "\xEB\x58\x90\x2D\x46\x56\x45\x2D\x46\x53\x2D" {
		return true, nil
	}
	return false, nil
}

type Win32_OperatingSystem struct {
	LastBootUpTime string
}

func GetBootTime() (time.Time, error) {
	var operatingSystems []Win32_OperatingSystem
	query := "SELECT LastBootUpTime FROM Win32_OperatingSystem"

	err := wmi_.Query(query, &operatingSystems)
	if err != nil {
		return time.Time{}, errors.Wrapf(err, "query operating system")
	}

	if len(operatingSystems) == 0 {
		return time.Time{}, errors.New("no operating system found")
	}

	// WMI 时间格式: 20230909081650.500000+480
	bootTimeStr := operatingSystems[0].LastBootUpTime
	// 解析 WMI 时间格式（需要转换）
	parsedTime, err := parseWMIDateTime(bootTimeStr)
	if err != nil {
		return time.Time{}, errors.Wrapf(err, "parse boot time %s", bootTimeStr)
	}

	return parsedTime, nil
}

func parseWMIDateTime(wmiTime string) (time.Time, error) {
	if len(wmiTime) < 14 {
		return time.Time{}, errors.New("wmi time too short")
	}
	// 提取基本日期时间部分: yyyyMMddHHmmss
	timeStr := wmiTime[:14]
	layout := "20060102150405"
	return time.Parse(layout, timeStr)
}
