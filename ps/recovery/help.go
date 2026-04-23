package recovery

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/kisun-bit/drpkg/command"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/pkg/errors"
)

// IsRoot 若为系统根盘则返回true
func IsRoot(ctx context.Context, device string) bool {
	logger.Debugf("IsRoot(%s) ++", device)
	defer logger.Debugf("IsRoot(%s) --", device)

	switch runtime.GOOS {

	case "windows":
		rootFlagDirNames := []string{
			"Windows",
			"Users",
			"Program Files",
		}

		if !strings.HasSuffix(device, ":") {
			return false
		}

		mountpoint := device + "\\"
		return ContainDirs(mountpoint, rootFlagDirNames)

	case "linux":
		rootFlagDirNames := []string{
			"etc",
			"bin",
			"usr",
			"var",
			"boot",
		}

		mountpointTmpDir, err := os.MkdirTemp("", "isroot-*")
		if err != nil {
			logger.Warnf("IsRoot mkdir temp failed: %v", err)
			return false
		}
		defer os.RemoveAll(mountpointTmpDir)

		//mountCmd := fmt.Sprintf("mount -o ro %s %s", device, mountpointTmpDir)
		//
		//_, output, err := command.ExecuteWithContext(ctx, mountCmd)
		//if err != nil {
		//	logger.Warnf("IsRoot mount failed: dev=%s out=%s err=%v",
		//		device, output, err)
		//	return false
		//}

		support, err := Mount(ctx, device, mountpointTmpDir)
		if err != nil {
			logger.Warnf("IsRoot mount failed: dev=%s err=%v",
				device, err)
			return false
		}
		if !support {
			return false
		}

		defer func() {
			if err = Umount(mountpointTmpDir); err != nil {
				logger.Warnf("IsRoot umount failed: %v", err)
			}
		}()

		return ContainDirs(mountpointTmpDir, rootFlagDirNames)

	default:
		return false
	}
}

// IsEfi 若为EFI盘则返回true
func IsEfi(device string) bool {
	logger.Debugf("IsEfi(%s) ++", device)
	defer logger.Debugf("IsEfi(%s) --", device)

	if runtime.GOOS != "linux" {
		return false
	}

	mountpoint, err := os.MkdirTemp("", "isefi-*")
	if err != nil {
		logger.Warnf("IsEfi mkdir temp failed: %v", err)
		return false
	}
	defer os.RemoveAll(mountpoint)

	cmd := fmt.Sprintf("mount -o ro -t vfat %s %s", device, mountpoint)
	_, output, err := command.Execute(cmd)
	if err != nil {
		logger.Debugf("IsEfi mount failed dev=%s out=%s err=%v", device, output, err)
		return false
	}

	defer func() {
		_ = Umount(mountpoint)
	}()

	efiDir := filepath.Join(mountpoint, "EFI")
	if stat, err := os.Stat(efiDir); err == nil && stat.IsDir() {
		return true
	}

	return false
}

// IsBoot 若为启动盘则返回true
func IsBoot(ctx context.Context, device string) bool {
	logger.Debugf("IsBoot(%s) ++", device)
	defer logger.Debugf("IsBoot(%s) --", device)

	if runtime.GOOS != "linux" {
		return false
	}

	// 常见 boot 目录特征
	rootFlagFiles := []string{
		"vmlinuz",
		"initrd",
		"initramfs",
		"grub",
		"grub2",
	}

	mountpoint, err := os.MkdirTemp("", "isboot-*")
	if err != nil {
		logger.Warnf("IsBoot mkdir temp failed: %v", err)
		return false
	}
	defer os.RemoveAll(mountpoint)

	//// 只读挂载（避免写盘）
	//cmd := fmt.Sprintf("mount -o ro %s %s", device, mountpoint)
	//_, output, err := command.Execute(cmd)
	//if err != nil {
	//	logger.Debugf("IsBoot mount failed dev=%s out=%s err=%v", device, output, err)
	//	return false
	//}
	support, err := Mount(ctx, device, mountpoint)
	if err != nil {
		logger.Warnf("IsBoot mount failed: dev=%s err=%v",
			device, err)
		return false
	}
	if !support {
		return false
	}

	defer func() {
		_ = Umount(mountpoint)
	}()

	// 判断 boot 特征
	for _, name := range rootFlagFiles {
		path := filepath.Join(mountpoint, name)
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}

	// 额外判断：/boot/grub
	grubDir := filepath.Join(mountpoint, "grub")
	if stat, err := os.Stat(grubDir); err == nil && stat.IsDir() {
		return true
	}

	return false
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

func ContainDirs(dir string, subDirNames []string) (ok bool) {
	dirs_, ed := os.ReadDir(dir)
	subFileNames := make([]string, 0)
	if ed == nil {
		for _, d := range dirs_ {
			subFileNames = append(subFileNames, d.Name())
		}
		logger.Debugf("containDirs() sub filenames of %s: %#v", dir, subFileNames)
	}

	for _, flagDirName := range subDirNames {
		flagDirPath := filepath.Join(dir, flagDirName)
		if _, e := os.Stat(flagDirPath); e != nil {
			logger.Debugf("containDirs() subDirNames=%v path=%v existed=false", subDirNames, flagDirPath)
			return false
		}
	}
	return true
}
