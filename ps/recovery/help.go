package recovery

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/kisun-bit/drpkg/command"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/pkg/errors"
)

var SupportedFsTypes = []string{"ext4", "ext3", "ext2", "xfs", "fat", "vfat", "ntfs", "cramfs", "gfs2", "hfs", "hfsplus", "zfs", "jfs", "minix", "msdos", "reiserfs"}

// IsRootDevice 是否为系统根盘
func IsRootDevice(ctx context.Context, device string) bool {
	return withMount(ctx, device, "IsRootDevice", extend.IsRootDir)
}

// IsEfiDevice 是否为 EFI 分区
func IsEfiDevice(ctx context.Context, device string) bool {
	return withMount(ctx, device, "IsEfiDevice", extend.IsEfiDir)
}

// IsBootDevice 是否为启动分区
func IsBootDevice(ctx context.Context, device string) bool {
	return withMount(ctx, device, "IsBootDevice", extend.IsBootDir)
}

// DetectFSTypeByBlkid 使用 blkid 探测文件系统类型
func DetectFSTypeByBlkid(device string) (string, error) {
	switch runtime.GOOS {
	case "linux":
		_, output, err := command.Execute(fmt.Sprintf("blkid -o value -s TYPE %s", device))
		if err != nil {
			return "", err
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
