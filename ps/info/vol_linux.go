package info

import (
	"github.com/kisun-bit/drpkg/disk/table"
	"github.com/kisun-bit/drpkg/extend"
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
		v.Layout = extend.VolumeTypeSimple
		v.SegmentLayoutType = extend.SegmentLayoutTypeLine

		switch {
		case devMount.Major.IsLV():
			// 逻辑卷
			v.Segments, err = extend.LVSegments(devMount.Device)
			if err != nil {
				return nil, err
			}
		case devMount.Major.IsMultipath():
			// TODO 多路径
			continue
		default:
			isDA, err := extend.IsNormalDiskDevice(devMount.Device)
			if err != nil {
				return nil, err
			}
			if !isDA {
				continue
			}
			// 普通分区或磁盘
			seg, err := extend.DiskOrPartitionSegment(devMount.Device)
			if err != nil {
				return nil, err
			}
			v.Segments = append(v.Segments, seg)
		}

		v.MountPoint = devMount.Mountpoint
		v.GUID = extend.DeviceUUID(devMount.Device)
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
