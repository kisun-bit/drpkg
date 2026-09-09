package biotrkmeta

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"strings"
)

const (
	// SignatureStr 是元数据文件的固定签名。
	SignatureStr = "BIOTRKMETA"

	// HeaderSize 是 Header 固定大小。
	HeaderSize = 4096

	// VersionV1_0 是格式版本号 v1.0。
	VersionV1_0 = 0x00010000

	// DefaultBitIndexSpace 是一个 Bitmap Bit 对应的默认磁盘空间大小（4 MiB）。
	DefaultBitIndexSpace = 4 * 1024 * 1024

	// DefaultBitmapClusterSize 是一个 Bitmap Unit 的默认数据大小。
	DefaultBitmapClusterSize = 4096

	// DefaultTotalBitmapUnits 是 Bitmap Unit 默认总数量。
	DefaultTotalBitmapUnits = 8192

	// DefaultMaxProtectedDevices 是预分配的保护设备记录最大数量。
	// Create 会为此预留空间，确保后续 AddProtectDevice 不会导致区域重叠。
	DefaultMaxProtectedDevices = 256

	// MaxExtentsPerDevice 是每个 ProtectedDevice 最大 Extent 数量。
	MaxExtentsPerDevice = 128

	// ProtectedDeviceRecordSize 是单条 ProtectedDevice 记录的固定大小。
	// 4 (Type) + 512 (DeviceID) + 128*556 (Extents) + 2044 (padding) = 73728
	ProtectedDeviceRecordSize = 4 + DeviceIDLen + MaxExtentsPerDevice*ProtectedExtentBinSize + 2044

	// DiskIDBinSize 是 DiskID 的二进制大小。
	// 512 (ID) + 4 (Major) + 4 (Minor)
	DiskIDBinSize = DeviceIDLen + 4 + 4

	// DiskExtentBinSize 是 DiskExtent 的二进制大小。
	// 520 (DiskID) + 8 (Start) + 8 (Size)
	DiskExtentBinSize = DiskIDBinSize + 8 + 8

	// ProtectedExtentBinSize 是 ProtectedExtent 的二进制大小。
	// 4 (IsValid) + 536 (DiskExtent) + 8 (BitmapUnitStart) + 8 (BitmapUnitCount)
	ProtectedExtentBinSize = 4 + DiskExtentBinSize + 8 + 8
)

type DeviceType uint32

const (
	DeviceTypeDisk = iota
	DeviceTypeVolume
)

const (
	DeviceIDLen = 512
)

type ID [DeviceIDLen]byte

// DiskID 表示磁盘的唯一标识。
//
// Windows 使用 PNPDeviceID 标识磁盘。
// Linux 使用稳定的设备路径标识磁盘，并按预定义优先级回退。
type DiskID struct {
	// ID 为磁盘唯一标识。
	//
	// Windows：
	//   使用磁盘的 PNPDeviceID，例如：
	//   SCSI\DISK&VEN_INTEL&PROD_SSDSC2KB480G8\4&240534BA&0&000000
	//
	// Linux：
	//   按以下顺序选择设备路径，前一级不存在时回退到后一级：
	//   1. /dev/disk/by-path
	//   2. /dev/disk/by-id
	//   3. /dev/mapper
	//   4. /dev
	ID ID

	// Major 为 Linux 设备主设备号，仅 Linux 平台有效。
	Major uint32

	// Minor 为 Linux 设备次设备号，仅 Linux 平台有效。
	Minor uint32
}

// String 返回磁盘唯一标识的字符串表示。
func (d *DiskID) String() string {
	ret := trimZeroString(d.ID[:])
	if strings.TrimSpace(ret) == "" {
		return "EmptyDiskID"
	}
	return ret
}

func (d *DiskID) Equal(other *DiskID) bool {
	return bytes.Equal(d.ID[:], other.ID[:])
}

// DevicePath 返回磁盘对应的设备路径。
//
// Windows 返回 PNPDeviceID 对应的 `\\.\PhysicalDriveN`。
// Linux 返回 ID 对应的设备文件路径，不依赖 Major 和 Minor，
// 因为设备号可能随系统重启或设备变化而改变。
func (d *DiskID) DevicePath() (string, error) {
	if runtime.GOOS == "windows" {
		return getWindowsDiskPath(d.String())
	}

	return getLinuxDiskPath(d.String())
}

// DiskExtent 表示磁盘上的一段连续区域。
type DiskExtent struct {
	// DiskID 为所属磁盘的唯一标识。
	DiskID DiskID

	// Start 为区域起始偏移，单位为字节。
	Start uint64

	// Size 为区域长度，单位为字节。
	Size uint64
}

// String 返回磁盘区域的字符串表示。
func (d *DiskExtent) String() string {
	return fmt.Sprintf(
		"disk{%s}[%d+%d)",
		d.DiskID.String(),
		d.Start,
		d.Size,
	)
}

// ProtectedExtent 表示一个受保护的磁盘区域及其对应的位图单元区域。
type ProtectedExtent struct {
	// IsValid 是否有效，0-无效，1-有效
	IsValid bool

	// Extent 为受保护的磁盘区域。
	Extent DiskExtent

	// BitmapUnitStart 起始位图单元索引
	BitmapUnitStart uint64

	// BitmapUnitCount 位图单元数量
	BitmapUnitCount uint64
}

func (p *ProtectedExtent) String() string {
	return fmt.Sprintf(
		"protected_extent{extent=%s, bitmap_unit_start=%d, bitmap_unit_count=%d}",
		p.Extent.String(),
		p.BitmapUnitStart,
		p.BitmapUnitCount,
	)
}

// ProtectedDevice 表示一个受保护的设备。
type ProtectedDevice struct {
	// Type 为设备类型，用于区分磁盘、卷等设备。
	Type DeviceType

	// DeviceID 为设备唯一标识。
	//
	// Windows：
	//   磁盘使用 PNPDeviceID，例如：
	//     SCSI\DISK&VEN_INTEL&PROD_SSDSC2KB480G8\4&240534BA&0&000000
	//   卷使用 Volume GUID 路径，例如：
	//     \\?\Volume{e3b9397c-0000-0000-0000-f0ff18000000}\
	//
	// Linux：
	//   磁盘使用稳定设备路径，例如：
	//     pci-0000:81:00.0-nvme-1
	//   卷使用文件系统 UUID，例如：
	//     592f36e7-ff78-41ef-8a52-ffdb62a4c57a
	DeviceID [DeviceIDLen]byte

	// Extents 为设备上的受保护区域集合。
	Extents [128]ProtectedExtent

	// _padding 将记录大小对齐到 4096 字节，确保 Windows 物理磁盘直写兼容（含 4K 原生磁盘）。
	_padding [2044]byte
}

func (d *ProtectedDevice) String() string {
	extentStrItems := make([]string, 0, len(d.Extents))
	for _, extent := range d.Extents {
		extentStrItems = append(extentStrItems, extent.String())
	}

	return fmt.Sprintf(
		"protected_device{type=%v, device=%s, extents=[%s]}",
		d.Type,
		trimZeroString(d.DeviceID[:]),
		strings.Join(extentStrItems, ", "),
	)
}

// Header 是 BIOTRKMETA 文件头部，固定 4096 字节。
type Header struct {
	Signature             [16]byte
	Version               uint32
	CDPStatus             uint32
	ErrorCode             uint64
	BitIndexSpace         uint64
	BitmapClusterSize     uint32
	TotalBitmapUnits      uint32
	ProtectedRegionOffset uint64
	ProtectedRegionSize   uint64
	ProtectedDeviceCount  uint32
	WorkMode              uint32
	BitmapAllocMapOffset  uint64
	BitmapAllocMapSize    uint64
	BitmapDataOffset      uint64
	HeaderCRC32           uint32
	Reserved              [3996]byte
}

type BioTrkMetadata struct {
	file     *os.File
	filePath string
	offset   int64
	header   Header
	devices  []ProtectedDevice
	allocMap []byte
}
