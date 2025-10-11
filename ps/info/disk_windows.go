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
		d.SectorSize = int(geo.BytesPerSector)
		sad, err := extend.DiskAlignmentStorage(diskPath)
		if err != nil {
			return nil, errors.Wrapf(err, "DiskAlignmentStorage for %s", diskPath)
		}
		d.LogicalSectorSize = int(sad.BytesPerLogicalSector)
		d.PhysicalSectorSize = int(sad.BytesPerPhysicalSector)
		size, err := extend.FileSize(diskPath)
		if err != nil {
			return nil, errors.Wrapf(err, "FileSize for %s", diskPath)
		}
		d.Size = int64(size)
		// 注意：现代硬盘的真实容量不再与 CHS 信息匹配
		// 因此，通过CHS计算出来的实际容量会比真实硬盘容量更小，实际场景中请使用 IOCTL_DISK_GET_LENGTH_INFO 获取真实硬盘容量
		// d.Sectors = int64(geo.Cylinders) * int64(geo.TracksPerCylinder) * int64(geo.SectorsPerTrack)
		d.Sectors = d.Size / int64(d.SectorSize)
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
		if err = extendDiskGUID(&d); err != nil {
			return nil, err
		}
		disks = append(disks, d)
	}
	return disks, nil
}
