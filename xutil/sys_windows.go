package xutil

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
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
		return nil, errors.Wrapf(err, "wmi_.Query %q", query)
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

type win32DiskDriveDeviceID struct {
	DeviceID string
}

func ListDisks() ([]string, error) {
	var dst []win32DiskDriveDeviceID
	err := wmi_.Query("SELECT DeviceID FROM Win32_DiskDrive", &dst)
	if err != nil {
		return nil, err
	}
	var disks []string
	for _, d := range dst {
		disks = append(disks, d.DeviceID)
	}
	sort.Strings(disks)
	return disks, nil
}

func ListDisksV2() ([]Win32DiskDrive, error) {
	var dst []Win32DiskDrive
	err := wmi_.Query("SELECT DeviceID, PNPDeviceID, SIZE FROM Win32_DiskDrive", &dst)
	if err != nil {
		return nil, err
	}

	sort.Slice(dst, func(i, j int) bool {
		return dst[i].DeviceID < dst[j].DeviceID
	})

	return dst, nil
}

type Win32DiskDrive struct {
	DeviceID    string
	PNPDeviceID string
	Size        int64
}

func PnpDeviceID(deviceID string) (string, error) {
	var dst []Win32DiskDrive

	err := wmi_.Query(
		"SELECT DeviceID, PNPDeviceID, SIZE FROM Win32_DiskDrive",
		&dst,
	)
	if err != nil {
		return "", err
	}

	for _, d := range dst {
		if strings.EqualFold(d.DeviceID, deviceID) {
			return d.PNPDeviceID, nil
		}
	}

	return "", errors.Errorf("PNPDeviceID of %s not found", deviceID)
}

func Win32DiskSize(deviceID string) (int64, error) {
	var dst []Win32DiskDrive

	err := wmi_.Query(
		"SELECT DeviceID, PNPDeviceID, SIZE FROM Win32_DiskDrive",
		&dst,
	)
	if err != nil {
		return 0, err
	}

	for _, d := range dst {
		if strings.EqualFold(d.DeviceID, deviceID) {
			return d.Size, nil
		}
	}

	return 0, errors.Errorf("Win32DiskSize of %s not found", deviceID)
}

type DISK_GEOMETRY struct {
	Cylinders         uint64
	MediaType         uint32
	TracksPerCylinder uint32
	SectorsPerTrack   uint32
	BytesPerSector    uint32
}

type DISK_GEOMETRY_EX_RAW struct {
	Geometry DISK_GEOMETRY
	DiskSize uint64
}

func GetDiskGeometry(disk string) (DISK_GEOMETRY, error) {
	reader, err := os.Open(disk)
	if err != nil {
		return DISK_GEOMETRY{}, err
	}
	defer reader.Close()

	buf := make([]uint8, 0x80)
	var n uint32

	if err = windows.DeviceIoControl(
		windows.Handle(reader.Fd()),
		IOCTL_DISK_GET_DRIVE_GEOMETRY_EX,
		nil,
		0,
		&buf[0],
		uint32(len(buf)),
		&n,
		nil); err != nil {
		return DISK_GEOMETRY{}, err
	}

	diskGeometryBase := (*DISK_GEOMETRY_EX_RAW)(unsafe.Pointer(&buf[0]))
	blockSize := int64(diskGeometryBase.Geometry.BytesPerSector)
	blockCount := int64(diskGeometryBase.DiskSize) / blockSize
	if int64(diskGeometryBase.DiskSize)%blockSize != 0 {
		return DISK_GEOMETRY{}, errors.Errorf("block device size is not an integer multiple of its block size (%d %% %d = %d)",
			diskGeometryBase.DiskSize, blockSize, diskGeometryBase.DiskSize%uint64(blockSize))
	}
	_ = blockCount
	return diskGeometryBase.Geometry, nil
}

func BytesPerSector(dev string) (int, error) {
	geo, err := GetDiskGeometry(dev)
	if err != nil {
		return 0, err
	}
	return int(geo.BytesPerSector), nil
}

type GET_DISK_ATTRIBUTES struct {
	Version    uint32
	Reserved1  uint32
	Attributes uint64
}

const (
	DISK_ATTRIBUTE_OFFLINE   = 0x000000001
	DISK_ATTRIBUTE_READ_ONLY = 0x000000002
)

// GetDiskAttr 获取磁盘属性
func GetDiskAttr(hardDiskPath string) (offline, readonly bool, err error) {
	handle, err := OpenDevice(hardDiskPath)
	if err != nil {
		return false, false, err
	}
	defer func() {
		_ = windows.CloseHandle(handle)
	}()
	var dgSize uint32
	tmpLen := 16 << 10
	ioctlBuf := make([]byte, tmpLen)
	err = windows.DeviceIoControl(
		handle,
		IOCTL_DISK_GET_DISK_ATTRIBUTES,
		nil,
		0,
		&ioctlBuf[0],
		uint32(tmpLen),
		&dgSize,
		nil)
	if err != nil {
		return false, false, err
	}
	attr := GET_DISK_ATTRIBUTES{}
	err = struc.UnpackWithOptions(bytes.NewReader(ioctlBuf), &attr, &struc.Options{Order: binary.LittleEndian})
	if err != nil {
		return false, false, err
	}
	return attr.Attributes&DISK_ATTRIBUTE_OFFLINE > 0, attr.Attributes&DISK_ATTRIBUTE_READ_ONLY > 0, nil
}

type STORAGE_PROPERTY_QUERY struct {
	PropertyId           uint32
	QueryType            uint32
	AdditionalParameters [1]byte
}

type STORAGE_ACCESS_ALIGNMENT_DESCRIPTOR struct {
	Version                       uint32
	Size                          uint32
	BytesPerCacheLine             uint32
	BytesOffsetForCacheAlignment  uint32
	BytesPerLogicalSector         uint32
	BytesPerPhysicalSector        uint32
	BytesOffsetForSectorAlignment uint32
}

func DiskAlignmentStorage(hardDiskPath string) (sad STORAGE_ACCESS_ALIGNMENT_DESCRIPTOR, err error) {
	handle, err := OpenDevice(hardDiskPath)
	if err != nil {
		return sad, err
	}
	defer func() {
		_ = windows.CloseHandle(handle)
	}()

	query := STORAGE_PROPERTY_QUERY{
		PropertyId: 6, // StorageAccessAlignmentProperty
		QueryType:  0, // PropertyStandardQuery
	}
	var returned uint32

	err = windows.DeviceIoControl(
		handle,
		IOCTL_STORAGE_QUERY_PROPERTY,
		(*byte)(unsafe.Pointer(&query)),
		uint32(unsafe.Sizeof(query)),
		(*byte)(unsafe.Pointer(&sad)),
		uint32(unsafe.Sizeof(sad)),
		&returned,
		nil)
	if err != nil {
		return sad, err
	}

	return sad, nil
}

func DiskSectorAlignment(dev string) (sa StorageAlignment, err error) {
	sad, err := DiskAlignmentStorage(dev)
	if err != nil {
		if errors.Is(err, windows.ERROR_NOT_SUPPORTED) ||
			errors.Is(err, windows.ERROR_INVALID_PARAMETER) ||
			errors.Is(err, windows.ERROR_INVALID_FUNCTION) {
			sa.LogicalSectorSize = 512
			sa.PhysicalSectorSize = sa.LogicalSectorSize
			return sa, nil
		}
		return sa, errors.Wrapf(err, "DiskSectorAlignment")
	}
	sa.PhysicalSectorSize = int(sad.BytesPerPhysicalSector)
	sa.LogicalSectorSize = int(sad.BytesPerLogicalSector)
	return sa, nil
}

// TryToGrantSeSystemEnvironmentPrivilege 尝试获取 SeSystemEnvironmentPrivilege 的权限
// 参考：https://learn.microsoft.com/en-us/previous-versions/windows/it-pro/windows-10/security/threat-protection/auditing/event-4672
func TryToGrantSeSystemEnvironmentPrivilege() error {
	p := windows.CurrentProcess()
	var token windows.Token
	err := windows.OpenProcessToken(p, windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &token)
	if err != nil {
		return err
	}

	defer token.Close()

	var luid windows.LUID
	err = windows.LookupPrivilegeValue(nil, windows.StringToUTF16Ptr("SeSystemEnvironmentPrivilege"), &luid)
	if err != nil {
		return err
	}

	ap := windows.Tokenprivileges{
		PrivilegeCount: 1,
	}
	ap.Privileges[0].Luid = luid
	ap.Privileges[0].Attributes = windows.SE_PRIVILEGE_ENABLED

	return windows.AdjustTokenPrivileges(token, false, &ap, 0, nil, nil)
}

type Extent struct {
	NextVCN uint64
	LCN     uint64
}

type RetrievalPointersBuffer struct {
	ExtentCount uint32
	Padding     uint32
	StartingVCN uint64
	Extents     []Extent
}

func GetClusterSize(drive string) (int64, error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	procGetDiskFreeSpaceW := kernel32.NewProc("GetDiskFreeSpaceW")

	var sectorsPerCluster, bytesPerSector, numberOfFreeClusters, totalNumberOfClusters uint32

	// Drive 字符串需要以 null 结尾，例如 "C:\\"
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

func QueryFileExtentsOnVolume(file string, clusterSize int) (r RetrievalPointersBuffer, err error) {
	fd, err := os.Open(file)
	if err != nil {
		return r, err
	}
	defer fd.Close()

	stat, err := fd.Stat()
	if err != nil {
		return r, err
	}

	if stat.Size() < int64(clusterSize) {
		return r, errors.Errorf("file %s is too small (%vB)", file, stat.Size())
	}

	// 返回值定义：
	//  typedef struct RETRIEVAL_POINTERS_BUFFER {
	//    DWORD                    ExtentCount;
	//    LARGE_INTEGER            StartingVcn;
	//    struct {
	//      LARGE_INTEGER NextVcn;
	//      LARGE_INTEGER Lcn;
	//    };
	//    __unnamed_struct_195e_66 Extents[1];
	//  } RETRIEVAL_POINTERS_BUFFER, *PRETRIEVAL_POINTERS_BUFFER;

	// 计算最大有多少个extent.
	maxExtents := int(stat.Size() / int64(clusterSize))

	// 构造输出缓冲区.
	bufferSize := int(unsafe.Sizeof(uint32(0))) + // ExtentCount
		int(unsafe.Sizeof(uint32(0))) + // Padding
		int(unsafe.Sizeof(uint64(0))) + // StartingVCN
		maxExtents*int(unsafe.Sizeof(Extent{}))

	var startingVCN uint64 = 0
	var bytesReturned uint32

	outBuf := make([]byte, bufferSize)

	err = windows.DeviceIoControl(
		windows.Handle(fd.Fd()),
		windows.FSCTL_GET_RETRIEVAL_POINTERS,
		(*byte)(unsafe.Pointer(&startingVCN)),
		uint32(unsafe.Sizeof(startingVCN)),
		&outBuf[0],
		uint32(len(outBuf)),
		&bytesReturned,
		nil,
	)
	if err != nil {
		return r, err
	}
	if bytesReturned == 0 {
		return r, errors.Errorf("no bytes returned")
	}

	// 解析结构体字段
	r.ExtentCount = *(*uint32)(unsafe.Pointer(&outBuf[0]))
	r.StartingVCN = *(*uint64)(unsafe.Pointer(&outBuf[8])) // 4字节 padding 后是 StartingVCN

	r.Extents = make([]Extent, r.ExtentCount)
	baseOffset := 16 // Extents 从 offset 16 开始
	for i := 0; i < int(r.ExtentCount); i++ {
		offset := baseOffset + i*int(unsafe.Sizeof(Extent{}))
		extent := (*Extent)(unsafe.Pointer(&outBuf[offset]))
		r.Extents[i] = *extent
	}

	return r, nil
}

func FileDiskExtents(file string) (es []FileDiskExtentSegment, err error) {
	stat, err := os.Lstat(file)
	if err != nil {
		return nil, err
	}
	if stat.IsDir() {
		return nil, errors.Errorf("%s is a directory", file)
	}
	fileSize := stat.Size()

	volName := filepath.VolumeName(file)
	if volName == "" || !strings.HasSuffix(volName, ":") {
		return nil, errors.Errorf("volume name of %s is invalid", file)
	}
	volUncPath := fmt.Sprintf("\\\\.\\%s", volName)
	volPath := fmt.Sprintf("%s\\", volName)

	volExtentsOnDisk, err := VolumeMountpointToExtents(volUncPath)
	if err != nil {
		return nil, err
	}
	bytesPerCluster, err := GetClusterSize(volPath)
	if err != nil {
		return nil, err
	}
	fileExtentsInfo, err := QueryFileExtentsOnVolume(file, int(bytesPerCluster))
	if err != nil {
		return nil, err
	}
	if fileExtentsInfo.ExtentCount == 0 {
		return nil, errors.Errorf("file %s has no extents", file)
	}

	fileExtentsOnVolume := make([]volumeSegment, 0)
	vcn := fileExtentsInfo.StartingVCN
	for i, e := range fileExtentsInfo.Extents {
		if i > 0 {
			vcn = fileExtentsInfo.Extents[i-1].NextVCN
		}
		logicStart := int64(vcn) * bytesPerCluster
		segStart := int64(e.LCN) * bytesPerCluster
		segSize := int64(e.NextVCN-vcn) * bytesPerCluster
		if logicStart+segSize > fileSize {
			segSize = fileSize - logicStart
		}
		fileExtentsOnVolume = append(fileExtentsOnVolume, volumeSegment{
			start: segStart,
			size:  segSize,
		})
	}

	for _, fe := range fileExtentsOnVolume {
		extentSize := fe.size
		fileExtentVolStart := fe.start
		fileExtentVolEnd := fileExtentVolStart + extentSize
		diskDelta := fe.start

		diskVolStart := int64(0)
		for _, ve := range volExtentsOnDisk {
			diskVolEnd := diskVolStart + int64(ve.ExtentLength)
			diskDelta = fileExtentVolStart - diskVolStart

			if fileExtentVolStart < diskVolStart {
				return nil, errors.New("unexcepted range")
			} else if fileExtentVolStart >= diskVolStart && fileExtentVolEnd <= diskVolEnd {
				// 全包含
				es = append(es, FileDiskExtentSegment{
					Disk:  WindowsDiskPathFromID(ve.DiskNumber),
					Start: int64(ve.StartingOffset) + diskDelta,
					Size:  extentSize,
				})
				break
			} else if fileExtentVolStart < diskVolEnd && fileExtentVolEnd > diskVolEnd {
				// 部分包含，做截断处理
				deltaExtentSize := diskVolEnd - fileExtentVolStart
				es = append(es, FileDiskExtentSegment{
					Disk:  WindowsDiskPathFromID(ve.DiskNumber),
					Start: int64(ve.StartingOffset) + diskDelta,
					Size:  deltaExtentSize,
				})
				extentSize -= deltaExtentSize
				fileExtentVolStart += deltaExtentSize
			}

			diskVolStart = diskVolEnd
		}
	}

	if len(es) == 0 {
		return nil, errors.New("failed to calculate extents")
	}
	return es, nil
}

// QueryDosDevice 获取磁盘驱动器（例如：C:）的DosDevice形式（例如：\Device\HarddiskVolume1）
func QueryDosDevice(drive string) (string, error) {
	dev := make([]uint16, windows.MAX_PATH)
	_, err := windows.QueryDosDevice(windows.StringToUTF16Ptr(drive), &dev[0], windows.MAX_PATH)
	if err != nil {
		return "", err
	}
	return windows.UTF16ToString(dev), nil
}

func MsGuidFromBytes(arr []uint8) (g windows.GUID, err error) {
	if len(arr) != 16 {
		return windows.GUID{}, errors.New("length of bytes array of GUID is insufficient")
	}
	g.Data1 = uint32(arr[3])<<24 | uint32(arr[2])<<16 | uint32(arr[1])<<8 | uint32(arr[0])
	g.Data2 = uint16(arr[5])<<8 | uint16(arr[4])
	g.Data3 = uint16(arr[7])<<8 | uint16(arr[6])
	copy(g.Data4[:], arr[8:])
	return g, nil
}

// DISK_EXTENT
type diskExtent struct {
	DiskNumber     uint32
	_              uint32
	StartingOffset int64
	ExtentLength   int64
}

// VOLUME_DISK_EXTENTS
type volumeDiskExtents struct {
	NumberOfDiskExtents uint32
	_                   uint32
	Extents             [1]diskExtent
}

// 根据分区取第一个物理磁盘号
func GetDiskNum(vol string) (uint32, error) {
	root, _ := NormalizeDrive(vol, 0)
	if root == "" {
		return 0, fmt.Errorf("invalid volume: %q", vol)
	}
	// \\.\C:
	volPath := `\\.\` + strings.TrimRight(root, `\`)
	pVol, err := syscall.UTF16PtrFromString(volPath)
	if err != nil {
		return 0, err
	}

	hVol, err := syscall.CreateFile(
		pVol,
		0, // 只读
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,
		nil,
		syscall.OPEN_EXISTING,
		0,
		0,
	)
	if err != nil {
		return 0, fmt.Errorf("CreateFile volume %s failed: %v", volPath, err)
	}
	defer syscall.CloseHandle(hVol)

	out := make([]byte, 1024)
	var bytesRet uint32
	err = syscall.DeviceIoControl(
		hVol,
		0x00560000, // IOCTL_VOLUME_GET_VOLUME_DISK_EXTENTS
		nil,
		0,
		&out[0],
		uint32(len(out)),
		&bytesRet,
		nil,
	)
	if err != nil {
		return 0, fmt.Errorf("DeviceIoControl IOCTL_VOLUME_GET_VOLUME_DISK_EXTENTS failed: %v", err)
	}
	if bytesRet < uint32(unsafe.Sizeof(volumeDiskExtents{})) {
		return 0, fmt.Errorf("VOLUME_DISK_EXTENTS too small: %d", bytesRet)
	}

	vde := (*volumeDiskExtents)(unsafe.Pointer(&out[0]))
	if vde.NumberOfDiskExtents == 0 {
		return 0, fmt.Errorf("no disk extents for volume %s", volPath)
	}
	//第一个Extent的DiskNumber
	//有个坑，32位偏移量是4开始，64是8开始
	//直接用 Extents[0].DiskNumber，兼容32/64
	diskNum := vde.Extents[0].DiskNumber
	return diskNum, nil
}

// WindowsDir 返回 Windows 根目录（优先 SystemRoot，其次 WINDIR）。
func WindowsDir() string {
	if d := os.Getenv("SystemRoot"); d != "" {
		return d
	}
	return os.Getenv("WINDIR")
}

// 判断当前是否为 32 位进程运行在 64 位 Windows 上。
func IsWOW64() bool {
	if runtime.GOARCH != "386" {
		return false
	}
	// API
	var wow64 bool
	if err := windows.IsWow64Process(windows.CurrentProcess(), &wow64); err == nil {
		return wow64
	}
	return os.Getenv("PROCESSOR_ARCHITEW6432") != ""
}

// 返回系统命令路径：优先 Sysnative(在 WOW64 下)，其次 System32，最后回退到 exeName（走 PATH）。
func GetSystemExe(exeName string) string {
	windir := WindowsDir()
	if windir == "" {
		return exeName
	}

	sysDir := "System32"
	if IsWOW64() {
		sysDir = "Sysnative"
	}

	preferred := filepath.Join(windir, sysDir, exeName)
	if IsExisted(preferred) {
		return preferred
	}

	alt := filepath.Join(windir, "System32", exeName)
	if IsExisted(alt) {
		return alt
	}

	return exeName
}

// DiskFreedExtent 表示卷缩容后在物理磁盘上释放的区域。
type DiskFreedExtent struct {
	DiskNumber uint32 // 物理磁盘编号
	Offset     int64  // 释放区域在物理磁盘上的起始偏移（字节）
	Length     int64  // 释放区域的长度（字节）
}

// ShrinkVolume 对指定卷进行缩容，返回释放的物理磁盘区域。
//
// volumePath 为卷路径，如 "D:" 或 `\\.\D:`。
// shrinkBytes 为缩容的字节大小，实际缩容大小可能因文件系统对齐向上取整。
// reuse 为 true 时，若卷尾部已有 >= shrinkBytes 的未分配空间，则直接复用而不执行缩容。
//
// 内部通过 diskpart 执行缩容操作。
//
// 返回值：
//   - freed: 缩容后在物理磁盘上释放的区域列表。
//   - err: 错误信息。
//
// 需要管理员权限。缩容只能从卷的尾部释放空间。
func ShrinkVolume(volumePath string, shrinkBytes int64, reuse bool) (freed []DiskFreedExtent, err error) {
	volPath, err := normalizeVolumePath(volumePath)
	if err != nil {
		return nil, err
	}

	// 1. 获取当前 extents
	oldExtents, err := getVolumeExtents(volPath)
	if err != nil {
		return nil, err
	}

	if len(oldExtents) == 0 {
		return nil, errors.New("no disk extents found")
	}

	var totalSize int64
	for _, e := range oldExtents {
		totalSize += e.ExtentLength
	}

	// 2. 验证缩容大小
	if shrinkBytes <= 0 {
		return nil, errors.Errorf("shrink size must be positive, got %d", shrinkBytes)
	}
	if shrinkBytes >= totalSize {
		return nil, errors.Errorf("shrink size %d exceeds total size %d", shrinkBytes, totalSize)
	}

	// 3. 若指定复用，先检查卷尾部是否已有足够空闲空间
	if reuse {
		if freed := tryReuseFreeSpace(oldExtents, shrinkBytes); freed != nil {
			return freed, nil
		}
	}

	desiredMB := uint64(shrinkBytes) / (1024 * 1024)
	if desiredMB == 0 {
		desiredMB = 1 // 至少 1 MB
	}

	// 4. 通过 diskpart 执行缩容
	if err := diskpartShrink(volPath, desiredMB); err != nil {
		return nil, err
	}

	// 5. 缩容后获取 extents
	newExtents, err := getVolumeExtents(volPath)
	if err != nil {
		return nil, err
	}

	// 6. 计算释放的物理区域
	return diffDiskExtents(oldExtents, newExtents), nil
}

// tryReuseFreeSpace 检查卷尾部是否已有足够的未分配磁盘空间可以复用。
//
// 若卷最后一个 extent 所在的物理磁盘上，紧随其后有
// >= shrinkBytes 的未分配空间，则将其作为"已释放区域"返回。
func tryReuseFreeSpace(extents []diskExtent, shrinkBytes int64) []DiskFreedExtent {
	lastExt := extents[len(extents)-1]
	volEnd := lastExt.StartingOffset + lastExt.ExtentLength
	diskNum := lastExt.DiskNumber

	// 读取物理磁盘的分区布局
	partitions, err := getDiskPartitions(diskNum)
	if err != nil {
		return nil // 无法确定，回退到正常缩容
	}

	// 找到紧随卷尾部之后的分区间隙（空闲区域）
	nextPartStart := int64(-1)
	for _, p := range partitions {
		pStart := int64(p.StartingOffset.QuadPart)
		if pStart >= volEnd && (nextPartStart < 0 || pStart < nextPartStart) {
			nextPartStart = pStart
		}
	}
	if nextPartStart < 0 {
		// 没有后续分区：空闲区域延伸到磁盘末尾
		diskSize, err := getDiskSize(diskNum)
		if err != nil {
			return nil
		}
		nextPartStart = diskSize
	}

	freeAfterVol := nextPartStart - volEnd
	if freeAfterVol <= 0 || freeAfterVol < shrinkBytes {
		return nil
	}

	return []DiskFreedExtent{{
		DiskNumber: diskNum,
		Offset:     volEnd,
		Length:     freeAfterVol,
	}}
}

// diskPartition 表示一个磁盘分区的基本信息。
type diskPartition struct {
	StartingOffset  largeInteger
	PartitionLength largeInteger
}

type largeInteger struct {
	QuadPart uint64
}

// getDiskPartitions 读取物理磁盘的分区布局。
func getDiskPartitions(diskNumber uint32) ([]diskPartition, error) {
	diskPath := fmt.Sprintf(`\\.\PHYSICALDRIVE%d`, diskNumber)
	pDisk, err := syscall.UTF16PtrFromString(diskPath)
	if err != nil {
		return nil, err
	}

	hDisk, err := syscall.CreateFile(
		pDisk,
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,
		nil,
		syscall.OPEN_EXISTING,
		0,
		0,
	)
	if err != nil {
		return nil, err
	}
	defer syscall.CloseHandle(hDisk)

	// 先查询需要的缓冲区大小
	out := make([]byte, 16384) // 足够容纳 ~128 个分区
	var bytesRet uint32

	err = syscall.DeviceIoControl(
		hDisk,
		ioctlDiskGetDriveLayoutEx,
		nil,
		0,
		&out[0],
		uint32(len(out)),
		&bytesRet,
		nil,
	)
	if err != nil {
		return nil, err
	}

	if bytesRet < 8 {
		return nil, errors.New("drive layout too small")
	}

	// DRIVE_LAYOUT_INFORMATION_EX:
	//   PartitionStyle  uint32 (4) at offset 0
	//   PartitionCount  uint32 (4) at offset 4
	//   PartitionEntry  []PARTITION_INFORMATION_EX at offset 48

	partCount := *(*uint32)(unsafe.Pointer(&out[4]))
	if partCount == 0 {
		return nil, nil
	}

	// PARTITION_INFORMATION_EX 结构体偏移：
	//  PartitionStyle              4
	//  StartingOffset (LARGE_INTEGER) 8
	//  PartitionLength (LARGE_INTEGER) 8
	//  ... (总共 144 字节 per entry)

	const partEntrySize = 144
	const partEntryBase = 48

	partitions := make([]diskPartition, partCount)
	for i := range partitions {
		off := partEntryBase + i*partEntrySize
		if off+16 > int(bytesRet) {
			break
		}
		partitions[i].StartingOffset.QuadPart = *(*uint64)(unsafe.Pointer(&out[off+8]))
		partitions[i].PartitionLength.QuadPart = *(*uint64)(unsafe.Pointer(&out[off+16]))
	}

	return partitions, nil
}

const (
	// ioctlDiskGetDriveLayoutEx 是 IOCTL_DISK_GET_DRIVE_LAYOUT_EX 控制码。
	//
	// CTL_CODE(IOCTL_DISK_BASE, 0x0014, METHOD_BUFFERED, FILE_READ_ACCESS)
	ioctlDiskGetDriveLayoutEx = 0x00070050

	// ioctlDiskGetLengthInfo 是 IOCTL_DISK_GET_LENGTH_INFO 控制码。
	//
	// CTL_CODE(IOCTL_DISK_BASE, 0x0017, METHOD_BUFFERED, FILE_READ_ACCESS)
	ioctlDiskGetLengthInfo = 0x0007405C
)

// getDiskSize 获取物理磁盘的总大小（字节）。
func getDiskSize(diskNumber uint32) (int64, error) {
	diskPath := fmt.Sprintf(`\\.\PHYSICALDRIVE%d`, diskNumber)
	pDisk, err := syscall.UTF16PtrFromString(diskPath)
	if err != nil {
		return 0, err
	}

	hDisk, err := syscall.CreateFile(
		pDisk,
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,
		nil,
		syscall.OPEN_EXISTING,
		0,
		0,
	)
	if err != nil {
		return 0, err
	}
	defer syscall.CloseHandle(hDisk)

	out := make([]byte, 8)
	var bytesRet uint32

	err = syscall.DeviceIoControl(
		hDisk,
		ioctlDiskGetLengthInfo,
		nil,
		0,
		&out[0],
		uint32(len(out)),
		&bytesRet,
		nil,
	)
	if err != nil {
		return 0, err
	}

	if bytesRet >= 8 {
		return int64(*(*uint64)(unsafe.Pointer(&out[0]))), nil
	}
	return 0, errors.New("disk length too small")
}

// diskpartShrink 使用 diskpart 对卷进行缩容。
func diskpartShrink(volPath string, desiredMB uint64) error {
	vol := strings.TrimPrefix(volPath, `\\.\`)

	script := fmt.Sprintf("select volume=%s\nshrink desired=%d\nexit\n", vol, desiredMB)
	cmd := exec.Command("diskpart")
	cmd.Stdin = strings.NewReader(script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errors.Wrapf(err, "diskpart shrink %s by %d MB: %s", vol, desiredMB, string(out))
	}
	return nil
}

// getVolumeExtents 获取卷的物理 extent 列表。
func getVolumeExtents(volPath string) ([]diskExtent, error) {
	pVol, err := syscall.UTF16PtrFromString(volPath)
	if err != nil {
		return nil, err
	}

	hVol, err := syscall.CreateFile(
		pVol,
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,
		nil,
		syscall.OPEN_EXISTING,
		0,
		0,
	)
	if err != nil {
		return nil, errors.Wrapf(err, "open volume %s", volPath)
	}
	defer syscall.CloseHandle(hVol)

	return getVolumeDiskExtents(hVol)
}

// diffDiskExtents 计算缩容前后释放的物理区域。
//
// 缩容只从尾部释放空间，比较前后 extent 列表的末尾差异。
func diffDiskExtents(oldExtents, newExtents []diskExtent) []DiskFreedExtent {
	if len(oldExtents) == 0 {
		return nil
	}

	lastOld := oldExtents[len(oldExtents)-1]

	// 计算旧总大小
	var oldTotal, newTotal int64
	for _, e := range oldExtents {
		oldTotal += e.ExtentLength
	}
	for _, e := range newExtents {
		newTotal += e.ExtentLength
	}

	freedBytes := oldTotal - newTotal
	if freedBytes <= 0 {
		return nil
	}

	// 释放区域在最后一个 extent 的尾部
	freedEnd := lastOld.StartingOffset + lastOld.ExtentLength
	freedStart := freedEnd - freedBytes

	return []DiskFreedExtent{{
		DiskNumber: lastOld.DiskNumber,
		Offset:     freedStart,
		Length:     freedBytes,
	}}
}

// normalizeVolumePath 将卷路径规范化为 \\.\X: 格式。
func normalizeVolumePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("empty volume path")
	}

	// 去掉尾部反斜杠
	path = strings.TrimRight(path, `\`)

	// 如果已经是 \\.\X: 格式，直接返回
	if strings.HasPrefix(path, `\\.\`) {
		if len(path) < 6 || path[5] != ':' {
			return "", errors.Errorf("invalid volume path: %s", path)
		}
		return path, nil
	}

	// D: 或 D:\ 格式
	if len(path) >= 2 && path[1] == ':' {
		return `\\.\` + path[:2], nil
	}

	return "", errors.Errorf("unsupported volume path format: %s", path)
}

// getVolumeDiskExtents 获取卷的物理磁盘分布。
func getVolumeDiskExtents(hVol syscall.Handle) ([]diskExtent, error) {
	out := make([]byte, 4096)
	var bytesRet uint32

	err := syscall.DeviceIoControl(
		hVol,
		IOCTL_VOLUME_GET_VOLUME_DISK_EXTENTS,
		nil,
		0,
		&out[0],
		uint32(len(out)),
		&bytesRet,
		nil,
	)
	if err != nil {
		return nil, errors.Wrapf(err, "get disk extents")
	}

	if bytesRet < uint32(unsafe.Sizeof(volumeDiskExtents{})) {
		return nil, errors.Errorf("disk extents data too small: %d", bytesRet)
	}

	vde := (*volumeDiskExtents)(unsafe.Pointer(&out[0]))
	if vde.NumberOfDiskExtents == 0 {
		return nil, errors.New("zero disk extents")
	}

	// 将 C 数组转换为 Go 切片
	extentSize := unsafe.Sizeof(diskExtent{})
	result := make([]diskExtent, vde.NumberOfDiskExtents)
	for i := range result {
		ptr := (*diskExtent)(unsafe.Pointer(uintptr(unsafe.Pointer(&vde.Extents[0])) + uintptr(i)*extentSize))
		result[i] = *ptr
	}

	return result, nil
}

// getBytesPerSector 获取卷的扇区大小。
func getBytesPerSector(hVol syscall.Handle) (uint32, error) {
	out := make([]byte, 1024)
	var bytesRet uint32

	err := syscall.DeviceIoControl(
		hVol,
		0x0007405C, // IOCTL_DISK_GET_DRIVE_GEOMETRY_EX
		nil,
		0,
		&out[0],
		uint32(len(out)),
		&bytesRet,
		nil,
	)
	if err != nil {
		// 尝试使用老版本 IOCTL_DISK_GET_DRIVE_GEOMETRY
		return 512, nil // 回退到默认值
	}

	// DISK_GEOMETRY_EX 的 BytesPerSector 在偏移 16 处
	if bytesRet >= 20 {
		bps := *(*uint32)(unsafe.Pointer(&out[16]))
		if bps > 0 {
			return bps, nil
		}
	}

	return 512, nil
}

const (
	// fsctlShrinkVolume 是 FSCTL_SHRINK_VOLUME 控制码。
	//
	// CTL_CODE(FILE_DEVICE_FILE_SYSTEM, 51, METHOD_BUFFERED, FILE_ANY_ACCESS)
	// = (9 << 16) | (0 << 14) | (51 << 2) | 0
	fsctlShrinkVolume = 0x000900CC

	// shrinkVolumeRequestTypes
	shrinkPrepare = 1
	shrinkCommit  = 2
	shrinkAbort   = 3
)

// shrinkVolumeInformation 是 FSCTL_SHRINK_VOLUME 的输入结构体。
//
// 对应 Windows SDK 中的 SHRINK_VOLUME_INFORMATION：
//
//	typedef struct {
//	    SHRINK_VOLUME_REQUEST_TYPES RequestType;  // LONG, 4 bytes
//	    LONGLONG                    NewVolumeSize; // 8 bytes
//	} SHRINK_VOLUME_INFORMATION;
type shrinkVolumeInformation struct {
	RequestType   int32
	NewVolumeSize int64
}

// shrinkVolume 执行卷缩容（两步：Prepare → Commit）。
//
// newSize 为缩容后的卷目标大小（字节）。
func shrinkVolume(hVol syscall.Handle, newSize int64) error {
	info := shrinkVolumeInformation{
		RequestType:   shrinkPrepare,
		NewVolumeSize: newSize,
	}

	inBuf := make([]byte, unsafe.Sizeof(info))
	*(*shrinkVolumeInformation)(unsafe.Pointer(&inBuf[0])) = info

	var bytesRet uint32
	err := syscall.DeviceIoControl(
		hVol,
		fsctlShrinkVolume,
		&inBuf[0],
		uint32(len(inBuf)),
		nil,
		0,
		&bytesRet,
		nil,
	)
	if err != nil {
		return errors.Wrapf(err, "FSCTL_SHRINK_VOLUME Prepare to %d bytes", newSize)
	}

	// Commit
	info.RequestType = shrinkCommit
	*(*shrinkVolumeInformation)(unsafe.Pointer(&inBuf[0])) = info

	err = syscall.DeviceIoControl(
		hVol,
		fsctlShrinkVolume,
		&inBuf[0],
		uint32(len(inBuf)),
		nil,
		0,
		&bytesRet,
		nil,
	)
	if err != nil {
		// Commit 失败，尝试 Abort 回滚
		info.RequestType = shrinkAbort
		*(*shrinkVolumeInformation)(unsafe.Pointer(&inBuf[0])) = info
		syscall.DeviceIoControl(hVol, fsctlShrinkVolume, &inBuf[0], uint32(len(inBuf)), nil, 0, &bytesRet, nil)
		return errors.Wrapf(err, "FSCTL_SHRINK_VOLUME Commit to %d bytes", newSize)
	}

	return nil
}
