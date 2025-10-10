package info

import (
	"strings"

	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/ps/table"
	"github.com/pkg/errors"
)

func QueryDisks() (disks []Disk, err error) {
	diskPaths, err := extend.ListDisks()
	if err != nil {
		return nil, err
	}
	for _, diskPath := range diskPaths {
		d := Disk{}
		d.Name = strings.ToUpper(diskPath)
		d.Device = diskPath
		geo, err := extend.GetDiskGeometry(diskPath)
		if err != nil {
			return nil, errors.Wrapf(err, "GetDiskGeometry for %s", diskPath)
		}
		d.Sectors = int64(geo.Cylinders) * int64(geo.TracksPerCylinder) * int64(geo.SectorsPerTrack)
		d.SectorSize = int(geo.BytesPerSector)
		d.Size = d.Sectors * int64(d.SectorSize)
		diskProperty, err := extend.DiskProperty(diskPath)
		if err != nil {
			return nil, errors.Wrapf(err, "GetDiskProperty for %s", diskPath)
		}
		d.Vendor = diskProperty.VendorId
		d.Model = diskProperty.ProductId
		d.SerialNumber = diskProperty.SerialNumber
		offline, readonly, err := extend.GetDiskAttr(diskPath)
		if err != nil {
			return nil, errors.Wrapf(err, "GetDiskAttr for %s", diskPath)
		}
		d.IsReadOnly = readonly
		d.IsOnline = !offline
		d.Table, err = GetDiskTable(diskPath)
		if err != nil {
			return nil, errors.Wrapf(err, "GetDiskTable for %s", diskPath)
		}
		// 检测是否是动态磁盘
		for _, p := range d.Table.Partitions {
			if d.IsMsDynamic {
				break
			}
			switch p.Type {
			case table.MBR_DYNAMIC_PARTITION, table.GPT_MSFT_LDM_METADATA, table.GPT_MSFT_LDM_DATA:
				d.IsMsDynamic = true
				continue
			}
		}
		extendDiskGUID(&d)
		disks = append(disks, d)
	}
	return disks, nil
}
