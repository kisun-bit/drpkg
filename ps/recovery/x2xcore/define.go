package x2xcore

import (
	"regexp"

	"github.com/pkg/errors"
)

// Linux 发行版家族
const (
	LinuxFamilyRHEL   = "RHEL"
	LinuxFamilyALT    = "ALT"
	LinuxFamilySUSE   = "SUSE"
	LinuxFamilyDebian = "DEBIAN"
)

// 支持挂载的文件系统类型
var SupportedFsTypes = []string{
	"ext4",
	"ext3",
	"ext2",
	"xfs",
	"fat",
	"vfat",
	"ntfs",
	"cramfs",
	"gfs2",
	"hfs",
	"hfsplus",
	"zfs",
	"jfs",
	"minix",
	"msdos",
	"reiserfs",
}

// Initrd生成工具的类型
const (
	InitrdToolDracut          = "dracut"
	InitrdToolUpdateInitramfs = "update-initramfs"
	InitrdToolMkinitrd        = "mkinitrd"
)

// 虚拟主板型号
const (
	ChipsetQ35    = "q35"
	ChipsetI440fx = "i440fx"
)

// 显卡类型
const (
	VideoBochs  = "bochs"
	VideoVGA    = "vga"
	VideoVirtio = "virtio"
	VideoRamfb  = "ramfb"
)

// 磁盘总线类型
const (
	DiskBusIde    = "ide"
	DiskBusSata   = "sata"
	DiskBusScsi   = "scsi"
	DiskBusVirtio = "virtio"
)

// 默认的离线系统挂载点
var (
	rootDir = "/mnt/sysroot"
)

// 正则匹配相关
var (
	reBlkidType = regexp.MustCompile(`TYPE="([^"]+)"`)
	reBlkidUuid = regexp.MustCompile(`UUID="([^"]+)"`)
)

// 错误相关
var (
	ErrorRootEnvNotMounted = errors.New("root environment is not mounted")
)
