// Package define provides common constants and type definitions
// for operating systems, architectures, filesystems,
// virtualization platforms and hardware environments.
package define

// OSType represents operating system type.
type OSType string

const (
	OSTypeWindows OSType = "windows"
	OSTypeLinux   OSType = "linux"
)

// OSArchitecture represents CPU architecture.
type OSArchitecture string

const (
	ArchAMD64   OSArchitecture = "amd64"
	ArchARM64   OSArchitecture = "arm64"
	Arch386     OSArchitecture = "386"
	ArchLoong64 OSArchitecture = "loong64"
	ArchRISCV64 OSArchitecture = "riscv64"
)

// OSDistro represents Linux distribution ID.
type OSDistro string

const (
	// RHEL family
	DistroFedora          OSDistro = "fedora"
	DistroRHEL                     = "rhel"
	DistroCentOS                   = "centos"
	DistroCircle                   = "circle"
	DistroScientificLinux          = "scientificlinux"
	DistroRedhatBased              = "redhat-based"
	DistroOracleLinux              = "ol"
	DistroRocky                    = "rocky"
	DistroKylin                    = "kylin"
	DistroNeoKylin                 = "neokylin"
	DistroAnolis                   = "anolis"
	DistroOpenEuler                = "openeuler"
	DistroAlma                     = "almalinux"

	// ALT family
	DistroALTLinux OSDistro = "altlinux"

	// SUSE family
	DistroSLES      OSDistro = "sles"
	DistroSUSEBased          = "suse-based"
	DistroOpenSUSE           = "opensuse"

	// Debian family
	DistroDebian    OSDistro = "debian"
	DistroUbuntu             = "ubuntu"
	DistroLinuxMint          = "linuxmint"
	DistroKaliLinux          = "kalilinux"

	// Microsoft
	DistroMicrosoft OSDistro = "microsoft"
)

// WindowsVersion represents Windows version.
type WindowsVersion string

const (
	WinUnknown WindowsVersion = "Unknown"

	Win2k    WindowsVersion = "win2k"
	WinXP                   = "winxp"
	WinVista                = "winvista"

	Win7  = "win7"
	Win8  = "win8"
	Win81 = "win8.1"
	Win10 = "win10"
	Win11 = "win11"

	Win2k3    = "win2k3"
	Win2k8    = "win2k8"
	Win2k8r2  = "win2k8r2"
	Win2k12   = "win2k12"
	Win2k12r2 = "win2k12r2"
	Win2k16   = "win2k16"
	Win2k19   = "win2k19"
	Win2k22   = "win2k22"
	Win2k25   = "win2k25"
)

// NTVersion represents Windows NT kernel version.
type NTVersion int

const (
	NTUnknown NTVersion = iota

	// Windows 2000
	NT50

	// Windows XP / Server 2003
	NT51
	NT52

	// Windows Vista / Server 2008
	NT60

	// Windows 7 / Server 2008 R2
	NT61

	// Windows 8 / Server 2012
	NT62

	// Windows 8.1 / Server 2012 R2
	NT63

	// Windows 10 / 11 / Server 2016+
	NT100
)

// OsNTVersion maps Windows version to NT version.
var OsNTVersion = map[WindowsVersion]NTVersion{
	Win2k: NT50,

	WinXP:  NT51,
	Win2k3: NT52,

	WinVista: NT60,
	Win2k8:   NT60,

	Win7:     NT61,
	Win2k8r2: NT61,

	Win8:    NT62,
	Win2k12: NT62,

	Win81:     NT63,
	Win2k12r2: NT63,

	Win10: NT100,
	Win11: NT100,

	Win2k16: NT100,
	Win2k19: NT100,
	Win2k22: NT100,
	Win2k25: NT100,
}

// HALType represents Windows Hardware Abstraction Layer type.
type HALType int

const (
	HALUnknown HALType = iota

	HALACPIMultiprocessor
	HALACPIUniprocessor
	HALStandardPC
	HALMPSMultiprocessor
	HALMPSUniprocessor
)

// OSFamily represents operating system family.
type OSFamily string

const (
	LinuxFamilyRHEL   OSFamily = "RHEL"
	LinuxFamilyALT             = "ALT"
	LinuxFamilySUSE            = "SUSE"
	LinuxFamilyDebian          = "DEBIAN"

	WindowsFamily OSFamily = "MICROSOFT"
)

// Initrd generation tools.
const (
	InitrdToolDracut          = "dracut"
	InitrdToolUpdateInitramfs = "update-initramfs"
	InitrdToolMkinitrd        = "mkinitrd"
)

// Virtual machine chipset.
const (
	ChipsetQ35    = "q35"
	ChipsetI440fx = "i440fx"
)

// Virtual GPU type.
const (
	VideoBochs  = "bochs"
	VideoVGA    = "vga"
	VideoVirtio = "virtio"
	VideoRamfb  = "ramfb"
)

// Disk bus type.
const (
	DiskBusIde        = "ide"
	DiskBusSata       = "sata"
	DiskBusVirtioScsi = "scsi"
	DiskBusVirtio     = "virtio"
)

// Network device type.
const (
	NetworkTypeE1000   = "e1000"
	NetworkTypeRTL8192 = "rtl8192"
	NetworkTypeVIRTIO  = "virtio"
)

// Filesystem type.
const (
	FsTypeUnknown = "unknown"

	FsTypeExt4  = "ext4"
	FsTypeExt3  = "ext3"
	FsTypeExt2  = "ext2"
	FsTypeExtFs = "ext2/3/4"

	FsTypeXFS   = "xfs"
	FsTypeBtrfs = "btrfs"
	FstypeApfs  = "apfs"

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

// HardwarePlatform represents physical or virtual platform.
type HardwarePlatform string

const (
	HPUnknown   HardwarePlatform = "unknown"
	HPVirt      HardwarePlatform = "virtual"
	HPBareMetal HardwarePlatform = "bare-metal"
)

// HPVirtType represents virtualization platform.
type HPVirtType string

const (
	HPVTNone      HPVirtType = "none"
	HPVTVmware    HPVirtType = "vmware"
	HPVTKvm       HPVirtType = "kvm"
	HPVTXen       HPVirtType = "xen"
	HPVTHyperV    HPVirtType = "hyperv"
	HPVTParallels HPVirtType = "parallels"
)

// BootMode represents system boot mode.
type BootMode string

const (
	BootModeUEFI BootMode = "uefi"
	BootModeBIOS BootMode = "bios"
)

// Signer represents driver signature issuer.
type Signer string

const (
	DrvSignerPrivate   Signer = "sign-private"
	DrvSignerVendor           = "sign-vendor"
	DrvSignerDistro           = "sign-distro"
	DrvSignerMicrosoft        = "sign-microsoft"
	DrvSignerWHQL             = "sign-whql"
)

// Hash represents cryptographic hash algorithm.
type Hash string

const (
	DrvHashUnknown Hash = "unknown"
	DrvHashSHA1         = "sha1"
	DrvHashSHA224       = "sha224"
	DrvHashSHA256       = "sha256"
	DrvHashSHA384       = "sha384"
	DrvHashSHA512       = "sha512"
)
