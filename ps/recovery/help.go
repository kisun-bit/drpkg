package recovery

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/kisun-bit/drpkg/command"
	"github.com/kisun-bit/drpkg/disk/table"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/ps/info"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
)

// IsRootDevice 是否为 Root 设备
func IsRootDevice(ctx context.Context, device string) bool {
	return withMount(ctx, device, "IsRootDevice", extend.IsRootDir)
}

// IsEfiDevice 是否为 EFI 设备
func IsEfiDevice(ctx context.Context, device string) bool {
	return withMount(ctx, device, "IsEfiDevice", extend.IsEfiDir)
}

// IsVarDevice 是否为 Var 设备
func IsVarDevice(ctx context.Context, device string) bool {
	return withMount(ctx, device, "IsVarDevice", extend.IsVarDir)
}

// IsUsrDevice 是否为 Usr 设备
func IsUsrDevice(ctx context.Context, device string) bool {
	return withMount(ctx, device, "IsUsrDevice", extend.IsUsrDir)
}

// IsBootDevice 是否为 Boot 设备
func IsBootDevice(ctx context.Context, device string) bool {
	return withMount(ctx, device, "IsBootDevice", extend.IsBootDir)
}

// SupportMount 指定设备是否支持挂载
func SupportMount(device string) (ok bool, fsType string) {
	fsType, err := DetectFSTypeByBlkid(device)
	if err != nil {
		return false, ""
	}
	if fsType == "" {
		return false, ""
	}
	return funk.InStrings(SupportedFsTypes, fsType), fsType
}

// DetectFSTypeByBlkid 使用 blkid 探测文件系统类型
func DetectFSTypeByBlkid(device string) (string, error) {
	switch runtime.GOOS {
	case "linux":
		_, output, err := command.Execute(fmt.Sprintf("blkid -o value -s TYPE %s", device))
		if err != nil {
			return "", err
		}
		match := reBlkidType.FindStringSubmatch(output)
		if len(match) > 1 {
			return match[1], nil
		}
		return strings.TrimSpace(output), nil
	default:
		return "", errors.Errorf("unsupported platform %s", runtime.GOOS)
	}
}

// DetectUuidByBlkid 使用 blkid 探测设备的UUID
func DetectUuidByBlkid(device string) (string, error) {
	switch runtime.GOOS {
	case "linux":
		_, output, err := command.Execute(fmt.Sprintf("blkid -o value -s UUID %s", device))
		if err != nil {
			return "", err
		}
		match := reBlkidUuid.FindStringSubmatch(output)
		if len(match) > 1 {
			return match[1], nil
		}
		return strings.TrimSpace(output), nil
	default:
		return "", errors.Errorf("unsupported platform %s", runtime.GOOS)
	}
}

// DetectFSRepairCmdline 探测设备的修复命令
func DetectFSRepairCmdline(device string) (cmdline string, ok bool) {
	switch runtime.GOOS {
	case "windows":
		if !strings.HasSuffix(device, ":") {
			logger.Warnf("DetectFSRepairCmdline: The device name (%s) is invalid; Windows platforms require it to end with a \":\" character.", device)
			return "", false
		}
		return fmt.Sprintf("CHKDSK /F %s", device), true
	case "linux":
		fsType, err := DetectFSTypeByBlkid(device)
		if err != nil {
			logger.Warnf("DetectFSRepairCmdline: DetectFSTypeByBlkid failed for %s. %v", device, err)
			return "", false
		}
		logger.Debugf("DetectFSRepairCmdline: device: %s, fsType: %s", device, fsType)

		switch fsType {
		case "ext4", "ext3", "ext2":
			return fmt.Sprintf("e2fsck -y %s", device), true
		case "xfs":
			return fmt.Sprintf("xfs_repair -L %s", device), true
		case "btrfs":
			return fmt.Sprintf("btrfs check --repair %s", device), true
		case "fat":
			return fmt.Sprintf("fsck.fat -a %s", device), true
		case "vfat":
			return fmt.Sprintf("fsck.vfat -a %s", device), true
		case "ntfs":
			return fmt.Sprintf("ntfsfix -b -d %s", device), true
		case "cramfs":
			return fmt.Sprintf("fsck.cramfs -y %s", device), true
		case "gfs2":
			return fmt.Sprintf("fsck.gfs2 -y %s", device), true
		case "hfs":
			return fmt.Sprintf("fsck.hfs -y %s", device), true
		case "hfsplus":
			return fmt.Sprintf("fsck.hfsplus -y %s", device), true
		case "zfs":
			return fmt.Sprintf("fsck.zfs -y %s", device), true
		case "jfs":
			return fmt.Sprintf("fsck.jfs -a %s", device), true
		case "minix":
			return fmt.Sprintf("fsck.minix -a %s", device), true
		case "msdos":
			return fmt.Sprintf("fsck.msdos -a %s", device), true
		case "reiserfs":
			return fmt.Sprintf("yes Yes | fsck.reiserfs --fix-fixable --rebuild-sb --rebuild-tree -y %s", device), true
		default:
			return "", false
		}

	default:
		return "", false
	}
}

func withMount(
	ctx context.Context,
	device string,
	tag string,
	check func(string) bool,
) bool {

	logger.Debugf("%s(%s) ++", tag, device)
	defer logger.Debugf("%s(%s) --", tag, device)

	var mountpoint string

	switch runtime.GOOS {

	case "windows":
		if !strings.HasSuffix(device, ":") {
			return false
		}
		mountpoint = device + "\\"

	case "linux":
		var err error

		mountpoint, err = os.MkdirTemp("", strings.ToLower(tag)+"-*")
		if err != nil {
			logger.Warnf("%s mkdir temp failed: %v", tag, err)
			return false
		}
		defer os.RemoveAll(mountpoint)

		support, err := Mount(ctx, device, mountpoint, true)
		if err != nil {
			logger.Warnf("%s mount failed: dev=%s err=%v", tag, device, err)
			return false
		}
		if !support {
			return false
		}

		defer func() {
			if err := Umount(mountpoint, false); err != nil {
				logger.Warnf("%s umount failed: %v", tag, err)
			}
		}()

	default:
		return false
	}

	if mountpoint == "" {
		return false
	}

	return check(mountpoint)
}

type fsDevice struct {
	Device string
	FsType string
}

func enumFilesystem(offline []string) ([]fsDevice, error) {
	logger.Debugf("enumFilesystem: ++")
	defer logger.Debugf("enumFilesystem: --")

	logger.Debugf("enumFilesystem: offline = %s", extend.Pretty(offline))

	psinfo, err := info.QueryPsInfo()
	if err != nil {
		return nil, err
	}

	logger.Debugf("enumFilesystem: psinfo=\n%s", psinfo.Pretty())

	slaveSet := make(map[string]struct{})
	addSlaves := func(slaves []string) {
		for _, s := range slaves {
			slaveSet[s] = struct{}{}
		}
	}

	for _, mp := range psinfo.Private.Linux.Multipath {
		addSlaves(mp.Slaves)
	}
	for _, rd := range psinfo.Private.Linux.Raid {
		addSlaves(rd.Slaves)
	}

	var fsDevList []string

	// 统一处理函数
	handleDevice := func(device string, dt info.DiskTable) {
		switch dt.Type {
		case table.TableTypeMBR, table.TableTypeGPT:
			for _, p := range dt.Partitions {
				if isInvalidPartition(p.Type) {
					continue
				}
				fsDevList = append(fsDevList, p.Device)
			}
		default:
			fsDevList = append(fsDevList, device)
		}
	}

	// 1. 普通磁盘
	for _, d := range psinfo.Public.Disks {
		if !funk.InStrings(offline, d.Device) {
			continue
		}
		if _, ok := slaveSet[d.Device]; ok {
			continue
		}
		handleDevice(d.Device, d.Table)
	}

	// 2. multipath
	for _, mp := range psinfo.Private.Linux.Multipath {
		if !allInOffline(mp.Slaves, offline) {
			continue
		}
		handleDevice(mp.Device, mp.Table)
	}

	// 3. raid
	for _, rd := range psinfo.Private.Linux.Raid {
		if !allInOffline(rd.Slaves, offline) {
			continue
		}
		handleDevice(rd.Device, rd.Table)
	}

	// 4. lvm
	for _, vg := range psinfo.Private.Linux.LVM.VGList {
		for _, lv := range vg.LVList {
			// 非标准卷
			if len(lv.Segments) == 0 {
				continue
			}
			lvDisks := make([]string, 0)
			for _, seg := range lv.Segments {
				lvDisks = append(lvDisks, seg.Device)
			}
			if !allInOffline(lvDisks, offline) {
				continue
			}
			fsDevList = append(fsDevList, lv.Device)
		}
	}

	fsDevList = funk.UniqString(fsDevList)

	// 过滤文件系统类型
	var devs []fsDevice
	for _, dev := range fsDevList {
		fsStr, _ := DetectFSTypeByBlkid(dev)
		if funk.InStrings(SupportedFsTypes, fsStr) || fsStr == "swap" {
			devs = append(devs, fsDevice{dev, fsStr})
		}
	}

	return devs, nil
}

func isInvalidPartition(ptype string) bool {
	return funk.InStrings(table.MBRExtendPartTypes, ptype) ||
		ptype == table.GPT_LINUX_LVM ||
		ptype == table.MBR_LINUX_LVM_PARTITION
}

func allInOffline(slaves []string, offline []string) bool {
	if len(slaves) == 0 {
		return false
	}
	for _, s := range slaves {
		if !funk.InStrings(offline, s) {
			return false
		}
	}
	return true
}
