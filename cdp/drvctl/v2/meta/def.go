package meta

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/kisun-bit/drpkg/xutil"
)

const (
	// SignatureStr 是元数据文件的固定签名。
	SignatureStr = "BIOTRKMETA"

	// HeaderSize 是 Header 固定大小。
	HeaderSize = 4096

	// AlignSize 是区域起始偏移与区域大小的对齐粒度。
	//
	// 元数据可能直接写在裸设备上（如 \\.\PHYSICALDRIVE0），Windows 要求裸设备 I/O 的
	// 偏移与长度都按扇区对齐，4Kn 原生扇区磁盘要求 4096 对齐。所有区域起始偏移与区域
	// 大小统一按 4096 对齐后，每个区域都可以整体做一次对齐读写。
	//
	// 注意对齐粒度是"区域"而不是"单条记录"：Protected Device Records 是变长的，
	// 单条记录长度不保证对齐，因此读写都以整个 Protected Region 为单位。
	AlignSize = 4096

	// Versionv1_0 是格式版本号 v1.0。
	Versionv1_0 = 0x00010000

	// DefaultBitIndexSpace 是一个 Bitmap Bit 对应的默认磁盘空间大小（512 KiB）。
	DefaultBitIndexSpace = 512 * 1024

	// DefaultBitmapClusterSize 是一个 Bitmap Unit 的默认数据大小。
	DefaultBitmapClusterSize = 4096

	// DefaultTotalBitmapUnits 是 Bitmap Unit 默认总数量。
	DefaultTotalBitmapUnits = 8192

	// DiskIDBinSize 是 DiskID 的二进制大小。
	// 512 (ID) + 4 (Major) + 4 (Minor)
	DiskIDBinSize = DeviceIDLen + 4 + 4

	// DiskExtentBinSize 是 DiskExtent 的二进制大小。
	// 520 (DiskID) + 8 (Start) + 8 (Size)
	//
	// ProtectedExtent.BitmapExtents 也是 DiskExtent，因此每一项同样占 536 Byte。
	DiskExtentBinSize = DiskIDBinSize + 8 + 8

	// ProtectedExtentFixedBinSize 是 ProtectedExtent 中不随 BitmapExtents 数量变化的部分。
	// 536 (Extent) + 8 (BitmapUnitStart) + 8 (BitmapUnitCount) + 4 (BitmapExtentCount)
	ProtectedExtentFixedBinSize = DiskExtentBinSize + 8 + 8 + 4

	// ProtectedDeviceFixedBinSize 是 ProtectedDevice 中不随 Extents 数量变化的部分。
	// 4 (Type) + 512 (DeviceID) + 4 (ExtentCount)
	ProtectedDeviceFixedBinSize = 4 + DeviceIDLen + 4

	// ProtectedDeviceMinBinSize 是单条 ProtectedDevice 记录的最小二进制大小。
	// 4 (TotalSize) + 4 (Type) + 512 (DeviceID) + 4 (ExtentCount)
	ProtectedDeviceMinBinSize = 4 + ProtectedDeviceFixedBinSize
)

type DeviceType uint32

const (
	DeviceTypeDisk = iota
	DeviceTypeVolume
)

type CDPStatus uint32

const (
	CDPStatusIdle  CDPStatus = iota // 空闲
	CDPStatusCDP                    // 实时备份
	CDPStatusCBT                    // 定时备份
	CDPStatusError                  // 异常
)

type RecordType uint8

const (
	RecordFromSharedMemory RecordType = iota + 1 // 来自共享内存中读取的实时IO数据
	RecordFromDisk                               // 来自磁盘的位图IO数据
)

const (
	DeviceIDLen = 512
)

type ID [DeviceIDLen]byte

func (id *ID) String() string {
	return xutil.Md5([]byte(xutil.TrimZeroString((*id)[:])))
}

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
	ret := xutil.TrimZeroString(d.ID[:])
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

type Segment struct {
	Start uint64
	Size  uint64
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
//
// Extents 与 BitmapExtents 都是变长切片，实际数量分别由所属 ProtectedDevice 的
// ExtentCount 和本结构的 BitmapExtentCount 记录，文件中按数量紧凑排列，无预留槽位。
//
// 切片里只存放有效项，因此不需要逐项的有效标记。
type ProtectedExtent struct {
	// Extent 为受保护的磁盘区域。
	Extent DiskExtent

	// BitmapUnitStart 起始位图单元索引
	BitmapUnitStart uint64

	// BitmapUnitCount 位图单元数量
	BitmapUnitCount uint64

	// BitmapExtentCount 为 BitmapExtents 的有效数量，与 len(BitmapExtents) 保持一致。
	BitmapExtentCount uint32 `struc:"sizeof=BitmapExtents"`

	// BitmapExtents 位图在物理磁盘的分布。
	// 需要调用Flush接口后基于PhysicalExtents接口的返回值确认位图单元数据区的分布，
	// 再基于这个ProtectedExtent的起始位图单元索引和长度，确定最终这个位图数据在物理磁盘的分布，
	// 最后将结果填充此值到文件中，然后再调用ListValidProtectDevice就能知道这个列表具体是怎样的了。
	//
	// 注意这里的 Start 是位图数据在物理磁盘上的绝对偏移，
	// 不是元数据区域内的相对偏移。
	BitmapExtents []DiskExtent
}

func (p *ProtectedExtent) String() string {
	return fmt.Sprintf(
		"protected_extent{extent=%s, bitmap_unit_start=%d, bitmap_unit_count=%d, bitmap_extents=%d}",
		p.Extent.String(),
		p.BitmapUnitStart,
		p.BitmapUnitCount,
		len(p.BitmapExtents),
	)
}

// ProtectedDevice 表示一个受保护的设备。
//
// 每一个 ProtectedDevice 对应 Protected Region 中的一条变长记录，
// 记录以 TotalSize 前缀开头，读取时按前缀顺序扫描。
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
	DeviceID ID

	// ExtentCount 为 Extents 的有效数量，与 len(Extents) 保持一致。
	ExtentCount uint32 `struc:"sizeof=Extents"`

	// Extents 为设备上的受保护区域集合。
	Extents []ProtectedExtent
}

func (d *ProtectedDevice) String() string {
	extentStrItems := make([]string, 0, len(d.Extents))
	for _, extent := range d.Extents {
		extentStrItems = append(extentStrItems, extent.String())
	}

	return fmt.Sprintf(
		"protected_device{type=%v, device=%s, extents=[%s]}",
		d.Type,
		xutil.TrimZeroString(d.DeviceID[:]),
		strings.Join(extentStrItems, ", "),
	)
}

// Header 是 BIOTRKMETA 文件头部，固定 4096 字节。
//
// 区域顺序为 Header → Allocation Map → Bitmap Unit Data → Protected Device Records。
// 记录区放在最后，是为了让变长记录可以自由增长：它后面没有任何其他区域，
// 长大也不会踩坏 Allocation Map 或 Bitmap Unit Data。
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
	Reserved              [HeaderSize - 100]byte
}

// BitmapAllocMapTotalSize 返回 Allocation Map 区域大小（已按 AlignSize 对齐）。
func (h *Header) BitmapAllocMapTotalSize() uint64 {
	return alignUp(uint64((h.TotalBitmapUnits + 7) / 8))
}

// BitmapDataTotalSize 返回 Bitmap Unit Data 区域大小。
func (h *Header) BitmapDataTotalSize() uint64 {
	return uint64(h.BitmapClusterSize) * uint64(h.TotalBitmapUnits)
}

// TotalSize 返回整个元数据区域的大小。
//
// Protected Region 是最后一个区域，因此总大小等于它的末尾。
// 该值随设备记录增减而变化。
func (h *Header) TotalSize() int64 {
	return int64(h.ProtectedRegionOffset + h.ProtectedRegionSize)
}

// applyBaseLayout 推导与记录内容无关的区域偏移。
//
// ProtectedRegionSize 由实际写入的记录决定，不在此处设置。
// 各区域首尾相接且起始偏移都是 AlignSize 的整数倍，因此每个区域都能整体做扇区对齐读写。
func applyBaseLayout(h *Header) {
	h.BitmapAllocMapOffset = HeaderSize
	h.BitmapAllocMapSize = h.BitmapAllocMapTotalSize()
	h.BitmapDataOffset = h.BitmapAllocMapOffset + h.BitmapAllocMapSize
	h.ProtectedRegionOffset = h.BitmapDataOffset + h.BitmapDataTotalSize()
}

// alignUp 将 v 向上取整到 AlignSize 的整数倍。
func alignUp(v uint64) uint64 {
	return (v + AlignSize - 1) / AlignSize * AlignSize
}

type BioTrkMetadata struct {
	file     *os.File
	filePath string
	offset   int64
	header   Header
	devices  []ProtectedDevice
	allocMap []byte

	// regionHighWater 是 Protected Region 历史上写到的最大字节数。
	// 记录变少时区域会缩小，Flush 需要把 [新末尾, regionHighWater) 一并写零，
	// 否则磁盘上会残留已删除设备的旧记录。
	regionHighWater uint64
}
