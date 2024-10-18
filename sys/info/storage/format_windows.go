package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/dustin/go-humanize"
	"github.com/kisun-bit/drpkg/disk/table"
	"github.com/kisun-bit/drpkg/sys/ioctl"
	"github.com/kisun-bit/drpkg/util/basic"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
	"github.com/tidwall/sjson"
	"os"
	"strings"
)

// StoragesJson Windows平台的存储信息.
func StoragesJson() (json_ string, err error) {
	json_ = "[]"
	ps, err := EnumCompatibleHardDisks()
	if err != nil {
		return "", err
	}
	for _, d := range ps {
		dt, e := table.GetDiskType(d)
		if e != nil {
			continue
		}
		var jt any
		switch dt {
		case table.DTypeGPT:
			jt, err = NewWindowsHardDiskGPT(d)
		case table.DTypeMBR:
			jt, err = NewWindowsHardDiskMBR(d)
		case table.DTypeRAW:
			jt, err = NewWindowsHardDiskNoPartitionTable(d)
		}
		if err != nil {
			return "", err
		}
		json_, err = sjson.Set(json_, "-1", jt)
		if err != nil {
			return "", err
		}
	}
	var out bytes.Buffer
	err = json.Indent(&out, []byte(json_), "", "\t")
	if err != nil {
		return "", err
	}
	return out.String(), nil
}

func NewWindowsHardDiskMBR(hardDisk string) (hardDiskMBR WindowsHardDiskMBR, err error) {
	dt, err := table.GetDiskType(hardDisk)
	if err != nil {
		return WindowsHardDiskMBR{}, err
	}
	if dt != table.DTypeMBR {
		return WindowsHardDiskMBR{}, errors.Errorf("%s is not an disk formated by MBR", hardDisk)
	}
	ptMBR, err := table.NewMBR(hardDisk, 0, false)
	if err != nil {
		return WindowsHardDiskMBR{}, err
	}
	jsonRAW, err := ptMBR.JSONFormat()
	if err != nil {
		return WindowsHardDiskMBR{}, err
	}
	err = json.Unmarshal([]byte(jsonRAW), &hardDiskMBR)
	if err != nil {
		return WindowsHardDiskMBR{}, err
	}
	hardDiskMBR.WindowsSharedDiskAttrs, err = collectWindowsSharedDiskAttrs(hardDisk)
	if err != nil {
		return WindowsHardDiskMBR{}, err
	}
	lds, err := ioctl.QueryLogicalDrivesUnderHardDisk(hardDisk)
	if err != nil {
		return WindowsHardDiskMBR{}, err
	}
	existedCDrive := false
	for i := 0; i < len(hardDiskMBR.Parts); i++ {
		hardDiskMBR.Parts[i].WindowsSharedPartAttrs, _ = collectWindowsSharedPartAttrs(
			hardDisk,
			hardDiskMBR.Parts[i].StartSector*table.MBRDefaultLBASize,
			hardDiskMBR.Parts[i].Size,
			lds)
		if installedWindows(hardDiskMBR.Parts[i].WindowsSharedPartAttrs) {
			existedCDrive = true
		}
	}
	for i := 0; i < len(hardDiskMBR.Parts); i++ {
		if installedWindows(hardDiskMBR.Parts[i].WindowsSharedPartAttrs) {
			hardDiskMBR.Parts[i].EffectiveForBoot = true
		}
		// 此磁盘存在C盘且当前分区含有启动标记，那么他就是对本系统引导有效的分区.
		if existedCDrive && hardDiskMBR.Parts[i].Boot {
			hardDiskMBR.Parts[i].EffectiveForBoot = true
		}
		if hardDiskMBR.Parts[i].EffectiveForBoot && !hardDiskMBR.EffectiveForBoot {
			hardDiskMBR.EffectiveForBoot = true
		}
		hardDiskMBR.Parts[i].DiskPath = hardDisk
		hardDiskMBR.Parts[i].BriefDesc = getPartBrief(
			hardDiskMBR.Parts[i].WindowsSharedPartAttrs,
			hardDiskMBR.Parts[i].Size,
			hardDiskMBR.Parts[i].TypeDesc)
	}
	hardDiskMBR.DiskPath = hardDisk
	hardDiskMBR.BriefDesc = getDiskBrief(hardDiskMBR.WindowsSharedDiskAttrs, hardDiskMBR.DiskLabelType, hardDiskMBR.Size)
	return hardDiskMBR, nil
}

func NewWindowsHardDiskGPT(hardDisk string) (hardDiskGPT WindowsHardDiskGPT, err error) {
	dt, err := table.GetDiskType(hardDisk)
	if err != nil {
		return WindowsHardDiskGPT{}, err
	}
	if dt != table.DTypeGPT {
		return WindowsHardDiskGPT{}, errors.Errorf("%s is not an disk formated by GPT", hardDisk)
	}
	ptGPT, err := table.NewGPT(hardDisk, 0)
	if err != nil {
		return WindowsHardDiskGPT{}, err
	}
	jsonRAW, err := ptGPT.JSONFormat()
	if err != nil {
		return WindowsHardDiskGPT{}, err
	}
	err = json.Unmarshal([]byte(jsonRAW), &hardDiskGPT)
	if err != nil {
		return WindowsHardDiskGPT{}, err
	}
	hardDiskGPT.WindowsSharedDiskAttrs, err = collectWindowsSharedDiskAttrs(hardDisk)
	if err != nil {
		return WindowsHardDiskGPT{}, err
	}
	lds, err := ioctl.QueryLogicalDrivesUnderHardDisk(hardDisk)
	if err != nil {
		return WindowsHardDiskGPT{}, err
	}
	existedCDrive := false
	for i := 0; i < len(hardDiskGPT.Parts); i++ {
		hardDiskGPT.Parts[i].WindowsSharedPartAttrs, _ = collectWindowsSharedPartAttrs(
			hardDisk,
			hardDiskGPT.Parts[i].StartSector*table.MBRDefaultLBASize,
			hardDiskGPT.Parts[i].Size,
			lds)
		if installedWindows(hardDiskGPT.Parts[i].WindowsSharedPartAttrs) {
			existedCDrive = true
		}
	}
	for i := 0; i < len(hardDiskGPT.Parts); i++ {
		if installedWindows(hardDiskGPT.Parts[i].WindowsSharedPartAttrs) {
			hardDiskGPT.Parts[i].EffectiveForBoot = true
		}
		// 此磁盘存在C盘且当前分区含有启动标记，那么他就是对本系统引导有效的分区.
		if existedCDrive && hardDiskGPT.Parts[i].Boot {
			hardDiskGPT.Parts[i].EffectiveForBoot = true
		}
		if hardDiskGPT.Parts[i].EffectiveForBoot && !hardDiskGPT.EffectiveForBoot {
			hardDiskGPT.EffectiveForBoot = true
		}
		hardDiskGPT.Parts[i].DiskPath = hardDisk
		hardDiskGPT.Parts[i].BriefDesc = getPartBrief(
			hardDiskGPT.Parts[i].WindowsSharedPartAttrs,
			hardDiskGPT.Parts[i].Size,
			hardDiskGPT.Parts[i].TypeDesc)
	}
	hardDiskGPT.DiskPath = hardDisk
	hardDiskGPT.BriefDesc = getDiskBrief(hardDiskGPT.WindowsSharedDiskAttrs, hardDiskGPT.DiskLabelType, hardDiskGPT.Size)
	return hardDiskGPT, nil
}

func NewWindowsHardDiskNoPartitionTable(hardDisk string) (hardDiskNoPT WindowsHardDiskRAW, err error) {
	hardDiskNoPT.DiskLabelType = string(table.DTypeRAW)
	hardDiskNoPT.WindowsSharedDiskAttrs, err = collectWindowsSharedDiskAttrs(hardDisk)
	if err != nil {
		return WindowsHardDiskRAW{}, err
	}
	hardDiskNoPT.DiskPath = hardDisk
	size, err := ioctl.QueryFileSize(hardDisk)
	if err != nil {
		return WindowsHardDiskRAW{}, err
	}
	hardDiskNoPT.Size = int64(size)
	hardDiskNoPT.BriefDesc = getDiskBrief(hardDiskNoPT.WindowsSharedDiskAttrs, hardDiskNoPT.DiskLabelType, hardDiskNoPT.Size)
	return hardDiskNoPT, nil
}

func collectWindowsSharedDiskAttrs(hardDisk string) (sda WindowsSharedDiskAttrs, err error) {
	sda.Offline, sda.ReadOnly, err = ioctl.QueryHardDiskAttr(hardDisk)
	if err != nil {
		return WindowsSharedDiskAttrs{}, nil
	}
	properties, err := ioctl.QueryHardDiskProperty(hardDisk)
	if err != nil {
		return WindowsSharedDiskAttrs{}, err
	}
	sda.BusType = ioctl.StorageBusTypeToString(properties.BusType)
	sda.Vendor = properties.VendorId
	sda.Product = properties.ProductId
	sda.SerialNumber = properties.SerialNumber
	sda.Revision = properties.ProductRevision

	type_, err := table.GetDiskType(hardDisk)
	if err != nil {
		return WindowsSharedDiskAttrs{}, err
	}

	notExistedValidParts := false
	switch type_ {
	case table.DTypeMBR:
		mbrPT, err := table.NewMBR(hardDisk, 0, false)
		if err != nil {
			return WindowsSharedDiskAttrs{}, err
		}
		sda.Dynamic = mbrPT.IsDynamic()
		notExistedValidParts = mbrPT.NotExistedValidParts()
		for _, mp := range mbrPT.FullMainPartitionEntries { // 无需考虑逻辑分区.
			if mp.IsBootable() {
				sda.Boot = true
			}
		}
	case table.DTypeGPT:
		gptPT, err := table.NewGPT(hardDisk, 0)
		if err != nil {
			return WindowsSharedDiskAttrs{}, err
		}
		sda.Dynamic = gptPT.IsDynamic()
		notExistedValidParts = gptPT.NotExistedValidParts()
		for _, gp := range gptPT.PartitionEntries {
			if gp.IsBootable() {
				sda.Boot = true
			}
		}
	}

	if notExistedValidParts || sda.Offline || type_ == table.DTypeRAW || sda.Dynamic {
		sda.Ineffective = true
	}
	return sda, nil
}

func collectWindowsSharedPartAttrs(hardDisk string, partStartBytes, partSize int64, logicDrives []ioctl.LogicalDrive) (spa WindowsSharedPartAttrs, err error) {
	//spa.volumePath, _ = ioctl.QueryVolumeMountPathByAddress(hardDisk, partStartBytes, partSize, false)
	_ = hardDisk
	_ = partSize

	for _, ld := range logicDrives {
		pi, err := ioctl.QueryPartitionInformation(ld.GUIDMountPath)
		if err != nil {
			// logging.GLog.Warnf("collectWindowsSharedPartAttrs failed to query parition info for `%s`, %v", ld.GUIDMountPath, err)
			return WindowsSharedPartAttrs{}, err
		}
		//logging.GLog.Debugf("%v %v %v,%v   %v,%v", hardDisk, ld.GUIDMountPath, partStartBytes, pi.StartingOffset, partSize, pi.PartitionLength)
		if partStartBytes == pi.StartingOffset { // TODO 不用比较partSize和pi.PartitionLength，因为实际测试环境中，偶出pi.PartitionLength比partSize小的情况.
			spa.VolumeLabel = ld.Label
			spa.VolumeDriveLetter = ld.DriveName
			if spa.VolumeDriveLetter != "" {
				spa.IsBitlockerVolume, err = ioctl.IsEncryptedByBitlocker(hardDisk, partStartBytes)
				if err != nil {
					//logging.GLog.Warnf("collectWindowsSharedPartAttrs failed to query bitlocker info for `%s`[%v:\\], %v",
					//	ld.GUIDMountPath, spa.VolumeDriveLetter, err)
					err = nil
				}
			}
			spa.PartActualID = int(pi.PartitionNumber)
			spa.VolumeFilesystem = ld.FileSystem
			spa.VolumePath = ld.GUIDMountPath
			ava, total, free, e := ioctl.QueryVolumeUsageInfo(ld.GUIDMountPath)
			if e != nil {
				_ = e
				//logging.GLog.Warnf("collectWindowsSharedPartAttrs failed to query volume usage for `%s`, %v", ld.GUIDMountPath, e)
			}
			spa.VolumeAvailableBytes = int64(ava)
			spa.VolumeTotalBytes = int64(total)
			spa.VolumeUsedBytes = int64(total) - int64(free)
		}
	}
	return spa, nil
}

func installedSys(directory string) bool {
	x := "windows"
	y := "program files"
	z := "programdata"
	des, _ := os.ReadDir(directory)
	dess := make([]string, 0)
	for _, d := range des {
		dess = append(dess, strings.ToLower(d.Name()))
	}
	return funk.InStrings(dess, x) && funk.InStrings(dess, y) && funk.InStrings(dess, z)
}

func installedWindows(spa WindowsSharedPartAttrs) bool {
	// TODO 有可能系统盘不是C:\\. 后续通过查询系统环境获取得到.
	return strings.ToLower(spa.VolumeDriveLetter) == "c" && installedSys(spa.VolumeDriveLetter+":\\") && spa.VolumeFilesystem == "NTFS"
}

func getDiskBrief(diskSharedAttrs WindowsSharedDiskAttrs, diskLabelType string, size int64) string {
	dn, _ := ioctl.ParseDiskNumberFromHardDiskPath(diskSharedAttrs.DiskPath)
	production := strings.Join([]string{diskSharedAttrs.Vendor, diskSharedAttrs.Product, diskSharedAttrs.Revision}, "")
	production = basic.TrimAllSpace(production)
	if production == "" {
		production = "HardDisk"
	}
	totalHuman := basic.TrimAllSpace(humanize.IBytes(uint64(size)))
	return fmt.Sprintf("HD%v[%s]:%s-%s(%s)", dn, diskLabelType, strings.ToUpper(diskSharedAttrs.BusType), production, totalHuman)
}

func getPartBrief(partSharedAttrs WindowsSharedPartAttrs, partSize int64, typeDesc string) string {
	// 格式: [描述]+[卷标]+[文件系统]+[空间信息]
	// 如：测试名称(F:\\)[NTFS]:(Used/Total:8.7GiB/40GiB)
	volDescItems := make([]string, 0)

	if partSharedAttrs.VolumeLabel != "" {
		volDescItems = append(volDescItems, partSharedAttrs.VolumeLabel)
	} else if typeDesc != "" {
		volDescItems = append(volDescItems, basic.TrimAllSpace(typeDesc))
	} else {
		volDescItems = append(volDescItems, "Unknown")
	}

	if partSharedAttrs.VolumeDriveLetter != "" {
		volDescItems = append(volDescItems,
			fmt.Sprintf("(%s:\\)", partSharedAttrs.VolumeDriveLetter))
	}

	if partSharedAttrs.VolumeFilesystem != "" {
		volDescItems = append(volDescItems,
			fmt.Sprintf("[%s]", partSharedAttrs.VolumeFilesystem))
	} else {
		volDescItems = append(volDescItems, "[RAW]")
	}

	if partSharedAttrs.IsBitlockerVolume {
		volDescItems = append(volDescItems, "(*Bitlocker)")
	}

	volDescItems = append(volDescItems, ":")

	if partSharedAttrs.VolumeUsedBytes != 0 && partSharedAttrs.VolumeTotalBytes != 0 {
		volUsage := fmt.Sprintf("(Used/Total:%s/%s)",
			basic.TrimAllSpace(humanize.IBytes(uint64(partSharedAttrs.VolumeUsedBytes))),
			basic.TrimAllSpace(humanize.IBytes(uint64(partSharedAttrs.VolumeTotalBytes))))
		volDescItems = append(volDescItems, volUsage)
	} else {
		volUsage := fmt.Sprintf("(Used/Total:--/%s)",
			basic.TrimAllSpace(humanize.IBytes(uint64(partSize))),
		)
		volDescItems = append(volDescItems, volUsage)
	}
	return strings.Join(volDescItems, "")
}
