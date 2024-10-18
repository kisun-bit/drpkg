package storage

import (
	"fmt"
	"github.com/kisun-bit/drpkg/disk/table"
	"github.com/kisun-bit/drpkg/sys/ioctl"
	"github.com/pkg/errors"
	"golang.org/x/sys/windows"
)

// EnumAllHardDisks 枚举所有的硬盘路径.
func EnumAllHardDisks() (hardDisks []string, err error) {
	for i := 0; i < ioctl.WindowsDefaultMaxHardDiskNumber; i++ {
		dp := fmt.Sprintf(`\\.\PHYSICALDRIVE%v`, i)
		len_, e := ioctl.QueryFileSize(dp)
		if e != nil {
			if e == windows.ERROR_ACCESS_DENIED {
				return nil, errors.Errorf("failed to get size of %s: %v", dp, e)
			}
			if !(e == windows.ERROR_FILE_NOT_FOUND || e == windows.ERROR_PATH_NOT_FOUND) {
				continue
			}
			continue
		}
		if len_ == 0 {
			continue
		}
		hardDisks = append(hardDisks, dp)
	}
	return hardDisks, nil
}

// EnumCompatibleHardDisks 获取所有符合迁移兼容性的硬盘路径.
// 关于迁移的兼容性, 请参考工程目录README.md.
func EnumCompatibleHardDisks() (hardDisks []string, err error) {
	allHardDisks, e := EnumAllHardDisks()
	if e != nil {
		return nil, e
	}
	for _, dp := range allHardDisks {
		ok, e := IsHardDiskCompatible(dp, false)
		if e != nil {
			return nil, e
		}
		if ok {
			hardDisks = append(hardDisks, dp)
		}
	}
	return hardDisks, nil
}

func isBusClassCompatible(busClass ioctl.WIN_STORAGE_BUS_TYPE) bool {
	for _, cb := range ioctl.WindowsValidStorageBus {
		if busClass == cb {
			return true
		}
	}
	return false
}

// IsHardDiskCompatible 若硬盘满足`sysmigrator`的兼容性, 则返回true.
func IsHardDiskCompatible(hardDisk string, ignoreIneffectiveHardDisk bool) (bool, error) {
	// 过滤掉总线类型非[SCSI, ATA, SATA, NVME, RAID, SAS]的硬盘.
	property, e := ioctl.QueryHardDiskProperty(hardDisk)
	if e != nil {
		return false, errors.Errorf("failed to query property of %s, %v", hardDisk, e)
	}
	if property.RemovableMedia || !isBusClassCompatible(property.BusType) {
		return false, nil
	}

	if !ignoreIneffectiveHardDisk {
		return true, nil
	}

	// 过滤掉脱机硬盘.
	offline, _, e := ioctl.QueryHardDiskAttr(hardDisk)
	if e != nil {
		return false, errors.Errorf("failed to get attributes of %s: %v", hardDisk, e)
	}
	if offline {
		return false, nil
	}

	// 过滤掉非MBR和非GPT的硬盘
	dt, e := table.GetDiskType(hardDisk)
	if e != nil {
		return false, nil
	}

	// 过滤掉动态硬盘以及无有效分区的硬盘.
	isDynamicDisk := false
	lackEffectiveParts := false
	switch dt {
	case table.DTypeGPT:
		gptDisk, e := table.NewGPT(hardDisk, 0)
		if e != nil {
			return false, nil
		}
		isDynamicDisk = gptDisk.IsDynamic()
		lackEffectiveParts = gptDisk.NotExistedValidParts()
	case table.DTypeMBR:
		mbrDisk, e := table.NewMBR(hardDisk, 0, false)
		if e != nil {
			return false, nil
		}
		isDynamicDisk = mbrDisk.IsDynamic()
		lackEffectiveParts = mbrDisk.NotExistedValidParts()
	}
	if isDynamicDisk {
		return false, nil
	}
	if lackEffectiveParts {
		return false, nil
	}
	return true, nil
}
