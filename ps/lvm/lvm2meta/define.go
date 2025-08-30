package lvm2meta

const (
	SectorSize = 1 << 9 // 512
	InitialCRC = 0xf597a6cf
)

const (
	LabelID = "LABELONE"
	// Label can be in any of the first 4 sectors
	LabelScanSectors = 4
	LabelScanSize    = LabelScanSectors * SectorSize
	LabelHeaderSize  = 32

	PhysicalVolumeIDLength = 32
)

const (
	RawLocationDescriptorSize = 24
)

const (
	MetadataHeaderSize = SectorSize
)
