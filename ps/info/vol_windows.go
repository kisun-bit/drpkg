package info

import (
	"errors"
	"fmt"
	"github.com/kisun-bit/drpkg/extend"
	"strings"
	"syscall"
)

func QueryVolumes() ([]Volume, error) {
	mountpoints, err := extend.VolumeMountpoints()
	if err != nil {
		return nil, err
	}

	vols := make([]Volume, 0)

	for _, mountpoint := range mountpoints {
		if IsMemoryOS() && strings.ToLower(mountpoint) == "x:" {
			continue
		}

		des, err := extend.VolumeMountpointToExtents(mountpoint)
		if err != nil {
			var ec syscall.Errno
			if errors.As(err, &ec) && ec == 1 {
				continue
			}
			continue
		}
		if len(des) == 0 {
			continue
		}

		curVol := Volume{}
		curVol.Name = fmt.Sprintf("Volume (%s)", mountpoint)
		curVol.MountPoint = mountpoint
		for _, d := range des {
			curVol.Segments = append(curVol.Segments, Segment{
				Disk:  extend.WindowsDiskPathFromID(d.DiskNumber),
				Start: d.StartingOffset,
				Size:  d.ExtentLength,
			})
		}

		label, fs_, vuuid, err := extend.VolumeExtraInfo(mountpoint)
		if err == nil {
			curVol.Filesystem = fs_
			curVol.UUID = vuuid
			if label != "" {
				curVol.Name = fmt.Sprintf("%s (%s)", label, mountpoint)
			}
		}

		curVol.TotalBytes, curVol.UsedBytes, curVol.AvailBytes, err = extend.VolumeUsageInfo(mountpoint)
		if err != nil {
			return nil, err
		}

		curVol.EnabledBitlocker, err = extend.VolumeEnabledBitlocker(
			extend.WindowsDiskPathFromID(des[0].DiskNumber),
			int64(des[0].StartingOffset))
		if err != nil {
			return nil, err
		}

		// TODO 更多信息

		vols = append(vols, curVol)
	}

	return vols, nil
}
