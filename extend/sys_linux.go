package extend

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/kisun-bit/drpkg/command"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
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

		switch runtime.GOARCH {
		case "386":
			_, _, errno = unix.Syscall(unix.SYS_IOCTL, f.Fd(), LinuxIOCTLGetBlockSize, uintptr(unsafe.Pointer(&size)))
			size <<= 9
		case "amd64", "arm64":
			_, _, errno = unix.Syscall(unix.SYS_IOCTL, f.Fd(), LinuxIOCTLGetBlockSize64, uintptr(unsafe.Pointer(&size)))
		}

		if errno != 0 {
			return getSizeFromSysfs(fileName)
		}
		return size, nil
	} else {
		return uint64(info.Size()), nil
	}
}

func getSizeFromSysfs(dev string) (uint64, error) {
	devname := filepath.Base(dev)
	data, err := os.ReadFile("/sys/class/block/" + devname + "/size")
	if err != nil {
		return 0, errors.Wrapf(err, "failed to read /sys/class/block/%s/size", devname)
	}

	sectors, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, errors.Wrapf(err, "failed to parse /sys/class/block/%s/size", devname)
	}

	// Linux这里的size单位是512-byte sectors
	return sectors * 512, nil
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

type DevMountpoint struct {
	Device     string
	Major      DevMajor
	Mountpoint string
	Filesystem string
}

type DevMajor string

func (dm DevMajor) splitMajor() (major, minor string) {
	parts := strings.SplitN(string(dm), ":", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

func (dm DevMajor) MajorNum() string {
	major, _ := dm.splitMajor()
	return major
}

func (dm DevMajor) MinNum() string {
	_, minor := dm.splitMajor()
	return minor
}

func (dm DevMajor) IsLV() bool {
	//return dm.MajorNum() == "253"

	dmNamePath := fmt.Sprintf("/sys/dev/block/%s/dm/name", dm)
	lvName, err := ReadStringFromFile(dmNamePath)
	if err != nil {
		return false
	}
	lvPath := fmt.Sprintf("/dev/mapper/%s", lvName)

	cmdline := fmt.Sprintf("lvdisplay %s", lvPath)
	r, _, _ := command.Execute(cmdline)
	return r == 0
}

func VolumeMountpoints() (volumeMountpoints []DevMountpoint, err error) {
	mountpointWithDev := make(map[string]string, 0)
	mountBinText, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(bytes.NewReader(mountBinText))
	for scanner.Scan() {
		lineGroups := strings.Split(scanner.Text(), " - ")
		if len(lineGroups) != 2 {
			continue
		}
		fieldsPre := strings.Fields(lineGroups[0])
		fieldsSuf := strings.Fields(lineGroups[1])

		if len(fieldsPre) < 5 || len(fieldsSuf) < 2 {
			continue
		}
		mountpoint, mountdev, major := fieldsPre[4], fieldsSuf[1], fieldsPre[2]
		if _, ok := mountpointWithDev[mountpoint]; ok {
			// 挂载点被设备重复挂载的，始终以第一挂载设备为主
			continue
		}
		if !strings.HasPrefix(mountdev, "/dev") {
			continue
		}
		mountpointWithDev[mountpoint] = mountdev
		volumeMountpoints = append(volumeMountpoints, DevMountpoint{
			Device:     mountdev,
			Major:      DevMajor(major),
			Mountpoint: mountpoint,
			Filesystem: fieldsSuf[0],
		})
	}

	return volumeMountpoints, nil
}

func DeviceMajorTable(filter ...string) (map[DevMajor]string, error) {
	dms := make(map[DevMajor]string)

	entries, err := os.ReadDir("/dev")
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		devPath := filepath.Join("/dev", entry.Name())
		if len(filter) != 0 && !funk.InStrings(filter, devPath) {
			continue
		}

		var stat syscall.Stat_t
		if err := syscall.Stat(devPath, &stat); err != nil {
			continue
		}

		// 检查是否是设备文件
		if stat.Mode&syscall.S_IFCHR != 0 || stat.Mode&syscall.S_IFBLK != 0 {
			major := uint64(stat.Rdev) >> 8
			minor := uint64(stat.Rdev) & 0xff
			dms[DevMajor(fmt.Sprintf("%d:%d", major, minor))] = devPath
		}
	}

	return dms, nil
}

// MountpointUsage 存储使用情况 (available bytes, byte capacity, byte usage, total inodes, inodes free, inode usage, error)
func MountpointUsage(path string) (int64, int64, int64, int64, int64, int64, error) {
	statfs := &unix.Statfs_t{}
	err := unix.Statfs(path, statfs)
	if err != nil {
		return 0, 0, 0, 0, 0, 0, err
	}

	available := int64(statfs.Bavail) * statfs.Bsize

	capacity := int64(statfs.Blocks) * statfs.Bsize

	usage := (int64(statfs.Blocks) - int64(statfs.Bfree)) * statfs.Bsize

	inodes := int64(statfs.Files)
	inodesFree := int64(statfs.Ffree)
	inodesUsed := inodes - inodesFree

	return available, capacity, usage, inodes, inodesFree, inodesUsed, nil
}

func GetBootTime() (time.Time, error) {
	content, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return time.Time{}, errors.Wrapf(err, "read /proc/uptime")
	}

	fields := strings.Fields(string(content))
	if len(fields) < 1 {
		return time.Time{}, errors.Wrapf(err, "fields /proc/uptime")
	}

	uptimeSeconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return time.Time{}, errors.Wrapf(err, "parse uptime from /proc/uptime")
	}

	bootTime := time.Now().Add(-time.Duration(uptimeSeconds * float64(time.Second)))
	return bootTime, nil
}

func ListDisks() (disks []string, err error) {
	sysBlockDir := "/sys/class/block"
	subFiles, err := os.ReadDir(sysBlockDir)
	if err != nil {
		return nil, err
	}

	for _, file := range subFiles {
		filename := file.Name()
		disk := filepath.Join("/dev", filename)

		_, diskName, e := ResolveDevice(disk)
		if e != nil {
			return nil, e
		}
		if filename != diskName {
			continue
		}

		ok, e := IsNormalDiskDevice(disk)
		if e != nil {
			return nil, e
		}
		if !ok {
			continue
		}

		disks = append(disks, disk)
	}

	sort.Strings(disks)
	return disks, nil
}

func GetDiskSectors(disk string) (int64, error) {
	r, e := ReadIntFromFile(filepath.Join("/sys/class/block", filepath.Base(disk), "size"))
	return r, errors.Wrapf(e, "get disk sectos")
}

func GetDiskSectorSize(disk string) (int64, error) {
	r, e := ReadIntFromFile(filepath.Join("/sys/class/block", filepath.Base(disk), "queue/hw_sector_size"))
	return r, errors.Wrapf(e, "get disk secto-size")
}

func GetDiskVendor(disk string) (string, error) {
	ret, err := ReadStringFromFile(filepath.Join("/sys/class/block", filepath.Base(disk), "device/vendor"))
	if err == nil {
		return ret, nil
	}
	if errors.Is(err, syscall.ENXIO) || os.IsNotExist(err) {
		return "", nil
	}
	return "", errors.Wrapf(err, "get disk vendor")
}

func GetDiskModel(disk string) (string, error) {
	ret, err := ReadStringFromFile(filepath.Join("/sys/class/block", filepath.Base(disk), "device/model"))
	if err == nil {
		return ret, nil
	}
	if errors.Is(err, syscall.ENXIO) || os.IsNotExist(err) {
		return "", nil
	}
	return "", errors.Wrapf(err, "get disk model")
}

func GetDiskSerialNumber(disk string) (string, error) {
	ret, err := os.ReadFile(filepath.Join("/sys/class/block", filepath.Base(disk), "device/wwid"))
	if err == nil && len(ret) > 4 {
		return strings.TrimSpace(string(ret[4:])), nil
	}
	if errors.Is(err, syscall.ENXIO) || os.IsNotExist(err) {
		// 获取vpd_80序列号
		vpdret, vpderr := os.ReadFile(filepath.Join("/sys/class/block", filepath.Base(disk), "device/vpd_pg80"))
		if vpderr == nil && len(vpdret) > 4 {
			return strings.TrimSpace(string(vpdret[4:])), nil
		}
		return "", nil
	}
	return "", errors.Wrapf(err, "get disk serial number")
}

func IsDiskReadonly(disk string) (bool, error) {
	r, e := ReadIntFromFile(filepath.Join("/sys/class/block", filepath.Base(disk), "ro"))
	return r != 0, errors.Wrapf(e, "get disk read-only attribute")
}

func IsDiskRemovable(disk string) (bool, error) {
	ret, err := ReadStringFromFile(filepath.Join("/sys/class/block", filepath.Base(disk), "removable"))
	if err == nil {
		return ret != "0", nil
	}
	if errors.Is(err, syscall.ENXIO) || os.IsNotExist(err) {
		return false, nil
	}
	return false, errors.Wrapf(err, "get disk removable attr")
}

func BytesPerSector(dev string) (int, error) {
	return DiskLogicalSectorSize(dev)
}

func DiskLogicalSectorSize(dev string) (int, error) {
	base := filepath.Base(dev)
	p := fmt.Sprintf("/sys/class/block/%s/queue/logical_block_size", base)
	i, err := ReadIntFromFile(p)
	if err != nil {
		return 0, err
	}
	return int(i), nil
}

func DiskPhysicalSectorSize(dev string) (int, error) {
	base := filepath.Base(dev)
	p := fmt.Sprintf("/sys/class/block/%s/queue/physical_block_size", base)
	i, err := ReadIntFromFile(p)
	if err != nil {
		return 0, err
	}
	return int(i), nil
}

func DiskSectorAlignment(dev string) (sa StorageAlignment, err error) {
	sa.PhysicalSectorSize, err = DiskPhysicalSectorSize(dev)
	if err != nil {
		return
	}
	sa.LogicalSectorSize, err = DiskLogicalSectorSize(dev)
	if err != nil {
		return
	}
	return
}

// IsDirectAccessBlockDevice 判断是否是普通磁盘类设备(type==0)
func IsDirectAccessBlockDevice(device string) (bool, error) {
	_, diskName, err := ResolveDevice(device)
	if err != nil {
		return false, err
	}
	t, err := GetDeviceType(diskName)
	if err != nil {
		return false, err
	}
	return t == 0, nil
}

func IsNormalDiskDevice(device string) (bool, error) {
	_, diskName, err := ResolveDevice(device)
	if err != nil {
		return false, err
	}

	path := filepath.Join("/sys/class/block", diskName, "device/subsystem")

	linkTarget, err := filepath.EvalSymlinks(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if linkTarget == "" {
		return false, nil
	}

	if strings.HasPrefix(filepath.Base(diskName), "sr") {
		return false, nil
	}

	ro, _ := IsDiskReadonly(path)
	removable, _ := IsDiskRemovable(path)
	if ro && removable {
		return false, nil
	}

	return true, nil
}

func DeviceUUID(device string) string {
	if strings.HasPrefix(device, "/dev/mapper") {
		device, _ = filepath.EvalSymlinks(device)
	}
	uuidDevRoot := "/dev/disk/by-uuid"
	des, err := os.ReadDir(uuidDevRoot)
	if err != nil {
		return ""
	}
	if len(des) == 0 {
		return ""
	}
	for _, d := range des {
		uuid_ := d.Name()
		link, _ := filepath.EvalSymlinks(filepath.Join(uuidDevRoot, uuid_))
		if filepath.Base(link) == filepath.Base(device) {
			return uuid_
		}
	}
	return ""
}

// GetDeviceType 返回 SCSI peripheral type (0=direct-access,5=CD-ROM...)
func GetDeviceType(diskName string) (int, error) {
	b, err := os.ReadFile(filepath.Join("/sys/class/block", diskName, "device", "type"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return -1, nil // 某些虚拟块设备可能没有 type
		}
		return -1, err
	}
	t, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return -1, err
	}
	return t, nil
}

// ResolveDevice 返回真实块设备路径和对应“磁盘名”
// diskName 即 /sys/class/block/<diskName>
func ResolveDevice(devPath string) (realPath, diskName string, err error) {
	stat, err := os.Lstat(devPath)
	if err != nil {
		return "", "", err
	}
	if stat.Mode()&os.ModeSymlink != 0 {
		devPath, err = filepath.EvalSymlinks(devPath)
		if err != nil {
			return "", "", err
		}
	}

	base := filepath.Base(devPath)
	sysPath := filepath.Join("/sys/class/block", base)
	linkTarget, err := filepath.EvalSymlinks(sysPath)
	if err != nil {
		return "", "", err
	}

	// 如果上级目录就是 "block"，说明本身是磁盘
	if strings.HasSuffix(filepath.Dir(linkTarget), "block") {
		return devPath, base, nil
	}
	// 否则说明是分区，父目录名即磁盘名
	return devPath, filepath.Base(filepath.Dir(linkTarget)), nil
}

// DiskOrPartitionSegment 计算磁盘或分区的起始偏移与大小
func DiskOrPartitionSegment(device string) (Segment, error) {
	var seg Segment

	realDev, diskName, err := ResolveDevice(device)
	if err != nil {
		return seg, err
	}

	// 判断是否是磁盘本体还是分区
	sysPath := filepath.Join("/sys/class/block", filepath.Base(realDev))
	linkTarget, _ := filepath.EvalSymlinks(sysPath)
	isDisk := strings.HasSuffix(filepath.Dir(linkTarget), "block")

	if !isDisk {
		seg.Disk = filepath.Join("/dev", diskName)
		startBytes, err := os.ReadFile(filepath.Join(sysPath, "start"))
		if err != nil {
			return seg, err
		}
		start, err := strconv.ParseUint(strings.TrimSpace(string(startBytes)), 10, 64)
		if err != nil {
			return seg, err
		}
		// /sys/class/block/sda1/start的单位始终是512字节扇区（"kernel sector"）
		seg.Start = start * 512
	} else {
		seg.Disk = realDev
	}

	sizeBytes, err := os.ReadFile(filepath.Join(sysPath, "size"))
	if err != nil {
		return seg, err
	}
	sectors, err := strconv.ParseUint(strings.TrimSpace(string(sizeBytes)), 10, 64)
	if err != nil {
		return seg, err
	}
	// /sys/class/block/sda/size的单位始终是512字节扇区（"kernel sector"）
	seg.Size = sectors * 512
	return seg, nil
}

func LVSegments(lvPath string) (segments []Segment, err error) {
	if strings.HasPrefix(lvPath, "/dev/mapper") {
		if lvPath, err = filepath.EvalSymlinks(lvPath); err != nil {
			return nil, err
		}
	}
	if !IsExisted(lvPath) {
		return nil, errors.Errorf("LV %s does not exist", lvPath)
	}

	blockSysDir := filepath.Join("/sys/class/block", filepath.Base(lvPath))
	des, err := os.ReadDir(filepath.Join(blockSysDir, "slaves"))
	if err != nil {
		return nil, err
	}

	_, o, err := command.Execute("dmsetup table " + lvPath)
	if err != nil {
		return nil, err
	}

	for _, d := range des {
		diskOrPartitionName := d.Name()
		devicePath := filepath.Join("/dev", diskOrPartitionName)
		seg, err := DiskOrPartitionSegment(devicePath)
		if err != nil {
			return nil, err
		}

		slaveDeviceMajorTable, err := DeviceMajorTable(devicePath)
		if err != nil {
			return nil, err
		}
		slaveDeviceMajor := DevMajor("")
		for major, _ := range slaveDeviceMajorTable {
			slaveDeviceMajor = major
			break
		}
		if slaveDeviceMajor == "" {
			return nil, errors.Errorf("major of %s not found", devicePath)
		}

		lvPartialSegment := seg
		for _, tableLine := range strings.Split(o, "\n") {
			tableLine = strings.TrimSpace(tableLine)
			tableLineFields := strings.Fields(tableLine)
			if tableLine == "" {
				continue
			}
			if len(tableLineFields) != 5 || tableLineFields[2] != "linear" {
				return nil, errors.Errorf("unsupported dm-table: %s", tableLine)
			}
			lvPartialDevMajor := DevMajor(tableLineFields[3])
			if lvPartialDevMajor != slaveDeviceMajor {
				continue
			}
			lvPartialStartSector, err := strconv.ParseUint(tableLineFields[4], 10, 64)
			if err != nil {
				return nil, err
			}
			lvPartialSectors, err := strconv.ParseUint(tableLineFields[1], 10, 64)
			if err != nil {
				return nil, err
			}
			// LVM的扇区大小固定为512，见https://wiki.gentoo.org/wiki/Device-mapper
			// 原文如下：
			// """
			// The device mapper, like the rest of the Linux block layer deals with things at the sector level.
			// A sector defined as 512 bytes, regardless of the actual physical geometry the the block device.
			// All formulas and values to the device mapper will be in sectors unless otherwise stated
			// """
			lvPartialSegment.Start += lvPartialStartSector * 512
			lvPartialSegment.Size = lvPartialSectors * 512
			segments = append(segments, lvPartialSegment)
		}
	}

	return segments, nil
}

func FileDiskExtents(file string) (es []FileDiskExtentSegment, err error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, errors.Errorf("%s is a directory", file)
	}
	stat := info.Sys().(*syscall.Stat_t)
	fileSize := info.Size()
	fileMaj := uint32(stat.Dev >> 8)
	fileMin := uint32(stat.Dev & 0xff)

	devNameTable, err := DeviceMajorTable()
	if err != nil {
		return nil, err
	}
	fileDevMaj := DevMajor(fmt.Sprintf("%d:%d", fileMaj, fileMin))

	volume, ok := devNameTable[fileDevMaj]
	if !ok || volume == "" {
		return nil, errors.Errorf("device path of %s not found", volume)
	}

	eArr, err := getFileExtentsFp(f)
	if err != nil {
		return nil, err
	}

	fileRegionsOnVolume := make([]volumeSegment, 0)
	sz := int64(0)
	for _, d := range eArr {
		if sz >= fileSize {
			break
		}
		expectSize := fileSize - sz
		if expectSize > int64(d.Length) {
			expectSize = int64(d.Length)
		}
		fileRegionsOnVolume = append(fileRegionsOnVolume, volumeSegment{
			start: int64(d.Physical),
			size:  expectSize,
		})
		sz += expectSize
	}
	if len(fileRegionsOnVolume) == 0 {
		return nil, errors.Errorf("file %s has no regions on volume", file)
	}
	//spew.Dump(fileRegionsOnVolume)

	volumeRegionsOnDisk := make([]Segment, 0)
	if fileDevMaj.IsLV() {
		volumeRegionsOnDisk, err = LVSegments(volume)
		if err != nil {
			return nil, err
		}
	} else {
		// FIXME 兼容multipath和RAID
		seg, err := DiskOrPartitionSegment(volume)
		if err != nil {
			return nil, err
		}
		volumeRegionsOnDisk = append(volumeRegionsOnDisk, seg)
	}
	if len(volumeRegionsOnDisk) == 0 {
		return nil, errors.Errorf("volume %s has no regions on disk", volume)
	}
	//fmt.Println("------------------------")
	//spew.Dump(volumeRegionsOnDisk)

	for _, fe := range fileRegionsOnVolume {
		extentSize := fe.size
		fileExtentVolStart := fe.start
		fileExtentVolEnd := fileExtentVolStart + extentSize
		diskDelta := fe.start

		diskVolStart := int64(0)
		for _, ve := range volumeRegionsOnDisk {
			diskVolEnd := diskVolStart + int64(ve.Size)
			diskDelta = fileExtentVolStart - diskVolStart

			if fileExtentVolStart < diskVolStart {
				return nil, errors.New("unexcepted range")
			} else if fileExtentVolStart >= diskVolStart && fileExtentVolEnd <= diskVolEnd {
				// 全包含
				es = append(es, FileDiskExtentSegment{
					Disk:  ve.Disk,
					Start: int64(ve.Start) + diskDelta,
					Size:  extentSize,
				})
				break
			} else if fileExtentVolStart < diskVolEnd && fileExtentVolEnd > diskVolEnd {
				// 部分包含，做截断处理
				deltaExtentSize := diskVolEnd - fileExtentVolStart
				es = append(es, FileDiskExtentSegment{
					Disk:  ve.Disk,
					Start: int64(ve.Start) + diskDelta,
					Size:  deltaExtentSize,
				})
				extentSize -= deltaExtentSize
				fileExtentVolStart += deltaExtentSize
			}

			diskVolStart = diskVolEnd
		}
	}

	//fmt.Println(len(es))
	return es, nil
}
