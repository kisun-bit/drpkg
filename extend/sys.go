package extend

import (
	"runtime"
)

// ########################################## Windows平台相关 ##########################################

// 关于Win32 IOCTL各个控制码使用及其意义, 请参考: https://learn.microsoft.com/en-us/windows/win32/api/winioctl
const (
	IOCTL_DISK_GET_LENGTH_INFO           = 0x0007405C
	IOCTL_DISK_GET_DISK_ATTRIBUTES       = 0x000700f0
	IOCTL_STORAGE_QUERY_PROPERTY         = 0x002d1400
	IOCTL_DISK_GET_PARTITION_INFO_EX     = 0x70048
	IOCTL_VOLUME_GET_VOLUME_DISK_EXTENTS = 0x00560000
)

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

type WIN_STORAGE_BUS_TYPE byte

func (busType WIN_STORAGE_BUS_TYPE) String() string {
	switch busType {
	case BusTypeUnknown:
		return "Unknown"
	case BusTypeScsi:
		return "Scsi"
	case BusTypeAtapi:
		return "Atapi" // 光学设备如CD
	case BusTypeAta:
		return "Ata"
	case BusType1394:
		return "1394"
	case BusTypeSsa:
		return "Ssa"
	case BusTypeFibre:
		return "Fibre"
	case BusTypeUsb:
		return "Usb"
	case BusTypeRAID:
		return "RAID"
	case BusTypeiScsi:
		return "iScsi"
	case BusTypeSas:
		// SCSI 设备的一种类型，其中 SAS（Serial Attached SCSI，串行连接 SCSI）是其中之一,
		// SAS 是一种用于连接服务器和存储设备的高速、可靠的接口标准。它是 SCSI 标准的一种延伸，
		// 采用了串行连接的方式，提供了更高的性能和可靠性.
		return "Sas"
	case BusTypeSata:
		return "Sata"
	case BusTypeSd:
		return "Sd"
	case BusTypeMmc:
		return "Mmc"
	case BusTypeVirtual:
		return "Virtual"
	case BusTypeFileBackedVirtual:
		return "FileBackedVirtual"
	case BusTypeSpaces:
		return "Spaces"
	case BusTypeNvme:
		return "Nvme"
	case BusTypeSCM:
		return "SCM"
	case BusTypeUfs:
		return "Ufs"
	default:
		return ""
	}
}

// ########################################## Linux平台相关 ##########################################
const (
	LinuxIOCTLGetBlockSize   = 0x00001260
	LinuxIOCTLGetBLKPBSZ     = 0x0000126b
	LinuxIOCTLGetBlockSize64 = 0x80081272 // 获取设备大小
	LinuxIOCTLGetBLKBSZ      = 0x80081270
)

func IsWindowsPlatform() bool {
	return runtime.GOOS == "windows"
}
