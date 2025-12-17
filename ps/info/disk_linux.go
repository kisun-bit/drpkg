package info

import (
	"path/filepath"
	"strings"

	"github.com/kisun-bit/drpkg/extend"
)

func QueryDisks() (disks []Disk, err error) {
	diskPaths, err := extend.ListDisks()
	if err != nil {
		return nil, err
	}
	for _, diskPath := range diskPaths {
		d := Disk{}
		d.Name = diskPath
		d.Device = diskPath

		d.LogicalSectorSize, err = extend.DiskLogicalSectorSize(diskPath)
		if err != nil {
			return nil, err
		}
		d.PhysicalSectorSize, err = extend.DiskPhysicalSectorSize(diskPath)
		if err != nil {
			return nil, err
		}

		sectors, e := extend.GetDiskSectors(diskPath)
		if e != nil {
			return nil, e
		}
		d.Size = sectors * 512
		d.Sectors = d.Size / int64(d.LogicalSectorSize)

		d.Vendor, err = extend.GetDiskVendor(diskPath)
		if err != nil {
			return nil, err
		}
		d.Model, err = extend.GetDiskModel(diskPath)
		if err != nil {
			return nil, err
		}
		d.SerialNumber, err = extend.GetDiskSerialNumber(diskPath)
		if err != nil {
			return nil, err
		}
		d.Bus, _ = GetDiskBusType(diskPath)

		d.IsReadOnly, err = extend.IsDiskReadonly(diskPath)
		if err != nil {
			return nil, err
		}
		d.Table, err = GetDiskTable(diskPath)
		if err != nil {
			return nil, err
		}
		if err = extendDiskGUID(&d); err != nil {
			return nil, err
		}
		d.IsMsDynamic = false
		d.IsOnline = true
		disks = append(disks, d)
	}
	return disks, nil
}

func GetDiskBusType(dev string) (string, error) {
	sysPath := filepath.Join("/sys/block", filepath.Base(dev), "device")

	realPath, err := filepath.EvalSymlinks(sysPath)
	if err != nil {
		return "", err
	}

	switch {
	case strings.Contains(realPath, "virtio"):
		return "virtio", nil
	case strings.Contains(realPath, "nvme"):
		return "nvme", nil
	case strings.Contains(realPath, "usb"):
		return "usb", nil
	case strings.Contains(realPath, "ata"):
		return "sata", nil
	case strings.Contains(realPath, "scsi"):
		return "scsi", nil
	case strings.Contains(realPath, "mmc"):
		return "mmc", nil
	default:
		return "unknown", nil
	}
}
