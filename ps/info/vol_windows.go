package info

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"syscall"

	"github.com/kisun-bit/drpkg/disk/table"
	"github.com/kisun-bit/drpkg/extend"
)

func QueryVolumes() ([]Volume, error) {
	//mountpoints, err := extend.VolumeMountpoints()
	//if err != nil {
	//	return nil, err
	//}
	volumes, err := extend.ListWin32VolumeByWMI()
	if err != nil {
		return nil, err
	}

	volTypeTable, err := extend.QueryMsVolumeTypeTable()
	if err != nil {
		return nil, err
	}

	vols := make([]Volume, 0)

	for _, v := range volumes {
		drvMountpoint := strings.TrimSuffix(v.Name, "\\")
		if strings.HasSuffix(v.Name, ":\\") {
			drvMountpoint = strings.TrimSuffix(v.Name, "\\")
		}

		mountpoint := strings.TrimSuffix(v.DeviceID, "\\")
		if IsMemoryOS() && strings.ToLower(v.Name) == "x:\\" {
			continue
		}
		if v.Capacity == 0 || v.DriveType != 3 {
			continue
		}

		des, err := extend.VolumeMountpointToExtents(mountpoint)
		if err != nil {
			var ec syscall.Errno
			if errors.As(err, &ec) && ec == 1 {
				continue
			}
			if errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
				return nil, err
			}
			continue
		}
		if len(des) == 0 {
			continue
		}

		curVol := Volume{}
		curVol.Name = fmt.Sprintf("Volume (%s)", v.Name)
		curVol.MountPoint = drvMountpoint

		curVol.Layout = extend.VolumeTypeSimple
		vt, ok := volTypeTable[strings.ToLower(string(v.Name[0]))]
		if ok {
			curVol.Layout = vt
		}

		switch curVol.Layout {
		case extend.VolumeTypeMsRaid5, extend.VolumeTypeMsStripe:
			curVol.SegmentLayoutType = extend.SegmentLayoutTypeUnknown
		case extend.VolumeTypeMsMirror:
			curVol.SegmentLayoutType = extend.SegmentLayoutTypeMirror
		default:
			curVol.SegmentLayoutType = extend.SegmentLayoutTypeLine
		}

		for _, d := range des {
			curVol.Segments = append(curVol.Segments, extend.Segment{
				Device: extend.WindowsDiskPathFromID(d.DiskNumber),
				Start:  d.StartingOffset,
				Size:   d.ExtentLength,
			})
		}

		curVol.Size, err = extend.FileSize(mountpoint)
		if err != nil {
			return nil, err
		}

		label, fs_, vuuid, err := extend.VolumeExtraInfo(mountpoint)
		if err == nil {
			curVol.Filesystem = strings.ToLower(fs_)
			curVol.GUID = vuuid
			if label != "" {
				curVol.Name = fmt.Sprintf("%s (%s)", label, drvMountpoint)
			}
		}

		curVol.Usage.TotalBytes, curVol.Usage.UsedBytes, curVol.Usage.AvailBytes, err = extend.VolumeUsageInfo(mountpoint)
		if err != nil {
			return nil, err
		}

		curVol.EnabledBitlocker, err = extend.VolumeEnabledBitlocker(
			extend.WindowsDiskPathFromID(des[0].DiskNumber),
			int64(des[0].StartingOffset))
		if err != nil {
			return nil, err
		}

		isDiskBootable := false
		for _, d := range curVol.Segments {
			if table.IsDiskBootable(d.Device) {
				isDiskBootable = true
				break
			}
		}
		curVol.IsBootable = isDiskBootable && ContainsOSFileOrBootFile(curVol.MountPoint)

		vols = append(vols, curVol)
	}

	sort.Slice(vols, func(i, j int) bool {
		return vols[i].MountPoint < vols[j].MountPoint
	})

	return vols, nil
}
