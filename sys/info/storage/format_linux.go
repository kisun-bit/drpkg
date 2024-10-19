package storage

import (
	"encoding/json"
	"fmt"
	"github.com/dustin/go-humanize"
	"github.com/kisun-bit/drpkg/disk/filesystem/fossick"
	"github.com/kisun-bit/drpkg/disk/lvm"
	"github.com/kisun-bit/drpkg/disk/table"
	"github.com/kisun-bit/drpkg/sys/ioctl"
	"github.com/kisun-bit/drpkg/util"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
	"github.com/tidwall/sjson"
	"path/filepath"
	"strings"
)

func Storages(lvmInfo LinuxLVM) (json_ string, err error) {
	json_ = "[]"
	swaps, _ := SwapInfo()
	hds, err := EnumCompatibleHardDisks()
	if err != nil {
		return "", err
	}
	rs, err := ioctl.GetStorage()
	if err != nil {
		return "", err
	}
	rsGrepped := new(ioctl.ResourcesStorage)
	for _, disk_ := range rs.Disks {
		if funk.InStrings(hds, disk_.Path) {
			rsGrepped.Disks = append(rsGrepped.Disks, disk_)
		}
	}
	ioctl.FixResourcesStorage(rsGrepped)
	for _, hardDisk := range hds {
		disk_, ok := queryResourceStorageDiskByPath(hardDisk, rsGrepped.Disks)
		if !ok {
			continue
		}
		dtType, _ := table.GetDiskType(hardDisk)
		var jt any
		switch dtType {
		case table.DTypeMBR:
			jt, err = NewLinuxHardDiskMBR(disk_, lvmInfo, swaps)
		case table.DTypeGPT:
			jt, err = NewLinuxHardDiskGPT(disk_, lvmInfo, swaps)
		case table.DTypeRAW:
			jt, err = NewLinuxHardDiskNoPartitionTable(disk_, lvmInfo, swaps)
		}
		if err != nil {
			return "", err
		}
		json_, err = sjson.Set(json_, "-1", jt)
		if err != nil {
			return "", err
		}
	}
	return json_, nil
}

func queryResourceStorageDiskByPath(hardDisk string, ss []ioctl.ResourcesStorageDisk) (ioctl.ResourcesStorageDisk, bool) {
	for _, s := range ss {
		if filepath.Join("/dev", s.Name) != hardDisk {
			continue
		}
		return s, true
	}
	return ioctl.ResourcesStorageDisk{}, false
}

func NewLinuxHardDiskMBR(disk_ ioctl.ResourcesStorageDisk, lvmInfo LinuxLVM, swaps []Swap) (hardDiskMBR LinuxHardDiskMBR, err error) {
	hardDisk := filepath.Join("/dev", disk_.Name)
	dt, err := table.GetDiskType(hardDisk)
	if err != nil {
		return LinuxHardDiskMBR{}, err
	}
	if dt != table.DTypeMBR {
		return LinuxHardDiskMBR{}, errors.Errorf("%s is not an disk formated by MBR", hardDisk)
	}
	ptMBR, err := table.NewMBR(hardDisk, 0, false)
	if err != nil {
		return LinuxHardDiskMBR{}, err
	}
	jsonRAW, err := ptMBR.JSONFormat()
	if err != nil {
		return LinuxHardDiskMBR{}, err
	}
	err = json.Unmarshal([]byte(jsonRAW), &hardDiskMBR)
	if err != nil {
		return LinuxHardDiskMBR{}, err
	}
	hardDiskMBR.LinuxSharedDiskAttrs, err = collectLinuxSharedDiskAttrs(disk_, lvmInfo, swaps)
	if err != nil {
		return LinuxHardDiskMBR{}, err
	}
	for i := 0; i < len(hardDiskMBR.Parts); i++ {
		partName := ioctl.GeneratePartDeviceName(disk_.Name, hardDiskMBR.Parts[i].Index)
		hardDiskMBR.Parts[i].LinuxSharedPartAttrs, _ = collectLinuxSharedPartAttrs(
			disk_, partName, lvmInfo, swaps)
		if hardDiskMBR.Parts[i].EffectiveForBoot && !hardDiskMBR.EffectiveForBoot {
			hardDiskMBR.EffectiveForBoot = true
		}
		if hardDiskMBR.Parts[i].Boot {
			hardDiskMBR.Boot = true
		}
	}
	for i := 0; i < len(hardDiskMBR.Parts); i++ {
		if hardDiskMBR.EffectiveForBoot && hardDiskMBR.Parts[i].Boot {
			hardDiskMBR.Parts[i].EffectiveForBoot = true
		}
		hardDiskMBR.Parts[i].BriefDesc = getPartBrief(
			lvmInfo,
			hardDiskMBR.Parts[i].LinuxSharedPartAttrs, hardDiskMBR.Parts[i].Size, hardDiskMBR.Parts[i].TypeDesc)
	}
	hardDiskMBR.DiskPath = hardDisk
	hardDiskMBR.IsDisk = true
	hardDiskMBR.BriefDesc = getDiskBrief(hardDiskMBR.LinuxSharedDiskAttrs, hardDiskMBR.DiskLabelType, 0, hardDiskMBR.Size)
	return hardDiskMBR, nil
}

func NewLinuxHardDiskGPT(disk_ ioctl.ResourcesStorageDisk, lvmInfo LinuxLVM, swaps []Swap) (hardDiskGPT LinuxHardDiskGPT, err error) {
	hardDisk := filepath.Join("/dev", disk_.Name)
	dt, err := table.GetDiskType(hardDisk)
	if err != nil {
		return LinuxHardDiskGPT{}, err
	}
	if dt != table.DTypeGPT {
		return LinuxHardDiskGPT{}, errors.Errorf("%s is not an disk formated by GPT", hardDisk)
	}
	ptMBR, err := table.NewGPT(hardDisk, 0)
	if err != nil {
		return LinuxHardDiskGPT{}, err
	}
	jsonRAW, err := ptMBR.JSONFormat()
	if err != nil {
		return LinuxHardDiskGPT{}, err
	}
	err = json.Unmarshal([]byte(jsonRAW), &hardDiskGPT)
	if err != nil {
		return LinuxHardDiskGPT{}, err
	}
	hardDiskGPT.LinuxSharedDiskAttrs, err = collectLinuxSharedDiskAttrs(disk_, lvmInfo, swaps)
	if err != nil {
		return LinuxHardDiskGPT{}, err
	}
	for i := 0; i < len(hardDiskGPT.Parts); i++ {
		partName := ioctl.GeneratePartDeviceName(disk_.Name, hardDiskGPT.Parts[i].Index)
		hardDiskGPT.Parts[i].LinuxSharedPartAttrs, _ = collectLinuxSharedPartAttrs(
			disk_, partName, lvmInfo, swaps)
		if hardDiskGPT.Parts[i].EffectiveForBoot && !hardDiskGPT.EffectiveForBoot {
			hardDiskGPT.EffectiveForBoot = true
		}
		if hardDiskGPT.Parts[i].Boot {
			hardDiskGPT.Boot = true
		}
	}
	for i := 0; i < len(hardDiskGPT.Parts); i++ {
		if hardDiskGPT.EffectiveForBoot && hardDiskGPT.Parts[i].Boot {
			hardDiskGPT.Parts[i].EffectiveForBoot = true
		}
		hardDiskGPT.Parts[i].BriefDesc = getPartBrief(
			lvmInfo,
			hardDiskGPT.Parts[i].LinuxSharedPartAttrs, hardDiskGPT.Parts[i].Size, hardDiskGPT.Parts[i].TypeDesc)
	}
	hardDiskGPT.DiskPath = hardDisk
	hardDiskGPT.IsDisk = true
	hardDiskGPT.BriefDesc = getDiskBrief(hardDiskGPT.LinuxSharedDiskAttrs, hardDiskGPT.DiskLabelType, 0, hardDiskGPT.Size)
	return hardDiskGPT, nil
}

func NewLinuxHardDiskNoPartitionTable(disk_ ioctl.ResourcesStorageDisk, lvmInfo LinuxLVM, swaps []Swap) (hardDiskNoPT LinuxHardDiskNoPartitionTable, err error) {
	hardDiskNoPT.DiskLabelType = string(table.DTypeRAW)
	hardDiskNoPT.Size = int64(disk_.Size)
	hardDiskNoPT.SectorSize = int(disk_.BlockSize)
	if hardDiskNoPT.SectorSize > 0 {
		hardDiskNoPT.Sectors = hardDiskNoPT.Size / int64(hardDiskNoPT.SectorSize)
	}
	hardDiskNoPT.LinuxSharedDiskAttrs, err = collectLinuxSharedDiskAttrs(disk_, lvmInfo, swaps)
	if err != nil {
		return LinuxHardDiskNoPartitionTable{}, err
	}
	hardDiskNoPT.DiskPath = filepath.Join("/dev", disk_.Name)

	pvUsedSize := int64(0)
	size := hardDiskNoPT.Size
	pv, e := lvm.FindPv(hardDiskNoPT.DiskPath)
	if e == nil {
		hardDiskNoPT.IsPV = true
		pvUsedSize = pv.Size - pv.Free
		size = pv.Size
	}

	if hardDiskNoPT.VolumeFilesystem == "" && !hardDiskNoPT.IsPV && !hardDiskNoPT.IsPart && hardDiskNoPT.DiskLabelType == string(table.DTypeRAW) {
		hardDiskNoPT.Ineffective = true
	}

	hardDiskNoPT.BriefDesc = getDiskBrief(hardDiskNoPT.LinuxSharedDiskAttrs, hardDiskNoPT.DiskLabelType, pvUsedSize, size)
	return hardDiskNoPT, err
}

func collectLinuxSharedDiskAttrs(disk_ ioctl.ResourcesStorageDisk, lvmInfo LinuxLVM, swaps []Swap) (sda LinuxSharedDiskAttrs, err error) {
	sda.ReadOnly = disk_.ReadOnly
	sda.BusType = disk_.Type
	sda.Model = disk_.Model
	sda.SerialNumber = disk_.Serial
	sda.LinuxSharedPartAttrs, err = collectLinuxSharedPartAttrs(disk_, disk_.Name, lvmInfo, swaps)
	return sda, err
}

func collectLinuxSharedPartAttrs(disk_ ioctl.ResourcesStorageDisk, deviceName string, lvmInfo LinuxLVM, swaps []Swap) (spa LinuxSharedPartAttrs, err error) {
	defer func() {
		if err != nil {
			return
		}
		if spa.VolumeFilesystem == "" {
			spa.VolumeFilesystem, _ = getFilesystem(filepath.Join("/dev", deviceName))
		}
		if strings.HasPrefix(spa.VolumeFilesystem, "ext") {
			spa.VolumeFilesystem = fossick.EXT.String()
		}

		if spa.VolumeFilesystem != "" {
			spa.IsPart = true
		}

		// 修正swap文件系统标记
		if len(swaps) != 0 {
			for _, swap := range swaps {
				if swap.Filename == spa.VolumePath {
					spa.VolumeFilesystem = "swap"
					spa.VolumeUsedBytes = swap.Used
					spa.VolumeTotalBytes = swap.Size
					spa.VolumeAvailableBytes = swap.Size - swap.Used
				}
			}
		}

		// 修正EffectiveForBoot标记, 符合修正的情况如下所示:
		// 1. 挂载路径符合IsEffectiveForBoot
		// 2. 挂载路径不符合IsEffectiveForBoot，但是存在Boot标记，且
		if !spa.EffectiveForBoot {
			spa.EffectiveForBoot = IsEffectiveForBoot(spa.VolumeMountPath)
		}

		if spa.VolumeMountPath != "" {
			spa.VolumeAvailableBytes, spa.VolumeTotalBytes, spa.VolumeUsedBytes, _, _, _, _ = ioctl.UsageInfo(
				spa.VolumeMountPath)
		}
	}()

	spa.VolumePath = filepath.Join("/dev", deviceName)

	if lvmInfo.SupportLVM {
		pv, ok := lvmInfo.FindPV(spa.VolumePath)
		if ok {
			spa.IsPV = true
			if pv.EffectiveForBoot {
				spa.EffectiveForBoot = true
			}
		}
	} else {
		spa.IsPV, _ = isPV(spa.VolumePath)
	}

	if disk_.Name == deviceName {
		spa.PartUUID = disk_.PartUUID
		spa.DeviceID = disk_.DeviceID
		spa.VolumeUUID = disk_.UUID
		spa.VolumeMountPath = disk_.MountPath
		spa.VolumeFilesystem = disk_.Filesystem
		return spa, err
	} else {
		for _, dp := range disk_.Partitions {
			if dp.Name == deviceName {
				spa.PartUUID = dp.PartUUID
				spa.DeviceID = dp.DeviceID
				spa.VolumeUUID = dp.UUID
				spa.VolumeMountPath = dp.MountPath
				spa.VolumeFilesystem = dp.Filesystem
			}
		}
	}
	// TODO 支持分区中含磁盘分区表的结构.
	return spa, nil
}

func getDiskBrief(diskSharedAttrs LinuxSharedDiskAttrs, diskLabelType string, pvUsed, size int64) string {
	if diskLabelType != string(table.DTypeRAW) && diskLabelType != "" {
		return _getDiskBrief(
			diskSharedAttrs.DiskPath, diskLabelType, diskSharedAttrs.BusType, diskSharedAttrs.Model, size)
	}
	if diskSharedAttrs.IsPV {
		return _getPVBrief(
			diskSharedAttrs.DiskPath, pvUsed, size)
	}
	if diskSharedAttrs.IsPart {
		return _getVolumeBrief(
			diskSharedAttrs.DiskPath, diskSharedAttrs.VolumeFilesystem, diskSharedAttrs.VolumeMountPath,
			diskSharedAttrs.VolumeUsedBytes, diskSharedAttrs.VolumeTotalBytes, size)
	}
	production := util.TrimAllSpace(diskSharedAttrs.Model)
	if production == "" {
		production = "UnknownProduct"
	}
	return fmt.Sprintf("Unknown-%s[RAW]:%s-%s(%s)",
		diskSharedAttrs.DiskPath,
		strings.ToUpper(diskSharedAttrs.BusType),
		strings.ReplaceAll(diskSharedAttrs.Model, " ", ""),
		util.TrimAllSpace(humanize.IBytes(uint64(size))))
}

func getPartBrief(lvmInfo LinuxLVM, partSharedAttrs LinuxSharedPartAttrs, partSize int64, typeDesc string) string {
	if partSharedAttrs.IsPart {
		return _getVolumeBrief(partSharedAttrs.VolumePath, partSharedAttrs.VolumeFilesystem,
			partSharedAttrs.VolumeMountPath, partSharedAttrs.VolumeUsedBytes, partSharedAttrs.VolumeTotalBytes, partSize)
	}
	if partSharedAttrs.IsPV {
		pvUsed := int64(0)
		pvSize := partSize
		pv, ok := lvmInfo.FindPV(partSharedAttrs.VolumePath)
		if ok {
			pvSize = pv.Size
			pvUsed = pv.Size - pv.Free
		}
		return _getPVBrief(partSharedAttrs.VolumePath, pvUsed, pvSize)
	}
	return fmt.Sprintf("Part-%s:(Used:--/%s)",
		util.TrimAllSpace(typeDesc), util.TrimAllSpace(humanize.IBytes(uint64(partSize))))
}

func _getDiskBrief(path, label, bus, model string, size int64) string {
	// 格式举例: Disk-/dev/sda3[GPT]:SCSI-VirtualDisk(40GB)
	production := util.TrimAllSpace(model)
	if production == "" {
		production = "UnknownProduct"
	}
	totalHuman := util.TrimAllSpace(humanize.IBytes(uint64(size)))
	return fmt.Sprintf("%s-%v[%s]:%s-%s(%s)",
		"Disk", path, label, strings.ToUpper(bus), production, totalHuman)
}

func _getVolumeBrief(path, filesystem_, mountPath string, used, total, partSize int64) string {
	// 格式举例: Volume-/dev/sda2(挂载点:无)[ext2/3/4]:(Used/Total:--/40GiB)
	if mountPath == "" {
		mountPath = "--"
	}
	usedDesc := util.TrimAllSpace(humanize.IBytes(uint64(used)))
	if used == 0 {
		usedDesc = "--"
	}
	fsDesc := fmt.Sprintf("[%s]", filesystem_)
	if fsDesc == "" {
		fsDesc = ""
	}
	totalDesc := util.TrimAllSpace(humanize.IBytes(uint64(total)))
	if total == 0 {
		totalDesc = util.TrimAllSpace(humanize.IBytes(uint64(partSize)))
	}
	return fmt.Sprintf("%s-%s(Mount:%s)%s:(Used/Total:%s/%s)", "Volume", path, mountPath, fsDesc, usedDesc, totalDesc)
}

func _getPVBrief(path string, used, total int64) string {
	// 格式举例: PV-/dev/sda3[LVM]:(Used/Total:--/40GB)
	usedDesc := util.TrimAllSpace(humanize.IBytes(uint64(used)))
	if used < 0 {
		usedDesc = "0B"
	}
	totalDesc := util.TrimAllSpace(humanize.IBytes(uint64(total)))
	return fmt.Sprintf("%s-%s[LVM]:(Used/Total:%s/%s)", "PV", path, usedDesc, totalDesc)
}

func _getVGBrief(name string, used, total int64) string {
	// 格式举例: VG-VolGroup[LVM]:(Used/Total:--/40GB)
	usedDesc := util.TrimAllSpace(humanize.IBytes(uint64(used)))
	if used < 0 {
		usedDesc = "0B"
	}
	totalDesc := util.TrimAllSpace(humanize.IBytes(uint64(total)))
	return fmt.Sprintf("%s-%s[LVM]:(Used/Total:%s/%s)", "VG", name, usedDesc, totalDesc)
}

func _getLVBrief(vgName, lvName, attr, filesystem_, mountPath, pool, origin string, isVolume bool, used, total, lvSize int64) string {
	_ = pool
	attrBytes, err := lvm.ParseLvAttrs(attr)
	if err != nil {
		return fmt.Sprintf("LV-%s/%s[ERROR ATTR]:(Used/Total:--/%s)",
			vgName, lvName, util.TrimAllSpace(humanize.IBytes(uint64(lvSize))))
	}
	if isVolume {
		return _getVolumeBrief(
			fmt.Sprintf("%s/%s", vgName, lvName),
			filesystem_, mountPath, used, total, lvSize)
	}
	lvType := attrBytes[0]
	prefix := "UnknownLVType"
	switch lvType {
	case lvm.LV_ATTR_VOL_TYPE_THIN_POOL:
		prefix = "ThinPool"
	case lvm.LV_ATTR_VOL_TYPE_THIN_VOLUME:
		prefix = "ThinLV"
		if origin != "" {
			prefix = "ThinSnapshot"
		}
	case lvm.LV_ATTR_VOL_TYPE_SNAPSHOT:
		prefix = "Snapshot"
	case 0:
		prefix = "LV"
	default:
		// do nothing
	}
	return fmt.Sprintf("%s-%s/%s[%s]:(Used/Total:--/%s)",
		prefix,
		vgName,
		lvName,
		attr,
		util.TrimAllSpace(humanize.IBytes(uint64(lvSize))),
	)
}
