package info

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kisun-bit/drpkg/command"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/ps/table"
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
			// 普通分区或磁盘
			seg, err := DiskOrPartitionSegment(devMount.Device)
			if err != nil {
				return nil, err
			}
			v.Segments = append(v.Segments, seg)
		}

		v.MountPoint = devMount.Mountpoint
		v.UUID = DeviceUUID(devMount.Device)
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
		v.IsBootable = isDiskBootable && ContainsOSFile(v.MountPoint)

		vols = append(vols, v)
	}

	return vols, nil
}

func DiskOrPartitionSegment(device string) (segment Segment, err error) {
	if !extend.IsExisted(device) {
		return segment, errors.Errorf("device %s does not exist", device)
	}

	isDisk := true
	deviceClassPath := filepath.Join("/sys/class/block", filepath.Base(device))
	deviceClassLinkTarget, err := filepath.EvalSymlinks(deviceClassPath)
	if err != nil {
		return segment, err
	}

	if strings.HasSuffix(filepath.Dir(deviceClassLinkTarget), "block") {
		isDisk = true
	} else {
		isDisk = false
	}

	if !isDisk {
		segment.Disk = filepath.Join("/dev", filepath.Base(filepath.Dir(deviceClassLinkTarget)))
		partStartBin, err := os.ReadFile(filepath.Join(deviceClassPath, "start"))
		if err != nil {
			return segment, err
		}
		partStartBin = bytes.TrimSpace(partStartBin)
		startSector, err := strconv.ParseUint(string(partStartBin), 10, 64)
		if err != nil {
			return segment, err
		}
		segment.Start = startSector * 512
	} else {
		segment.Disk = device
	}

	sizeBin, err := os.ReadFile(filepath.Join(deviceClassPath, "size"))
	if err != nil {
		return segment, err
	}
	sizeBin = bytes.TrimSpace(sizeBin)
	sectors, err := strconv.ParseUint(string(sizeBin), 10, 64)
	if err != nil {
		return segment, err
	}
	segment.Size = sectors * 512

	return segment, nil
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
