package info

import (
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
		d.Sectors, err = extend.GetDiskSectors(diskPath)
		if err != nil {
			return nil, err
		}
		ss, e := extend.GetDiskSectorSize(diskPath)
		if e != nil {
			return nil, e
		}
		d.SectorSize = int(ss)
		d.LogicalSectorSize, err = extend.DiskLogicalSectorSize(diskPath)
		if err != nil {
			return nil, err
		}
		d.PhysicalSectorSize, err = extend.DiskPhysicalSectorSize(diskPath)
		if err != nil {
			return nil, err
		}
		d.Size = ss * d.Sectors
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
