package lvm2meta

import "os"

/*
本模块中相关结构，参考自:
1. https://github.com/lvmteam/lvm2
2. https://blog.csdn.net/superyongzhe/article/details/126439071
**/

// LabelHeader LVM 标签头的结构.
type LabelHeader struct {
	ID       [8]byte // 标签的标识符. 必须是"LABELONE"
	Sector   uint64  // 当前标签所处的扇区编号.
	CRC      uint32  // 从 Offset 开始到扇区结尾的数据的CRC校验值.
	Offset   uint32  // 标签正文起始位置偏移（从标签开始位置，以字节为单位进行计算，一般是32，也就是是label_header的大小）
	Typename [8]byte // 标签类型，一般都是“LVM2 001”
}

type StaticPhysicalVolumeHeader struct {
	UUID       [PhysicalVolumeIDLength]byte
	DeviceSize uint64 // PV的大小，注意：若此PV归属于一个VG，那么此值将被覆盖.
}

type PhysicalVolumeHeader struct {
	StaticPhysicalVolumeHeader
	DiskAreas     []DataAreaDescriptor // 数据区域列表.
	MetadataAreas []DataAreaDescriptor // 元数据区域列表.
}

type DataAreaDescriptor struct {
	Offset uint64
	Size   uint64
}

type StaticPhysicalVolumeHeaderExtension struct {
	Version uint32
	Flags   uint32
}

type PhysicalVolumeHeaderExtension struct {
	StaticPhysicalVolumeHeaderExtension
	BooloaderAreas []DataAreaDescriptor
}

type StaticMetadataHeader struct {
	CRC       uint32   // 该结构所在扇区除checksum_xl外所有数据的的CRC校验
	Signature [16]byte // 必须是："\x20LVM2\x20x[5A%r0N*>"
	Version   uint32   // 版本号， 必须是1
	Offset    uint64   // 元数据区域的起始位置的字节偏移，从整个分区的第0个扇区开始计算
	Size      uint64   // 区域的大小，以字节为单位，0表示剩余所有空间
}

type MetadataHeader struct {
	StaticMetadataHeader
	Locations []RawLocationDescriptor
}

type RawLocationDescriptor struct {
	Offset uint64 // 从该扇区开始的偏移地址.
	Size   uint64 // 大小.
	CRC    uint32 // CRC校验.
	// Flags 标志.
	// 1: Location should be ignored
	Flags uint32
}

type PhysicalVolume struct {
	device         string
	deviceHandle   *os.File
	BlkSize        uint32
	PhyBlkSize     uint32
	MetadataBlocks []MetaDataBlock // 调用 NewPhysicalVolume 后会自动生成.

	LabelHeader     LabelHeader
	Header          PhysicalVolumeHeader
	HeaderExt       PhysicalVolumeHeaderExtension
	MetadataHeaders []MetadataHeader
}

type MetaDataBlock struct {
	Offset int64
	Length int
	Bytes  []byte
}
