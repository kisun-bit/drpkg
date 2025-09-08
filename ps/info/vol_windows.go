package info

import (
	"errors"
	"fmt"
	"strings"
	"syscall"

	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/ps/table"
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

		curVol.Size, err = extend.FileSize(curVol.MountPoint + "\\")
		if err != nil {
			return nil, err
		}

		label, fs_, vuuid, err := extend.VolumeExtraInfo(mountpoint)
		if err == nil {
			curVol.Filesystem = strings.ToLower(fs_)
			curVol.UUID = vuuid
			if label != "" {
				curVol.Name = fmt.Sprintf("%s (%s)", label, mountpoint)
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
			if table.IsDiskBootable(d.Disk) {
				isDiskBootable = true
				break
			}
		}
		curVol.IsBootable = isDiskBootable && ContainsOSFile(curVol.MountPoint)

		vols = append(vols, curVol)
	}

	return vols, nil
}
