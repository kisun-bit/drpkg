package meta

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
	"golang.org/x/sys/unix"
)

const (
	W_DSYNC_MODE = os.O_WRONLY | syscall.O_DSYNC
	R_DSYNC_MODE = os.O_RDONLY | syscall.O_DSYNC | syscall.O_DIRECT
)

func customFileAttr(path string) error {
	_ = path
	return nil
}

// ResolveDevice 返回真实块设备路径和对应“磁盘名”
// diskName即/sys/class/block/<diskName>
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

	// 如果上级目录就是"block"，说明本身是磁盘
	if strings.HasSuffix(filepath.Dir(linkTarget), "block") {
		return devPath, base, nil
	}
	// 否则说明是分区，父目录名即磁盘名
	return devPath, filepath.Base(filepath.Dir(linkTarget)), nil
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
		if (stat.Mode & syscall.S_IFMT) == syscall.S_IFBLK {
			major := stat.Rdev >> 8
			minor := uint64(stat.Rdev) & 0xff
			dms[DevMajor(fmt.Sprintf("%d:%d", major, minor))] = devPath
		}
	}

	return dms, nil
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
	newpath := lvPath

	// 尝试获取其链接，若成功则直接使用，若失败则可能是快照lv、已是绝对路径的情况之一
	newpath, err = filepath.EvalSymlinks(lvPath)
	if err == nil {
		lvPath = newpath
	}

	// 排除lv快照
	if strings.HasPrefix(lvPath, "/dev/mapper") && !IsExisted(lvPath) {
		return segments, nil
	}

	if !IsExisted(lvPath) {
		return nil, errors.Errorf("LV %s does not exist", lvPath)
	}

	blockSysDir := filepath.Join("/sys/class/block", filepath.Base(lvPath))
	des, err := os.ReadDir(filepath.Join(blockSysDir, "slaves"))
	if err != nil {
		return nil, err
	}

	o, e := exec.Command("dmsetup", "table", lvPath).Output()
	if e != nil {
		return nil, e
	}

	segMap := make(map[int]Segment)
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
		for i, tableLine := range strings.Split(string(o), "\n") {
			tableLine = strings.TrimSpace(tableLine)
			tableLineFields := strings.Fields(tableLine)
			if tableLine == "" {
				continue
			}
			if len(tableLineFields) != 5 {
				return nil, errors.Errorf("unsupported dm-table: %s", tableLine)
			}
			if tableLineFields[2] != "linear" {
				// FIXME: 标识非线性数据
				return segments, nil
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
			segMap[i] = lvPartialSegment
			//segments = append(segments, lvPartialSegment)
		}
	}

	segments = SortSegments(segMap)

	return segments, nil
}

func SortSegments(segMap map[int]Segment) []Segment {
	keys := make([]int, 0, len(segMap))
	for k := range segMap {
		keys = append(keys, k)
	}

	sort.Ints(keys)

	ret := make([]Segment, 0, len(keys))
	for _, k := range keys {
		ret = append(ret, segMap[k])
	}

	return ret
}

func resolveToBlockName(dev string) (string, error) {
	realPath, err := filepath.EvalSymlinks(dev)
	if err != nil {
		return "", err
	}

	// 取最后一段，例如 /dev/dm-1 -> dm-1
	return filepath.Base(realPath), nil
}

func IsLVMDevice(dev string) (bool, error) {
	name, err := resolveToBlockName(dev)
	if err != nil {
		return false, err
	}

	uuidPath := "/sys/class/block/" + name + "/dm/uuid"

	data, err := os.ReadFile(uuidPath)
	if err != nil {
		// 不是 dm 设备（比如 sda），直接返回 false
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	uuid := strings.TrimSpace(string(data))

	return strings.HasPrefix(uuid, "LVM-"), nil
}

const BtrfsSuperMagic = 0x9123683E

// IsBtrfs reports whether the given path resides on a Btrfs filesystem.
func IsBtrfs(path string) (bool, error) {
	var st unix.Statfs_t

	if err := unix.Statfs(path, &st); err != nil {
		return false, errors.Errorf("statfs %q: %v", path, err)
	}

	return uint64(st.Type) == BtrfsSuperMagic, nil
}

func FindMountPoint(path string) (string, error) {
	absPath, err := filepath.Abs(path) // 获取绝对路径
	if err != nil {
		return "", err
	}

	for {
		var stat syscall.Stat_t
		err = syscall.Stat(absPath, &stat)
		if err != nil {
			return "", err
		}

		// 获取父目录的绝对路径
		parentPath := filepath.Dir(absPath)

		// 获取父目录的stat信息
		var parentStat syscall.Stat_t
		err = syscall.Stat(parentPath, &parentStat)
		if err != nil {
			return "", err
		}

		// 如果当前路径与父目录的设备ID不同，则当前路径是挂载点
		if stat.Dev != parentStat.Dev {
			return absPath, nil
		}

		// 如果已经到达根目录，直接返回根目录
		if absPath == parentPath {
			return absPath, nil
		}

		// 逐级向上检查
		absPath = parentPath
	}
}

type DevMountpoint struct {
	Device     string
	Major      DevMajor
	Mountpoint string
	Filesystem string
}

func VolumeMountpoints() (volumeMountpoints []DevMountpoint, err error) {
	mountpointWithDev := make(map[string]string)
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

// btrfsLogicalMirror 对应 btrfs-map-logical 输出的一行:
// mirror 1 logical 8130252800 physical 9782808576 device /dev/mapper/system-root
type btrfsLogicalMirror struct {
	Mirror   int
	Logical  int64
	Physical int64
	Device   string
}

var btrfsMapLogicalLineRe = regexp.MustCompile(
	`^mirror\s+(\d+)\s+logical\s+(\d+)\s+physical\s+(\d+)\s+device\s+(\S+)`,
)

// btrfsMapLogical 调用 `btrfs-map-logical -l <logical> <device>`,
// 解析出该 logical 地址在各个副本(mirror)上对应的物理偏移和设备。
func btrfsMapLogical(device string, logical int64) ([]btrfsLogicalMirror, error) {
	cmd := exec.Command("btrfs-map-logical", "-l", strconv.FormatInt(logical, 10), device)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, errors.Wrapf(err, "btrfs-map-logical -l %d %s failed",
			logical, device)
	}

	var mirrors []btrfsLogicalMirror
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		m := btrfsMapLogicalLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		mirrorIdx, _ := strconv.Atoi(m[1])
		logicalAddr, _ := strconv.ParseInt(m[2], 10, 64)
		physicalAddr, _ := strconv.ParseInt(m[3], 10, 64)
		mirrors = append(mirrors, btrfsLogicalMirror{
			Mirror:   mirrorIdx,
			Logical:  logicalAddr,
			Physical: physicalAddr,
			Device:   m[4],
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, errors.Wrap(err, "scan btrfs-map-logical output")
	}
	if len(mirrors) == 0 {
		return nil, errors.Errorf("no mapping parsed from btrfs-map-logical")
	}
	return mirrors, nil
}

// pickBtrfsMirror 优先选择 device 与 preferDevice 一致的副本,
// 找不到则退化为第一个副本(单设备 btrfs 场景下两者本就一致)。
func pickBtrfsMirror(mirrors []btrfsLogicalMirror, preferDevice string) btrfsLogicalMirror {
	for _, m := range mirrors {
		if m.Device == preferDevice {
			return m
		}
	}
	return mirrors[0]
}

const (
	BTRFS_IOCTL_MAGIC = 0x94
	BTRFS_IOC_SYNC    = 0x9408
)

func btrfsSync(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	_, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		f.Fd(),
		BTRFS_IOC_SYNC,
		0,
	)

	if errno != 0 {
		return errno
	}

	return nil
}
