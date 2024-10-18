package ioctl

// ########################################## Linux平台相关 ##########################################

const (
	DevDiskByPath     = "/dev/disk/by-path"
	DevDiskByPartUUID = "/dev/disk/by-partuuid"
	DevDiskByUUID     = "/dev/disk/by-uuid"
	DevDiskByLabel    = "/dev/disk/by-label"
	DevDiskByID       = "/dev/disk/by-id"
	RunUdevData       = "/run/udev/data"
	SysClassBlock     = "/sys/class/block"
	SysClassNet       = "/sys/class/net"
	ProcSelfMountInfo = "/proc/self/mountinfo"
	ProcSwaps         = "/proc/swaps"
	KernelModels      = "/lib/modules"
)

const (
	LinuxIOCTLGetBlockSize   = 0x00001260
	LinuxIOCTLGetBlockSize64 = 0x80081272 // 获取设备大小.
)

// EffectiveMountPathsForBoot 对于启动/安装Linux系统时(以Centos7.*为例), 下述目录是与系统启动密切相关的.
var EffectiveMountPathsForBoot = []string{
	"/",
	"/usr",
	"/boot",
	"/boot/efi",
	"/home",
	"/var",
	"swap",
	"biosboot",
}

var Grub2PlatformTargets = []string{
	"arm-coreboot",
	"arm-efi",
	"arm-uboot",
	"arm64-efi",
	"i386-coreboot",
	"i386-efi",
	"i386-ieee1275",
	"i386-multiboot",
	"i386-pc",
	"i386-qemu",
	"i386-xen",
	"i386-xen_pvh",
	"ia64-efi",
	"mips-arc",
	"mips-qemu_mips",
	"mipsel-arc",
	"mipsel-loongson",
	"mipsel-qemu_mips",
	"powerpc-ieee1275",
	"riscv32-efi",
	"riscv64-efi",
	"sparc64-ieee1275",
	"x86_64-efi",
	"x86_64-xen",
	// TODO 更多类型.
}

// ########################################## Windows平台相关 ##########################################

// 关于Win32 IOCTL各个控制码使用及其意义, 请参考: https://learn.microsoft.com/en-us/windows/win32/api/winioctl
const (
	IOCTL_DISK_GET_LENGTH_INFO           = 0x0007405C
	IOCTL_DISK_GET_DISK_ATTRIBUTES       = 0x000700f0
	IOCTL_STORAGE_QUERY_PROPERTY         = 0x002d1400
	IOCTL_DISK_GET_PARTITION_INFO_EX     = 0x70048
	IOCTL_VOLUME_GET_VOLUME_DISK_EXTENTS = 0x00560000
)

const _DISK_ATTRIBUTE_OFFLINE = 0x000000001
const _DISK_ATTRIBUTE_READ_ONLY = 0x000000002

// Windows平台迁移支持的最多硬盘数量(操作系统层, 与存储层无关如RAID).
const WindowsDefaultMaxHardDiskNumber = 32

// 关于存储总线类型的枚举值, 请参考: https://learn.microsoft.com/en-us/windows/win32/api/winioctl/ne-winioctl-storage_bus_type
const (
	BusTypeUnknown WIN_STORAGE_BUS_TYPE = iota + 0 // Unknown bus type.
	BusTypeScsi                                    // SCSI bus.
	BusTypeAtapi                                   // ATAPI bus.
	BusTypeAta                                     // ATA Bus.
	BusType1394                                    // IEEE-1394 bus.
	BusTypeSsa                                     // SSA bus. 高性能存储网络.
	BusTypeFibre                                   // Fibre Channel bus. FC存储.
	BusTypeUsb                                     // USB bus.
	BusTypeRAID                                    // RAID bus.
	BusTypeiScsi                                   // iSCSI bus.
	BusTypeSas                                     // Serial Attached SCSI (SAS) bus. 串行SCSI. W2k3 SP1之后(包含)才支持.
	BusTypeSata                                    // SATA bus. W2k3 SP1之后(包含)才支持.
	BusTypeSd
	BusTypeMmc
	BusTypeVirtual
	BusTypeFileBackedVirtual
	BusTypeSpaces
	BusTypeNvme
	BusTypeSCM
	BusTypeUfs
	BusTypeMax
	BusTypeMaxReserved WIN_STORAGE_BUS_TYPE = 0x7F
)

// Windows平台迁移支持的硬盘总线类型.
var WindowsValidStorageBus = []WIN_STORAGE_BUS_TYPE{
	BusTypeAta, BusTypeScsi, BusTypeSata, BusTypeNvme, BusTypeRAID, BusTypeSas,
}

const (
	BootProtoNone   = "none"
	BootProtoDHCP   = "dhcp"
	BootProtoStatic = "static"
)

const (
	TypeIfCfg        = "ifcfg"
	TypeInterface    = "interface"
	TypeNMConnection = "nmconnection"
)
