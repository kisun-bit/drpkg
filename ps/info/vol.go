package info

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/kisun-bit/drpkg/extend"
)

var ()

type Volume struct {
	// Name 卷的显示名称
	// Windows: "数据卷 (D:)"
	// Linux: "/dev/sda1" 或 "/dev/mapper/rl-root"
	Name string `json:"name"`

	// Layout 布局类型，
	// 可取：简单、带区（Windows特有）、RAID-5（Windows特有）、镜像（Windows特有）、跨区（Windows特有）等
	Layout extend.VolumeType `json:"layout"`

	// SegmentLayoutType 卷的数据分布类型，可取：line（默认）、mirror、unknown
	SegmentLayoutType extend.SegmentLayoutType `json:"segmentLayoutType"`

	// Segments 卷的数据分布在哪些磁盘区间
	Segments []extend.Segment `json:"segments"`

	// MountPoint 卷的挂载点
	// Windows: 使用卷的装入点，如："C:"
	// Linux: 使用设备的挂载点"/home"
	MountPoint string `json:"mountpoint"`

	// GUID 设备唯一标识符
	// Windows: 取自卷名（如：\\?\Volume{e3b9397c-0000-0000-0000-100000000000}\）中的GUID
	// Linux：取自 blkid 输出的UUID
	GUID string `json:"guid"`

	// Filesystem 卷的文件系统类型
	Filesystem string `json:"filesystem"`

	// Usage 卷使用情况
	Usage UsageInfo `json:"usage"`

	// Size 大小
	Size uint64 `json:"size"`

	// IsBootable 是否为启动卷
	// true 代表此卷与系统启动相关
	IsBootable bool `json:"isBootable"`
	// EnabledBitlocker 卷是否启用了BitLocker加密
	EnabledBitlocker bool `json:"isBitlocker"`

	// TODO 更多字段
}

// UsageInfo 存储空间使用信息
type UsageInfo struct {
	TotalBytes uint64 `json:"totalBytes"`
	UsedBytes  uint64 `json:"usedBytes"`
	AvailBytes uint64 `json:"availBytes"`
}

func EffectiveForBoot(dir string) bool {
	return IsRootDir(dir) || IsBootDir(dir) || IsEfiDir(dir)
}

func IsWindowsRoot(dir string) bool {
	if !extend.ContainAllSubDirs(dir, "Windows") {
		return false
	}
	registryPath := filepath.Join(dir, "Windows", "System32", "config", "SYSTEM")
	return extend.IsExisted(registryPath)
}

func IsLinuxRoot(dir string) bool {
	// 必须目录（放宽）
	if !extend.ContainAllSubDirs(dir, "etc", "usr") {
		return false
	}

	// 至少存在一个关键文件
	passwdPath := filepath.Join(dir, "etc", "passwd")
	if !extend.IsExisted(passwdPath) {
		return false
	}

	// systemd 或 init 存在一个
	initPath := filepath.Join(dir, "sbin", "init")
	if extend.IsExisted(initPath) {
		return true
	}
	sysmdPath := filepath.Join(dir, "lib", "systemd", "systemd")
	if extend.IsExisted(sysmdPath) {
		return true
	}

	return false
}

func IsEfiBoot(dir string) bool {
	if !extend.ContainAllSubDirs(dir, "EFI") {
		return false
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) == 0 {
		return false
	}

	return true
}

func IsLinuxBoot(dir string) bool {
	if extend.ContainAnySubDirs(dir, "grub", "grub2") {
		return true
	}

	if extend.ContainAnySubPrefixFiles(dir, "vmlinuz", "initrd", "initramfs") {
		return true
	}

	return false
}

func IsWindowsBoot(dir string) bool {
	if !extend.ContainAllSubFiles(dir, "bootmgr") {
		return false
	}

	bcdPath := filepath.Join(dir, "Boot", "BCD")
	if extend.IsExisted(bcdPath) {
		return true
	}
	return false
}

func IsRootDir(dir string) bool {
	switch runtime.GOOS {
	case "windows":
		dir = extend.NormalizeWindowsRoot(dir)
		return IsWindowsRoot(dir)
	case "linux":
		return IsLinuxRoot(dir)
	default:
		return false
	}
}

func IsEfiDir(dir string) bool {
	if runtime.GOOS == "windows" {
		dir = extend.NormalizeWindowsRoot(dir)
	}
	return IsEfiBoot(dir)
}

func IsBootDir(dir string) bool {
	switch runtime.GOOS {
	case "windows":
		dir = extend.NormalizeWindowsRoot(dir)
		return IsWindowsBoot(dir)

	case "linux":
		return IsLinuxBoot(dir)

	default:
		return false
	}
}
