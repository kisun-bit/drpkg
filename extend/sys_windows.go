package extend

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
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

type win32DiskDrive struct {
	DeviceID string
}

func ListDisks() ([]string, error) {
	var dst []win32DiskDrive
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

	if err = windows.DeviceIoControl(windows.Handle(reader.Fd()), IOCTL_DISK_GET_DRIVE_GEOMETRY_EX, nil, 0, &buf[0], uint32(len(buf)), &n, nil); err != nil {
		return DISK_GEOMETRY{}, err
	}

	diskGeometryBase := (*DISK_GEOMETRY_EX_RAW)(unsafe.Pointer(&buf[0]))
	blockSize := int64(diskGeometryBase.Geometry.BytesPerSector)
	blockCount := int64(diskGeometryBase.DiskSize) / blockSize
	if int64(diskGeometryBase.DiskSize)%blockSize != 0 {
		return DISK_GEOMETRY{}, errors.Errorf("block device size is not an integer multiple of its block size (%d %% %d = %d)", diskGeometryBase.DiskSize, blockSize, diskGeometryBase.DiskSize%uint64(blockSize))
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

func CreateHiddenFile(path string, sizeBytes int64, removeBefore bool) error {
	const chunkSize = 1 << 20
	buf := make([]byte, chunkSize)

	if removeBefore {
		_ = os.Remove(path)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_TRUNC|os.O_SYNC, 0666)
	if err != nil {
		return err
	}
	defer f.Close()

	remaining := sizeBytes
	for remaining > 0 {
		toWrite := chunkSize
		if remaining < int64(chunkSize) {
			toWrite = int(remaining)
		}
		nr, er := f.Write(buf[:toWrite])
		if er != nil {
			return er
		}
		remaining -= int64(nr)
	}

	if err = f.Sync(); err != nil {
		return err
	}

	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	if err = syscall.SetFileAttributes(ptr, syscall.FILE_ATTRIBUTE_HIDDEN); err != nil {
		return err
	}

	return nil
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

type volumeSegment struct {
	start int64
	size  int64
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

func CopyFileByDiskExtents(file string, dst io.Writer) (int64, error) {
	es, err := FileDiskExtents(file)
	if err != nil {
		return 0, err
	}
	bytesPerCluster, err := GetClusterSize(filepath.VolumeName(file) + "\\")
	if err != nil {
		return 0, err
	}

	buf := make([]byte, bytesPerCluster)
	size := int64(0)

	for _, de := range es {
		df, eopen := os.Open(de.Disk)
		if eopen != nil {
			return 0, eopen
		}

		remain := de.Size
		start := de.Start
		for {
			if remain <= 0 {
				_ = df.Close()
				break
			}
			nr, er := df.ReadAt(buf, start)
			if er != nil {
				_ = df.Close()
				return 0, errors.Wrapf(er, "failed to read extent from %s", de.Disk)
			}
			wLen := nr
			if int64(nr) > remain {
				wLen = int(remain)
			}
			nw, ew := dst.Write(buf[:wLen])
			if ew != nil {
				_ = df.Close()
				return 0, errors.Wrap(ew, "failed to write extent to writer")
			}
			size += int64(nw)
			remain -= int64(nr)
			start += int64(nr)
		}
	}

	return size, nil
}
