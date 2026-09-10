package meta

import (
	"runtime"
	"strings"

	"github.com/kisun-bit/drpkg/disk/table"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/platform/info"
	"github.com/kisun-bit/drpkg/xutil"
)

type MetadataVolume struct {
	Brief string `json:"brief"`
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	Used  int64  `json:"used"`
	Free  int64  `json:"free"`
}

type MetadataVolumes struct {
	Volumes []MetadataVolume `json:"volumes"`
}

func ListVolumesForMetadata(psinfo *info.PsInfo, maxProtectedDiskCount, maxProtectedSizeTBPerDisk int) (vs MetadataVolumes, err error) {
	if runtime.GOOS != "windows" {
		return vs, nil
	}

	//
	// 条件：
	// * 非动态磁盘上的简单卷
	//

	maxDiskCount := uint32(maxProtectedDiskCount)
	maxDiskSize := uint64(maxProtectedSizeTBPerDisk * (1 << 40))

	h, err := GenerateHeader(maxDiskCount, maxDiskSize)
	if err != nil {
		return vs, err
	}

	metadataSize := int64(h.MaxDiskCount)*int64(h.BitsPerDiskBitmap/8) + DefaultFirstDiskBitmapStart
	logger.Debugf("ListVolumesForMetadata: metadataSize=%v", metadataSize)

	for _, v := range psinfo.Public.Volumes {
		if v.Layout != xutil.VolumeTypeSimple ||
			len(v.Segments) != 1 ||
			!strings.HasSuffix(v.MountPoint, ":") ||
			v.Usage.AvailBytes <= uint64(metadataSize) {
			continue
		}

		//
		// 过滤掉动态磁盘、只读磁盘、逻辑卷
		//

		basedDynamicDisk := false
		basedReadonlyDisk := false
		isLogicDisk := false
		isIscsiDisk := false
		for _, disk := range psinfo.Public.Disks {
			if disk.Device != v.Segments[0].Device {
				continue
			}
			if disk.IsMsDynamic {
				basedDynamicDisk = true
				break
			}
			if disk.IsReadOnly {
				basedReadonlyDisk = true
				break
			}
			if strings.Contains(disk.PathId, "-iscsi-") || strings.EqualFold(disk.Bus, "iscsi") {
				isIscsiDisk = true
				break
			}
			if disk.Table.Type == table.TableTypeMBR {
				for _, p := range disk.Table.Partitions {
					if isLogicDisk {
						break
					}
					for _, et := range table.MBRExtendPartTypes {
						if p.Type == et {
							if v.Segments[0].Start >= uint64(p.Start) && v.Segments[0].Start <= uint64(p.Start)+uint64(p.Size) {
								isLogicDisk = true
								break
							}
						}
					}
				}
			}
		}

		if basedDynamicDisk || basedReadonlyDisk || isLogicDisk || isIscsiDisk {
			continue
		}

		vs.Volumes = append(vs.Volumes, MetadataVolume{
			Brief: v.Name,
			Name:  strings.TrimSuffix(v.MountPoint, ":"),
			Size:  int64(v.Usage.TotalBytes),
			Used:  int64(v.Usage.UsedBytes),
			Free:  int64(v.Usage.AvailBytes),
		})
	}

	return vs, nil
}
