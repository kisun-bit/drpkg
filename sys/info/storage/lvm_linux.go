package storage

import (
	"encoding/json"
	"fmt"
	"github.com/kisun-bit/drpkg/disk/filesystem/fossick"
	"github.com/kisun-bit/drpkg/disk/lvm"
	"github.com/kisun-bit/drpkg/sys/ioctl"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
	"path/filepath"
	"strings"
)

func (li *LinuxLVM) IsPV(device string) bool {
	_, ok := li.FindPV(device)
	return ok
}

func (li *LinuxLVM) FindPV(device string) (pv PV, ok bool) {
	if len(li.PVList) == 0 {
		return PV{}, false
	}
	for _, pv_ := range li.PVList {
		if pv_.Name == device {
			return pv_, true
		}
	}
	return PV{}, false
}

func LVMInfo() (lvmInfo LinuxLVM, err error) {
	lvmInfo.SupportLVM = lvm.SupportLVM
	if !lvmInfo.SupportLVM {
		return LinuxLVM{}, nil
	}
	lvmInfo.MajorVersion = lvm.LVMMarjorVersion
	if lvmInfo.MajorVersion < 2 {
		return LinuxLVM{}, errors.Errorf("only supports LVM with major version greater than 2")
	}

	effectBootVgs := make([]string, 0)

	// Swap信息
	swaps, _ := SwapInfo()

	// 解析操作系统的挂载点信息.
	mountIDs, err := ioctl.GetMountIDs()
	if err != nil {
		return LinuxLVM{}, err
	}

	// 逻辑卷信息收集.
	lvs, err := lvm.Lvs()
	if err != nil {
		return LinuxLVM{}, err
	}
	for _, lv := range lvs {
		lvInfo := LV{}
		lvInfo.Name = lv.Name
		lvInfo.DmName, _ = ioctl.QueryLVDmName(lv.VgName, lv.Name)
		lvInfo.UUID = lv.UUID
		lvInfo.VGName = lv.VgName
		lvInfo.Pool = lv.Pool
		lvInfo.Origin = lv.Origin
		lvInfo.Size = lv.Size
		lvInfo.Attr = lv.AttrStr
		lvInfo.VolumePath = fmt.Sprintf("/dev/mapper/%s-%s",
			strings.ReplaceAll(lv.VgName, "-", "--"), strings.ReplaceAll(lv.Name, "-", "--"))
		devNum := ioctl.QueryDeviceNumber(lvInfo.DmName)
		if devNum != "" {
			if v, ok := mountIDs[devNum]; ok {
				lvInfo.VolumeFilesystem = v.Filesystem
				if strings.HasPrefix(v.Filesystem, "ext") {
					lvInfo.VolumeFilesystem = fossick.EXT.String()
				}
				lvInfo.VolumeMountPath = v.MountPath
				if lvInfo.VolumeMountPath != "" {
					lvInfo.VolumeAvailableBytes, lvInfo.VolumeTotalBytes, lvInfo.VolumeUsedBytes, _, _, _, _ = ioctl.UsageInfo(
						lvInfo.VolumeMountPath)
				}
			}
		}
		if lvInfo.VolumeFilesystem == "" && lvInfo.DmName != "" {
			lvInfo.VolumeFilesystem, _ = getFilesystem(filepath.Join("/dev", lvInfo.DmName))
		}
		if lvInfo.VolumeFilesystem != "" && lvInfo.Origin == "" {
			lvInfo.IsVolume = true
		}
		if lvInfo.DmName != "" {
			lvInfo.VolumeUUID = ioctl.MatchDiskBy(ioctl.DevDiskByUUID, lvInfo.DmName)
			lvInfo.PartUUID = ioctl.MatchDiskBy(ioctl.DevDiskByPartUUID, lvInfo.DmName)
		}
		if len(swaps) != 0 {
			for _, swap := range swaps {
				//if swap.Filename == filepath.Join("/dev", lvInfo.DmName) {
				relDmPath, _ := filepath.EvalSymlinks(lvInfo.VolumePath)
				if swap.Filename == relDmPath {
					lvInfo.VolumeFilesystem = "swap"
					lvInfo.VolumeUsedBytes = swap.Used
					lvInfo.VolumeTotalBytes = swap.Size
					lvInfo.VolumeAvailableBytes = swap.Size - swap.Used
					lvInfo.IsVolume = true
					lvInfo.EffectiveForBoot = true
				}
			}
		}
		if IsEffectiveForBoot(lvInfo.VolumeMountPath) {
			lvInfo.EffectiveForBoot = true
			effectBootVgs = append(effectBootVgs, lvInfo.VGName)
		}
		lvInfo.BriefDesc = _getLVBrief(lv.VgName, lv.Name, lv.AttrStr, lvInfo.VolumeFilesystem, lvInfo.VolumeMountPath,
			lvInfo.Pool, lvInfo.Origin, lvInfo.IsVolume, lvInfo.VolumeUsedBytes, lvInfo.VolumeTotalBytes, lvInfo.Size)
		lvmInfo.LVList = append(lvmInfo.LVList, lvInfo)
	}

	// 卷组信息收集.
	vgs, err := lvm.Vgs()
	if err != nil {
		return LinuxLVM{}, err
	}
	for _, vg := range vgs {
		vgInfo := VG{}
		vgInfo.Name = vg.Name
		vgInfo.UUID = vg.UUID
		vgInfo.ExtentSize = vg.ExtentSize
		vgInfo.Size = vg.Size
		vgInfo.Free = vg.Free
		vgInfo.Attr = vg.AttrStr
		vgInfo.BriefDesc = _getVGBrief(vg.Name, vg.Size-vg.Free, vg.Size)
		//if len(vg.Lvs) == 0 {
		//	// 过滤掉VG下无LV的VG
		//	continue
		//}
		for _, pv := range vg.Pvs {
			vgInfo.PVNames = append(vgInfo.PVNames, pv.Path)
		}
		for _, lv := range vg.Lvs {
			vgInfo.LVNames = append(vgInfo.LVNames, lv.Name)
		}
		if funk.InStrings(effectBootVgs, vgInfo.Name) {
			vgInfo.EffectiveForBoot = true
		}
		lvmInfo.VGList = append(lvmInfo.VGList, vgInfo)
	}

	// 物理卷信息收集.
	pvs, err := lvm.Pvs()
	if err != nil {
		return LinuxLVM{}, err
	}
	for _, pv := range pvs {
		pvInfo := PV{}
		pvInfo.Name = pv.Path
		pvInfo.UUID = pv.UUID
		pvInfo.VGName = pv.VgName
		//// 过滤掉PV下无VG的PV
		//if pv.VgName == "" {
		//	continue
		//}
		pvInfo.Size = pv.Size
		pvInfo.Free = pv.Free
		pvInfo.Attr = pv.AttrStr
		pvInfo.BriefDesc = _getPVBrief(pv.Path, pv.Size-pv.Free, pv.Size)
		if funk.InStrings(effectBootVgs, pvInfo.VGName) {
			pvInfo.EffectiveForBoot = true
		}
		lvmInfo.PVList = append(lvmInfo.PVList, pvInfo)
	}

	return lvmInfo, nil
}

func LVMJson() (string, error) {
	lvmInfo, err := LVMInfo()
	if err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(lvmInfo, "", "\t")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func IsEffectiveForBoot(mountPath string) bool {
	return funk.InStrings(ioctl.EffectiveMountPathsForBoot, mountPath)
}
