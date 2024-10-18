package ioctl

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"github.com/kisun-bit/drpkg/util/logger"
	"github.com/lunixbochs/struc"
	"github.com/pkg/errors"
	"github.com/shirou/gopsutil/v3/process"
	"github.com/thoas/go-funk"
	wmi_ "github.com/yusufpapurcu/wmi"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
	"io"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

func QueryFileSize(fileName string) (size uint64, err error) {
	fileName = strings.ToUpper(fileName)
	isStorageDevice := strings.HasPrefix(fileName, `\\.\PHYSICALDRIVE`)
	isVssDevice := strings.HasPrefix(fileName, `\\?\GLOBALROOT\DEVICE\HARDDISKVOLUMESHADOWCOPY`)
	if isVssDevice && runtime.GOOS == "windows" {
		fileName += string(filepath.Separator)
		fmt.Println(fileName)
	}
	if !isStorageDevice {
		stat, err := os.Lstat(fileName)
		if err != nil {
			return 0, err
		}
		return uint64(stat.Size()), nil
	}
	handle, err := OpenDevice(fileName)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = windows.CloseHandle(handle)
	}()

	if isStorageDevice {
		var lenSize uint32
		var info GET_LENGTH_INFORMATION
		err = windows.DeviceIoControl(
			handle,
			IOCTL_DISK_GET_LENGTH_INFO,
			nil,
			0,
			(*byte)(unsafe.Pointer(&info)),
			uint32(unsafe.Sizeof(info)),
			&lenSize,
			nil)
		if err != nil {
			return 0, err
		}
		return uint64(info.Length), nil
	} else if isVssDevice {
		// 获取设备大小
		return QueryVolumeSize(fileName)
	}
	return 0, errors.New("unsupported size")
}

func SysLanguageIsZhCN() bool {
	rl, e := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_ID)
	if e != nil {
		return false
	}
	if len(rl) == 0 {
		return false
	}
	return rl[0] == "0804"
}

// QueryHardDiskAttr 获取硬盘属性信息(是否脱机, 是否只读).
func QueryHardDiskAttr(hardDiskPath string) (offline, readonly bool, err error) {
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
	return attr.Attributes&_DISK_ATTRIBUTE_OFFLINE > 0, attr.Attributes&_DISK_ATTRIBUTE_READ_ONLY > 0, nil
}

// QueryHardDiskProperty 查询硬盘配置空间特征信息.
func QueryHardDiskProperty(path string) (*StorageDeviceDescription, error) {
	handle, err := OpenDevice(path)
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

	result.VendorId = strings.Trim(readNullTerminatedAscii(buffer, int(resp.VendorIdOffset)), " ")
	result.ProductId = strings.Trim(readNullTerminatedAscii(buffer, int(resp.ProductIdOffset)), " ")
	result.SerialNumber = strings.Trim(readNullTerminatedAscii(buffer, int(resp.SerialNumberOffset)), " ")
	result.ProductRevision = strings.Trim(readNullTerminatedAscii(buffer, int(resp.ProductRevisionOffset)), " ")

	return result, nil
}

// QueryVolumeMountPathByAddress 通过卷的位置信息获取其挂载点路径(格式如: \\?\Volume{e3b9397c-0000-0000-0000-100000000000}\).
//   - 若调用者传递的位置信息参数为卷的，那么请尽量使得 isStrict 为true,
//     这样匹配挂载点路径时, diskNumber, startABytes, size三者必须完全匹配.
//   - 若调用者传递的位置信息参数为分区的, 那么可以任意设置 isStrict.
func QueryVolumeMountPathByAddress(hardDisk string, startABytes, size int64, isStrict bool) (mountPath string, err error) {
	volumeMountPaths, err := QueryAllVolumeMountPathUnderHardDisk(hardDisk)
	if err != nil {
		return "", err
	}
	for _, currentVolumeMountPath := range volumeMountPaths {
		// 必须忽略装载点的末尾的`\`字符, 否则无法打开此装载点.
		currentVolumeMountPath = strings.TrimRight(currentVolumeMountPath, "\\")
		pi, ePI := QueryPartitionInformation(currentVolumeMountPath)
		if ePI != nil {
			logger.Warnf("QueryVolumeMountPathByAddress QueryPartitionInformation: %v", ePI)
			continue // 遇到异常不终止, 直接判定下一个装载点.
		}
		matched := false
		if isStrict {
			matched = pi.StartingOffset == startABytes && pi.PartitionLength == size
		} else {
			matched = pi.StartingOffset == startABytes
		}
		if matched {
			return currentVolumeMountPath, nil
		}
	}
	return "", errors.Errorf("mount path of volume not found at address(#DISK-%v #START-%v #SIZE-%v)",
		hardDisk, startABytes, size)
}

// QueryAllVolumeMountPathUnderHardDisk 查询指定硬盘上所有的卷挂载点.
func QueryAllVolumeMountPathUnderHardDisk(hardDisk string) (volumeMountPaths []string, err error) {
	diskNumber, err := ParseDiskNumberFromHardDiskPath(hardDisk)
	if err != nil {
		return nil, err
	}
	guidBuf := make([]uint16, windows.MAX_PATH)
	hFindVolume, e := windows.FindFirstVolume(&guidBuf[0], uint32(len(guidBuf)))
	if e != nil {
		return nil, err
	}
	defer func() {
		_ = windows.FindVolumeClose(hFindVolume)
	}()
VolumeLoop:
	for ; ; err = windows.FindNextVolume(hFindVolume, &guidBuf[0], uint32(len(guidBuf))) {
		if err != nil {
			switch {
			case errors.Is(err.(windows.Errno), windows.ERROR_NO_MORE_FILES):
				break VolumeLoop
			default:
				continue VolumeLoop
			}
		}
		currentVolumeMountPath := windows.UTF16ToString(guidBuf)
		did, eDI := QueryHardDiskNumberByVolume(currentVolumeMountPath)
		if eDI != nil {
			// 一般而言, 光盘/软盘驱动器在调用此接口时会出现失败.
			//logger.Warnf("QueryVolumeMountPathByAddress DriveLetterToDiskNumber: %v", eDI)
			continue VolumeLoop
		}
		if did == diskNumber {
			volumeMountPaths = append(volumeMountPaths, currentVolumeMountPath)
		}
	}
	return volumeMountPaths, nil
}

func QueryVolumeExtents(volumePath string) (extBytes VolumeDiskExtents, err error) {
	if strings.HasPrefix(volumePath, `\\?\Volume`) {
		volumePath = strings.TrimSuffix(volumePath, "\\")
	}
	volumeHandle, err := windows.CreateFile(
		windows.StringToUTF16Ptr(volumePath),
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0)

	if err != nil {
		return nil, fmt.Errorf("error opening volume: %s", err)
	}
	defer func() {
		_ = windows.CloseHandle(volumeHandle)
	}()

	var bytesReturned uint32
	tmpSize := 16 << 10
	extBytes = make(VolumeDiskExtents, tmpSize)
	err = windows.DeviceIoControl(
		volumeHandle,
		IOCTL_VOLUME_GET_VOLUME_DISK_EXTENTS,
		nil,
		0,
		&extBytes[0],
		uint32(tmpSize),
		&bytesReturned,
		nil)
	if err != nil {
		return nil, err
	}
	return extBytes, nil
}

// QueryHardDiskNumberByVolume 获取卷所处的硬盘ID.
func QueryHardDiskNumberByVolume(volumePath string) (int, error) {
	extBytes, err := QueryVolumeExtents(volumePath)
	if err != nil {
		if e, ok := err.(syscall.Errno); ok {
			switch e {
			case 1:
				// 光盘/软盘等.
				return -1, nil
			}
		}
		logger.Warnf("QueryHardDiskNumberByVolume Len err=%v", err)
		return -1, err
	}
	if extBytes.Len() != 1 {
		err = fmt.Errorf("`%s` is spanned volume, it spans multiple disks", volumePath)
		logger.Warnf("QueryHardDiskNumberByVolume Len err=%v", err)
		return -1, nil
	}
	return int(extBytes.Extent(0).DiskNumber), nil
}

func QueryVolumeSize(volumePath string) (uint64, error) {
	extBytes, err := QueryVolumeExtents(volumePath)
	if err != nil {
		if e, ok := err.(syscall.Errno); ok {
			switch e {
			case 1:
				// 光盘/软盘等.
				return 0, nil
			}
		}
		logger.Warnf("QueryVolumeSize QueryVolumeExtents err=%v", err)
		return 0, err
	}
	if extBytes.Len() != 1 {
		err = fmt.Errorf("`%s` is spanned volume, it spans multiple disks", volumePath)
		logger.Warnf("QueryVolumeSize Len err=%v", err)
		return 0, nil
	}
	return extBytes.Extent(0).ExtentLength, nil
}

// QueryVolumeUsageInfo 通过卷名查询其磁盘使用情况.
func QueryVolumeUsageInfo(volume string) (available, total, free uint64, err error) {
	err = windows.GetDiskFreeSpaceEx(windows.StringToUTF16Ptr(volume), &available, &total, &free)
	return
}

// QueryPartitionInformation 获取指定卷的PARTITION_INFORMATION_EX信息.
// volumePath 可取`卷的挂载点路径`(注意不能带尾缀\符号, \\\\?\\Volume{e3b9397c-0000-0000-0000-100000000000}) 或 `卷驱动器的UNC路径`(如: \\.\C:)
func QueryPartitionInformation(volumePath string) (pi PARTITION_INFORMATION_EX, err error) {
	if strings.HasPrefix(volumePath, `\\?\Volume`) {
		volumePath = strings.TrimSuffix(volumePath, "\\")
	}
	handle, err := windows.CreateFile(
		windows.StringToUTF16Ptr(volumePath),
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
		//(*byte)(unsafe.Pointer(&pi)),
		//uint32(unsafe.Sizeof(pi)),
		&ioctlBuf[0],
		uint32(tmpLen),
		&dgSize,
		nil)
	if err != nil {
		return PARTITION_INFORMATION_EX{}, fmt.Errorf("failed to call IOCTL_DISK_GET_PARTITION_INFO_EX for %s, %v", volumePath, err)
	}
	err = struc.UnpackWithOptions(bytes.NewReader(ioctlBuf), &pi, &struc.Options{Order: binary.LittleEndian})
	if err != nil {
		err = fmt.Errorf("failed to unpack partition info raw, %v", err)
	}
	//pi.PartitionStyle = binary.LittleEndian.Uint32(ioctlBuf[:])
	//pi.StartingOffset = binary.LittleEndian.Uint64(ioctlBuf[8:])
	//pi.PartitionLength = binary.LittleEndian.Uint64(ioctlBuf[16:])
	//pi.PartitionNumber = binary.LittleEndian.Uint32(ioctlBuf[24:])
	return pi, err
}

// ListDrivesByWin32 通过win32接口获取所有的卷标.
func ListDrivesByWin32() (drives []string, err error) {
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
		drives = append(drives, letter)
	}
	return drives, nil
}

// ListDrivesByOpen 通过句柄打开函数获取所有的卷标.
func ListDrivesByOpen() (drives []string, err error) {
	for _, drive := range "ABCDEFGHIJKLMNOPQRSTUVWXYZ" {
		f, e := os.Open(string(drive) + ":\\")
		if e == nil {
			drives = append(drives, string(drive))
			_ = f.Close()
		}
	}
	return drives, nil
}

func ListDrives() (drives []string, err error) {
	drives, err = ListDrivesByWin32()
	if err == nil {
		return drives, nil
	}
	drives, err = ListDrivesByOpen()
	return drives, err
}

// GetLogicalDrives 获取Windows系统的所有逻辑卷(带驱动器号).
// Deprecated, 已弃用, 换用 GetLogicalDriversUnderHardDisk .
func GetLogicalDrives() (lds []LogicalDrive, err error) {
	drives, e := ListDrives()
	if e != nil {
		return nil, e
	}
	for _, letter := range drives {
		path := letter + ":"
		drive := LogicalDrive{DriveName: letter, UNCPath: fmt.Sprintf(`\\.\%s`, path)}
		utfPath, eUtf16 := windows.UTF16PtrFromString(path + `\`)
		if eUtf16 != nil {
			continue
		}
		if isValidLogicalDrive(utfPath) {
			label, fs, serial, eLF := QueryVolumeLabelAndFilesystemAndUniqueID(utfPath)
			if eLF == nil {
				drive.UniqueID = serial
				drive.Label = label
				drive.FileSystem = fs
			}
			dn, edn := QueryHardDiskNumberByVolume(drive.UNCPath)
			if edn != nil {
				logger.Warnf("GetLogicalDrives failed to call `DriveLetterToDiskNumber`: %v", edn)
				return nil, edn
			}
			drive.DiskNumber = dn
			lds = append(lds, drive)
		}
	}
	return lds, nil
}

// GetLogicalDriversUnderHardDisk 获取指定硬盘下的所有逻辑卷(所有存在挂载点的卷).
func GetLogicalDriversUnderHardDisk(hardDisk string) (lds []LogicalDrive, err error) {
	volumeMountPaths, err := QueryAllVolumeMountPathUnderHardDisk(hardDisk)
	if err != nil {
		return nil, err
	}
	for _, volumeMountPathWithoutSuffix := range volumeMountPaths {
		ld := LogicalDrive{}
		ld.GUIDMountPath = volumeMountPathWithoutSuffix
		driveMountPath, e := QueryVolumeDriveNameByVolumeMountPoint(ld.GUIDMountPath)
		if e == nil {
			ld.DriveMountPath = driveMountPath
		}
		utfPath, eUtf := windows.UTF16PtrFromString(ld.GUIDMountPath)
		if eUtf != nil {
			continue
		}
		if !isValidLogicalDrive(utfPath) {
			continue
		}
		label, fs, serial, eLF := QueryVolumeLabelAndFilesystemAndUniqueID(utfPath)
		if eLF == nil {
			ld.UniqueID = serial
			ld.Label = label
			ld.FileSystem = fs
		}
		dn, edn := QueryHardDiskNumberByVolume(ld.GUIDMountPath)
		if edn != nil {
			logger.Warnf("GetLogicalDriversUnderHardDisk failed to call `DriveLetterToDiskNumber`: %v, path=%v", edn, ld.GUIDMountPath)
			return nil, edn
		}
		ld.DiskNumber = dn
		if driveMountPath != "" {
			ld.DriveName = strings.TrimSuffix(ld.DriveMountPath, ":\\")
			ld.UNCPath = fmt.Sprintf(`\\.\%v:`, ld.DriveName)
			if ld.Label == "" {
				ld.Label = "本地磁盘" // TODO 如果为英文系统, 其值为`Local disk`
			}
		}
		lds = append(lds, ld)
	}
	return lds, nil
}

// QueryVolumeDriveNameByVolumeMountPoint 通过卷的GUID挂载点路径来获取卷驱动器号路径.
func QueryVolumeDriveNameByVolumeMountPoint(volumeMountPath string) (driveMountPath string, err error) {
	var rootPathLen uint32
	rootPathBuf := make([]uint16, windows.MAX_PATH+1)

	volumeMountPathPtr, _ := windows.UTF16PtrFromString(volumeMountPath)
	err = windows.GetVolumePathNamesForVolumeName(volumeMountPathPtr, &rootPathBuf[0], (windows.MAX_PATH+1)*2, &rootPathLen)
	if err != nil && errors.Is(err.(windows.Errno), windows.ERROR_MORE_DATA) {
		// 若缓存太小就重试.
		rootPathBuf = make([]uint16, (rootPathLen+1)/2)
		err = windows.GetVolumePathNamesForVolumeName(
			volumeMountPathPtr, &rootPathBuf[0], rootPathLen, &rootPathLen)
	}
	return windows.UTF16ToString(rootPathBuf), err
}

func QueryVolumeName(path *uint16) (string, error) {
	buf := make([]uint16, 1<<10)
	err := windows.GetVolumeNameForVolumeMountPoint(path, &buf[0], uint32(len(buf)))
	if err != nil {
		logger.Warnf("QueryVolumeName err=%v", err)
		return "", err
	}
	return windows.UTF16ToString(buf), nil
}

// QueryVolumeLabelAndFilesystemAndUniqueID 获取卷的名称、唯一编号及文件系统类型.
func QueryVolumeLabelAndFilesystemAndUniqueID(path *uint16) (string, string, string, error) {
	lpVolumeNameBuffer := make([]uint16, 256)
	lpVolumeSerialNumber := uint32(0)
	lpMaximumComponentLength := uint32(0)
	lpFileSystemFlags := uint32(0)
	lpFileSystemNameBuffer := make([]uint16, 256)
	err := windows.GetVolumeInformation(
		path,
		&lpVolumeNameBuffer[0],
		uint32(len(lpVolumeNameBuffer)),
		&lpVolumeSerialNumber,
		&lpMaximumComponentLength,
		&lpFileSystemFlags,
		&lpFileSystemNameBuffer[0],
		uint32(len(lpFileSystemNameBuffer)))

	uniqID := fmt.Sprintf("%X", lpVolumeSerialNumber)
	volName, _ := QueryVolumeName(path) // volName 形如: `\\?\Volume{e3b9397c-0000-0000-0000-100000000000}\`
	if strings.HasPrefix(volName, `\\?\Volume`) {
		uniqID = strings.TrimSuffix(strings.TrimPrefix(volName, `\\?\Volume{`), `}\`)
	}
	if err != nil {
		//logger.Warnf("QueryVolumeLabelAndFilesystemAndUniqueID path=%v err=%v", windows.UTF16PtrToString(path), err)
		// 若能成功获取UniqueID，那么认为是无错误的
		if uniqID != "" {
			err = nil
		}
		return "", "RAW", uniqID, err
	}

	label := syscall.UTF16ToString(lpVolumeNameBuffer)
	fs := syscall.UTF16ToString(lpFileSystemNameBuffer)
	//logger.Debugf("QueryVolumeLabelAndFilesystemAndUniqueID path=%v label=%v fs=%v id=%v", windows.UTF16PtrToString(path), label, fs, uniqID)
	return label, fs, uniqID, nil
}

// QueryLogicalDrivesUnderHardDisk 获取指定硬盘上的所有的卷信息.
func QueryLogicalDrivesUnderHardDisk(hardDisk string) (lds []LogicalDrive, err error) {
	return GetLogicalDriversUnderHardDisk(hardDisk)
}

func OpenDevice(path string) (windows.Handle, error) {
	sPath, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return windows.CreateFile(
		sPath,
		windows.GENERIC_READ,
		//windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		0,
		0,
	)
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

func IsEncryptedByBitlocker(diskPath string, volumeStartOffset int64) (bool, error) {
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
	if err = windows.ReadFile(handle, bitlockerBytes, nil, nil); err != nil {
		return false, errors.Wrapf(err, "read device")
	}
	if string(bitlockerBytes[:11]) == "\xEB\x58\x90\x2D\x46\x56\x45\x2D\x46\x53\x2D" {
		return true, nil
	}
	return false, nil
}

func StorageBusTypeToString(busType WIN_STORAGE_BUS_TYPE) string {
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

func ParseDiskNumberFromHardDiskPath(hardDisk string) (number int, err error) {
	return strconv.Atoi(strings.TrimPrefix(strings.ToUpper(hardDisk), `\\.\PHYSICALDRIVE`))
}

// isValidLogicalDrive 判定卷path(`C:\`形式的utf16ptr)是否为DRIVE_REMOVABLE、DRIVE_FIXED、DRIVE_REMOTE驱动器类型.
func isValidLogicalDrive(path *uint16) bool {
	ret := windows.GetDriveType(path)
	if ret == windows.DRIVE_NO_ROOT_DIR || ret == windows.DRIVE_CDROM || ret == windows.DRIVE_UNKNOWN || ret == windows.DRIVE_RAMDISK {
		return false
	}
	return true
}

func readNullTerminatedAscii(buf []byte, offset int) string {
	if offset <= 0 {
		return ""
	}
	buf = buf[offset:]
	for i := 0; i < len(buf); i++ {
		if buf[i] == 0 {
			return string(buf[:i])
		}
	}
	return ""
}

// StartService 启动服务.
// 注意不要使用：github.com\iamacarpet\go-win64api来管理服务
func StartService(serviceName string) (bool, error) {
	m, err := mgr.Connect()
	if err != nil {
		return false, errors.Wrapf(err, "connect service mgr")
	}
	defer m.Disconnect()
	ss, err := m.ListServices()
	if err != nil {
		return false, errors.Wrapf(err, "list services")
	}
	for _, sn := range ss {
		if sn == serviceName {
			sIns, e := m.OpenService(sn)
			if e != nil {
				return false, errors.Wrapf(e, "open service `%s`", sn)
			}
			status, e := sIns.Query()
			if e != nil {
				return false, errors.Wrapf(e, "query service `%s`", sn)
			}
			if status.State == svc.Running {
				return true, nil
			}
			if e = _startSrv(sIns, 10); e != nil {
				return false, errors.Wrapf(e, "start service `%s`", sn)
			}
			break
		}
	}
	return true, nil
}

func _startSrv(sIns *mgr.Service, waitSecs int) error {
	args := []string{"is", "manual-started", fmt.Sprintf("%d", rand.Int())}
	if err := sIns.Start(args...); err != nil {
		return err
	}
	for i := 0; ; i++ {
		status, e := sIns.Query()
		if e != nil {
			return errors.Wrapf(e, "query service `%s`", sIns.Name)
		}
		if status.State == svc.Running {
			return nil
		}
		if i > waitSecs {
			return errors.Errorf("%s state is=%d, waiting timeout", sIns.Name, svc.Running)
		}
		time.Sleep(1 * time.Second)
	}
}

func IsPhysicalEthernet(name string) bool {
	var adapters []Win32_NetworkAdapter
	query := fmt.Sprintf("SELECT Name, NetConnectionID, PhysicalAdapter, Description FROM Win32_NetworkAdapter WHERE NetConnectionID='%s'", name)
	err := wmi_.Query(query, &adapters)
	if err != nil {
		log.Printf("Failed to run WMI query: %v\n", err)
		return false
	}

	for _, adapter := range adapters {
		if strings.EqualFold(adapter.NetConnectionID, name) && adapter.PhysicalAdapter {
			// 排除虚拟网卡, TODO 不健壮.
			if strings.Contains(adapter.Description, "vEthernet") || strings.Contains(adapter.Description, "Virtual") {
				return false
			}
			return true
		}
	}

	return false
}

func QueryExtraInfoForEth(name string) (info EthernetExtraInfo, ok bool) {
	netInterfacesIPv4 := `SYSTEM\CurrentControlSet\Services\Tcpip\Parameters\Interfaces`
	// netInterfacesIPv6 := `SYSTEM\CurrentControlSet\Services\Tcpip6\Parameters\Interfaces`
	guid, guidOK := SearchNetInterfaceGUID(name)
	if !guidOK {
		return info, false
	}

	info.Physical = ExistedNetworkCard(guid)
	info.IPv4bootProto = BootProtoNone
	info.IPv6bootProto = BootProtoDHCP

	ipv4interfaceCfgHandlePath := fmt.Sprintf("%s\\%s", netInterfacesIPv4, guid)
	ipv4interfaceCfgHandle, err := registry.OpenKey(registry.LOCAL_MACHINE, ipv4interfaceCfgHandlePath, registry.READ)
	if err != nil {
		return info, true
	}
	defer ipv4interfaceCfgHandle.Close()

	ipv4enableDHCPVal, _, _ := ipv4interfaceCfgHandle.GetIntegerValue("EnableDHCP")
	info.IPv4bootProto = BootProtoStatic
	if ipv4enableDHCPVal == 1 {
		info.IPv4bootProto = BootProtoDHCP
	}

	ipv4defaultGatewayList, _, _ := ipv4interfaceCfgHandle.GetStringsValue("DefaultGateway")
	for _, ga := range ipv4defaultGatewayList {
		if !funk.InStrings(info.IPv4gatewayList, ga) {
			info.IPv4gatewayList = append(info.IPv4gatewayList, ga)
		}
	}
	ipv4dhcpDefaultGatewayList, _, _ := ipv4interfaceCfgHandle.GetStringsValue("DhcpDefaultGateway")
	for _, ga := range ipv4dhcpDefaultGatewayList {
		if !funk.InStrings(info.IPv4gatewayList, ga) {
			info.IPv4gatewayList = append(info.IPv4gatewayList, ga)
		}
	}

	ipv4dnsListStr, _, _ := ipv4interfaceCfgHandle.GetStringValue("NameServer")
	if ipv4dnsListStr != "" {
		info.IPv4dnsList = append(info.IPv4dnsList, strings.Split(ipv4dnsListStr, ",")...)
	}

	// TODO IPv6信息采集.
	return info, true
}

func ExistedNetworkCard(interfaceGuid string) bool {
	netCard := `SOFTWARE\Microsoft\Windows NT\CurrentVersion\NetworkCards`
	netCardHandle, err := registry.OpenKey(registry.LOCAL_MACHINE, netCard, registry.READ)
	if err != nil {
		return false
	}
	defer netCardHandle.Close()
	idxList, err := netCardHandle.ReadSubKeyNames(-1)
	if err != nil {
		return false
	}
	for _, idx := range idxList {
		idxNetHandlePath := fmt.Sprintf("%s\\%s", netCard, idx)
		idxNetHandleHandle, err := registry.OpenKey(registry.LOCAL_MACHINE, idxNetHandlePath, registry.READ)
		if err != nil {
			continue
		}
		srvName, _, err := idxNetHandleHandle.GetStringValue("ServiceName")
		idxNetHandleHandle.Close()
		if err != nil {
			continue
		}
		if strings.ToLower(srvName) == strings.ToLower(interfaceGuid) {
			return true
		}
	}
	return false
}

func SearchNetInterfaceGUID(name string) (guid string, ok bool) {
	networkCtl := `SYSTEM\CurrentControlSet\Control\Network`
	hardwareIDListKey, err := registry.OpenKey(registry.LOCAL_MACHINE, networkCtl, registry.READ)
	if err != nil {
		return "", false
	}
	defer hardwareIDListKey.Close()
	hardwareIDList, err := hardwareIDListKey.ReadSubKeyNames(-1)
	if err != nil {
		return "", false
	}
	for _, hid := range hardwareIDList {
		if !IsWinGUIDFormat(hid) {
			continue
		}
		interfaceGUIDListPath := fmt.Sprintf("%s\\%s", networkCtl, hid)
		interfaceGUIDListKey, err := registry.OpenKey(registry.LOCAL_MACHINE, interfaceGUIDListPath, registry.READ)
		if err != nil {
			continue
		}
		interfaceGUIDList, err := interfaceGUIDListKey.ReadSubKeyNames(-1)
		interfaceGUIDListKey.Close()
		if err != nil {
			continue
		}
		for _, iid := range interfaceGUIDList {
			if !IsWinGUIDFormat(iid) {
				continue
			}
			interfaceConnPath := fmt.Sprintf("%s\\%s\\%s\\Connection", networkCtl, hid, iid)
			interfaceConnKey, err := registry.OpenKey(registry.LOCAL_MACHINE, interfaceConnPath, registry.READ)
			if err != nil {
				continue
			}
			netConnName, _, err := interfaceConnKey.GetStringValue("Name")
			interfaceConnKey.Close()
			if err != nil {
				continue
			}
			if name == netConnName {
				return iid, true
			}
		}
	}
	return "", false
}

var sampleWinGUID windows.GUID

func IsWinGUIDFormat(guid string) bool {
	if !strings.HasPrefix(guid, "{") || !strings.HasSuffix(guid, "}") {
		return false
	}
	if len(guid) != len(sampleWinGUID.String()) {
		return false
	}
	return true
}

func IsLiveCDEnv() bool {
	// 检查是否名为winpeshl.exe
	pss, err := process.Processes()
	if err != nil {
		log.Printf("Failed to list process: %v\n", err)
		_, e := os.Stat("X:\\windows\\system32\\winpeshl.exe")
		return e == nil
	}
	for _, p := range pss {
		name, _ := p.Name()
		if strings.Contains(name, "winpeshl.exe") {
			return true
		}
	}
	return false
}

func FlushBuffers(mountpoint string) error {
	// 参考：https://msdn.microsoft.com/en-us/library/windows/desktop/aa364439(v=vs.85).aspx  \\?\Volume
	handlePath := mountpoint
	if !strings.HasPrefix(handlePath, "\\\\?\\Volume") {
		volumeName := filepath.VolumeName(mountpoint)
		handlePath = fmt.Sprintf("\\\\.\\%s", volumeName)
	}
	handle, err := OpenDevice(handlePath)
	if err != nil {
		return errors.Wrapf(err, "open device")
	}
	defer windows.Close(handle)
	if err = syscall.Fsync(syscall.Handle(handle)); err != nil {
		return errors.Wrapf(err, "sync")
	}
	return nil
}

func FindMountPoint(path string) (string, error) {
	return filepath.VolumeName(path), nil
}
