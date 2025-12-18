package info

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kisun-bit/drpkg/command"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/pkg/errors"
)

var xenbusSysPathMatch = regexp.MustCompile(`/devices/vbd-\d+/block/`)

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
	cmdline := fmt.Sprintf("udevadm info --query=all --export --name=%s", filepath.Base(dev))
	_, o, e := command.Execute(cmdline)
	if e != nil {
		return "unknown", errors.Errorf("error getting disk bus type: %s", e)
	}

	var busStr string
	var sysPath string

	for _, line := range strings.Split(o, "\n") {
		if busStr != "" && sysPath != "" {
			break
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		items := strings.Split(line, ":")
		if len(items) < 2 {
			continue
		}

		kvStr := strings.TrimSpace(items[1])
		if strings.HasPrefix(kvStr, "ID_BUS=") && busStr == "" {
			busStr = strings.TrimSpace(strings.TrimLeft(kvStr, "ID_BUS="))
		}

		if items[0] == "P" && sysPath == "" {
			sysPath = strings.TrimSpace(strings.TrimLeft(line, "P:"))
		}
	}

	switch busStr {
	case "ata":
		return "ata", nil
	case "usb":
		return "usb", nil
	case "scsi":
		return "scsi", nil
	case "virtio":
		return "virtio", nil
	case "":
		if strings.Contains(sysPath, "/virtio") {
			return "virtio", nil
		}
		if strings.Contains(sysPath, "/nvme/") {
			return "nvme", nil
		}
		if strings.Contains(sysPath, "/virtual/block/nbd") {
			return "nbd", nil
		}
		if strings.Contains(sysPath, "/virtual/block/loop") {
			return "loop", nil
		}
		if xenbusSysPathMatch.MatchString(sysPath) {
			return "xenbus", nil
		}
		fallthrough
	default:
		return "unknown", nil
	}
}
