package storage

import (
	"github.com/kisun-bit/drpkg/disk/filesystem/fossick"
	"github.com/kisun-bit/drpkg/disk/lvm"
	"github.com/kisun-bit/drpkg/disk/table"
	"github.com/kisun-bit/drpkg/sys/ioctl"
	"github.com/kisun-bit/drpkg/util/logger"
	"path/filepath"
)

func EnumCompatibleHardDisks() (hardDisks []string, err error) {
	rs, err := ioctl.GetStorage()
	if err != nil {
		return nil, err
	}

	// Swap信息
	swaps, _ := SwapInfo()

	for _, s := range rs.Disks {
		//logger.Debugf("#### %v %v %v %v", s.Path, s.Removable, s.Type, s.BlockSize)
		if s.Removable {
			continue
		}
		if s.Type == "cdrom" {
			continue
		}
		// TODO 多路径设备的BlockSize为0. 后续需判定到底是排除还是不排除.
		if s.BlockSize != 512 {
			continue
		}
		// 磁盘存在分区表时，无有效分区表项就过滤掉.
		notExistsValidParts := false
		dtType, _ := table.GetDiskType(s.Path)
		switch dtType {
		case table.DTypeGPT:
			gptDisk, e := table.NewGPT(s.Path, 0)
			if e != nil {
				logger.Warnf("EnumCompatibleHardDisks failed to parse gpt parition table from %s", s.Path)
				continue
			}
			notExistsValidParts = gptDisk.NotExistedValidParts()
		case table.DTypeMBR:
			mbrDisk, e := table.NewMBR(s.Path, 0, false)
			if e != nil {
				logger.Warnf("EnumCompatibleHardDisks failed to parse mbr parition table from %s", s.Path)
				continue
			}
			notExistsValidParts = mbrDisk.NotExistedValidParts()
		default:
			fsType, _ := getFilesystem(s.Path)
			if fsType == "" {
				isPv, e := isPV(s.Path)
				if e != nil {
					return nil, e
				}
				isSwap_ := isSwap(swaps, s.Path)
				if !isPv && !isSwap_ {
					//continue
				}
			}
		}
		//if notExistsValidParts {
		//	continue
		//}
		_ = notExistsValidParts
		hardDisks = append(hardDisks, s.Path)
	}
	return hardDisks, err
}

// isPV 若设备是PV则返回True.
// 注意！！！！对于多路径设备，可能其执行LVM命令会报错，这是正常现象，需注意.
func isPV(device string) (bool, error) {
	if !lvm.SupportLVM {
		return false, nil
	}
	pv, err := lvm.FindPv(device)
	if err != nil {
		return false, nil
	}
	return pv.Path != "", nil
}

func isSwap(swaps []Swap, device string) bool {
	relPath, err := filepath.EvalSymlinks(device)
	if err != nil {
		relPath = device
	}
	for _, sp := range swaps {
		if sp.Filename == relPath {
			return true
		}
	}
	return false
}

func getFilesystem(device string) (string, error) {
	fsType, err := fossick.GetFilesystemType(device)
	return fsType.String(), err
}
