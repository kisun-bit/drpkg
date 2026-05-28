package define

const (
	OsWindows = "windows"
	OsLinux   = "linux"
)

const (
	ArchAmd64   = "amd64"
	ArchArm64   = "arm64"
	Arch386     = "386"
	ArchLoong64 = "loong64"
	ArchRiscv64 = "riscv64"
)

// Linux发行版ID
const (
	// RHEL family
	DistroFedora          = "fedora"
	DistroRHEL            = "rhel"
	DistroCentOS          = "centos"
	DistroCircle          = "circle"
	DistroScientificLinux = "scientificlinux"
	DistroRedhatBased     = "redhat-based"
	DistroOracleLinux     = "oraclelinux"
	DistroRocky           = "rocky"
	DistroKylin           = "kylin"
	DistroNeoKylin        = "neokylin"
	DistroAnolis          = "anolis"
	DistroOpenEuler       = "openeuler"
	DistroAlma            = "almalinux"

	// ALT family
	DistroALTLinux = "altlinux"

	// SUSE family
	DistroSLES      = "sles"
	DistroSUSEBased = "suse-based"
	DistroOpenSUSE  = "opensuse"

	// Debian family
	DistroDebian    = "debian"
	DistroUbuntu    = "ubuntu"
	DistroLinuxMint = "linuxmint"
	DistroKaliLinux = "kalilinux"
)

const (
	DistroMicrosoft = "microsoft"
)

// Linux 发行版家族
const (
	LinuxFamilyRHEL   = "RHEL"
	LinuxFamilyALT    = "ALT"
	LinuxFamilySUSE   = "SUSE"
	LinuxFamilyDebian = "DEBIAN"
)

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

// 文件系统类型
const (
	FsTypeExt4 = "ext4"
	FsTypeExt3 = "ext3"
	FsTypeExt2 = "ext2"

	FsTypeXFS   = "xfs"
	FsTypeBtrfs = "btrfs"

	FsTypeNtfs = "ntfs"

	FsTypeFAT   = "fat"
	FsTypeVFAT  = "vfat"
	FsTypeMSDOS = "msdos"
	FsTypeNTFS  = "ntfs"

	FsTypeCramFS = "cramfs"
	FsTypeGFS2   = "gfs2"

	FsTypeHFS     = "hfs"
	FsTypeHFSPlus = "hfsplus"

	FsTypeZFS = "zfs"
	FsTypeJFS = "jfs"

	FsTypeMinix    = "minix"
	FsTypeReiserFS = "reiserfs"

	FsTypeSwap = "swap"
)

// HardwarePlatform 基础硬件平台
type HardwarePlatform string

const (
	HPUnknown   HardwarePlatform = "unknown"
	HPVirt      HardwarePlatform = "virtual"    // 虚拟化 / 云平台
	HPBareMetal HardwarePlatform = "bare-metal" // 裸机（物理机）
)

// HPVirtType 虚拟化/云平台的类别
type HPVirtType string

const (
	HPVTNone   HPVirtType = "none"
	HPVTVmware HPVirtType = "vmware"
	HPVTKvm    HPVirtType = "kvm"
	HPVTXen    HPVirtType = "xen"
	HPVTHyperV HPVirtType = "hyper-v"
)

// BootMode 启动模式
type BootMode string

const (
	BootModeUEFI BootMode = "uefi"
	BootModeBIOS BootMode = "bios"
)
