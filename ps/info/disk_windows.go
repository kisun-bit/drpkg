package info

import (
	"strings"

	"github.com/kisun-bit/drpkg/disk/table"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/pkg/errors"
	"golang.org/x/sys/windows"
)

func QueryDisks() (disks []Disk, err error) {
	diskPaths, err := extend.ListDisksV2()
	if err != nil {
		return nil, errors.Wrapf(err, "list disks")
	}

	for _, dp := range diskPaths {

		diskPath := dp.DeviceID

		d := Disk{}
		d.Name = strings.ToUpper(dp.DeviceID)
		d.Device = dp.DeviceID
		d.PathId = dp.PNPDeviceID

		sad, e := extend.DiskAlignmentStorage(diskPath)
		if e == nil {
			d.LogicalSectorSize = int(sad.BytesPerLogicalSector)
			d.PhysicalSectorSize = int(sad.BytesPerPhysicalSector)
		} else {
			// 经测试，在win2k8r2上，IOCTL_STORAGE_QUERY_PROPERTY不受支持，
			// 因此我们以DISK_GEOMETRY数据为准即可
			if errors.Is(err, windows.ERROR_NOT_SUPPORTED) {
				d.LogicalSectorSize = 512
				d.PhysicalSectorSize = 512
			} else {
				return nil, errors.Wrapf(err, "DiskAlignmentStorage for %s", diskPath)
			}
		}

		size, e := extend.FileSize(diskPath)
		if e != nil {
			return nil, errors.Wrapf(e, "FileSize for %s", diskPath)
		}
		d.Size = int64(size)
		// 注意：现代硬盘的真实容量不再与 CHS 信息匹配
		// 因此，通过CHS计算出来的实际容量会比真实硬盘容量更小，实际场景中请使用 IOCTL_DISK_GET_LENGTH_INFO 获取真实硬盘容量
		// d.Sectors = int64(geo.Cylinders) * int64(geo.TracksPerCylinder) * int64(geo.SectorsPerTrack)
		d.Sectors = d.Size / int64(d.LogicalSectorSize)
		diskProperty, e := extend.DiskProperty(diskPath)
		if e != nil {
			return nil, errors.Wrapf(e, "GetDiskProperty for %s", diskPath)
		}
		d.Bus = diskProperty.BusType.String()

		d.Vendor = diskProperty.VendorId
		d.Model = diskProperty.ProductId
		d.SerialNumber = diskProperty.SerialNumber
		offline, readonly, e := extend.GetDiskAttr(diskPath)
		if e != nil {
			return nil, errors.Wrapf(e, "GetDiskAttr for %s", diskPath)
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
