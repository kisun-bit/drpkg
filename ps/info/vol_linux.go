package info

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kisun-bit/drpkg/command"
	"github.com/kisun-bit/drpkg/disk/table"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/pkg/errors"
)

func QueryVolumes() ([]Volume, error) {
	devMounts, err := extend.VolumeMountpoints()
	if err != nil {
		return nil, err
	}

	vols := make([]Volume, 0)

	for _, devMount := range devMounts {
		v := Volume{}
		v.Name = devMount.Device

		if devMount.Major.IsLV() {
			// 逻辑卷
			v.Segments, err = LVSegments(devMount.Device)
			if err != nil {
				return nil, err
			}
		} else {
			isDA, err := IsDirectAccessBlockDevice(devMount.Device)
			if err != nil {
				return nil, err
			}
			if !isDA {
				continue
			}
			// 普通分区或磁盘
			seg, err := DiskOrPartitionSegment(devMount.Device)
			if err != nil {
				return nil, err
			}
			v.Segments = append(v.Segments, seg)
		}

		v.MountPoint = devMount.Mountpoint
		v.GUID = DeviceUUID(devMount.Device)
		v.Filesystem = devMount.Filesystem

		ava, total, used, _, _, _, err := extend.MountpointUsage(devMount.Mountpoint)
		if err != nil {
			return nil, err
		}
		v.Usage.AvailBytes = uint64(ava)
		v.Usage.TotalBytes = uint64(total)
		v.Usage.UsedBytes = uint64(used)

		v.Size, err = extend.FileSize(devMount.Device)
		if err != nil {
			return nil, err
		}

		isDiskBootable := false
		for _, d := range v.Segments {
			if table.IsDiskBootable(d.Disk) {
				isDiskBootable = true
				break
			}
		}
		v.IsBootable = isDiskBootable && ContainsOSFileOrBootFile(v.MountPoint)

		vols = append(vols, v)
	}

	return vols, nil
}

// DiskOrPartitionSegment 计算磁盘或分区的起始偏移与大小
func DiskOrPartitionSegment(device string) (Segment, error) {
	var seg Segment

	realDev, diskName, err := resolveDevice(device)
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

// IsDirectAccessBlockDevice 判断是否是普通磁盘类设备(type==0)
func IsDirectAccessBlockDevice(device string) (bool, error) {
	_, diskName, err := resolveDevice(device)
	if err != nil {
		return false, err
	}
	t, err := getDeviceType(diskName)
	if err != nil {
		return false, err
	}
	return t == 0, nil
}

func LVSegments(lvPath string) (segments []Segment, err error) {
	if strings.HasPrefix(lvPath, "/dev/mapper") {
		if lvPath, err = filepath.EvalSymlinks(lvPath); err != nil {
			return nil, err
		}
	}
	if !extend.IsExisted(lvPath) {
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

		slaveDeviceMajorTable, err := extend.DeviceMajorTable(devicePath)
		if err != nil {
			return nil, err
		}
		slaveDeviceMajor := extend.DevMajor("")
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
			lvPartialDevMajor := extend.DevMajor(tableLineFields[3])
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

// resolveDevice 返回真实块设备路径和对应“磁盘名”
// diskName 即 /sys/class/block/<diskName>
func resolveDevice(devPath string) (realPath, diskName string, err error) {
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

// getDeviceType 返回 SCSI peripheral type (0=direct-access,5=CD-ROM...)
func getDeviceType(diskName string) (int, error) {
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
