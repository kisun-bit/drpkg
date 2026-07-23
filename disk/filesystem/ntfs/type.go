package ntfs

const (
	// OemIDOffset is the byte offset of the 8-byte OEM identifier in the
	// boot sector. Real NTFS volumes carry the literal "NTFS    ".
	OemIDOffset   = 3
	OemIDNTFS     = "NTFS    "
	BootSignature = 0xAA55

	// Well-known MFT record numbers.
	MftRecordMFT    = 0 // $MFT itself
	MftRecordRoot   = 5 // the root directory "."
	MftRecordBitmap = 6 // Mft $BITMAP

	// FILE record header flags.
	FileRecordInUse     = 0x0001
	FileRecordDirectory = 0x0002

	// Attribute type codes.
	AttrStandardInformation = 0x10
	AttrAttributeList       = 0x20
	AttrFileName            = 0x30
	AttrData                = 0x80
	AttrIndexRoot           = 0x90
	AttrIndexAllocation     = 0xA0
	AttrReparsePoint        = 0xC0
	AttrEnd                 = 0xFFFFFFFF

	// Attribute flags word (record header offset 0x0C).
	AttrFlagCompressed = 0x0001 // LZNT1-compressed non-resident data
	AttrFlagEncrypted  = 0x4000 // EFS-encrypted (not decodable without keys)
	AttrFlagSparse     = 0x8000 // sparse non-resident data

	// MinBytesPerSector is the smallest sane sector size; NTFS uses 512 and
	// up. Anything below this (notably 1, which makes sectorEnd-2 negative)
	// is rejected. The boot field is a uint16 so the value is already
	// bounded above by 64 KiB; we only need a floor plus a power-of-two
	// requirement.
	MinBytesPerSector = 512
	// MaxRecordSize caps a single MFT FILE record / INDX block. The NTFS
	// spec allows 512B..64KiB; we allow up to 1 MiB of slack and reject
	// anything larger (e.g. the ~2 GiB a signed-shift byte can produce).
	MaxRecordSize = 1 << 20 // 1 MiB
	// MaxRunLengthClusters bounds the cluster count of a single data run so
	// an 8-byte length field of 0x7FFF...FF cannot drive run arithmetic to
	// overflow or a huge allocation. 2^40 clusters is a petabyte-scale
	// ceiling that no legitimate run reaches.
	MaxRunLengthClusters = int64(1) << 40
)

// BootSector holds the parsed BIOS Parameter Block fields the reader
// needs. All multi-byte fields are little-endian on disk.
type BootSector struct {
	BytesPerSector    uint32
	SectorsPerCluster uint32
	MftCluster        uint64 // $MftClusterNumber
	MftRecordSize     uint32 // bytes per FILE record
	IndexRecordSize   uint32 // bytes per INDX block
	TotalSectors      int64
}

func (b *BootSector) ClusterSize() int64 {
	return int64(b.BytesPerSector) * int64(b.SectorsPerCluster)
}

func (b *BootSector) TotalCluster() int64 {
	return b.TotalSectors / int64(b.SectorsPerCluster)
}

// DataRun is one extent of a non-resident attribute: lengthClusters
// clusters starting at startCluster. A sparse run has startCluster == -1
// and represents a hole (zeroes).
type DataRun struct {
	LengthClusters int64
	StartCluster   int64
	Sparse         bool
}

type FileRecord struct {
	Flags uint16
	Attrs []Attribute
}

func (fr *FileRecord) IsDir() bool { return fr.Flags&FileRecordDirectory != 0 }

type Attribute struct {
	TypeCode    uint32
	Name        string
	NonResident bool

	// Flags is the Attribute Flags word (record header offset 0x0C):
	// bit 0x0001 = compressed, 0x4000 = encrypted, 0x8000 = sparse.
	Flags uint16
	// AttrID is the per-record Attribute instance id (record header offset
	// 0x0E), used to disambiguate Attribute-list fragments.
	AttrID uint16

	// resident payload
	ResidentData []byte

	// non-resident payload
	Runs     []DataRun
	RealSize uint64 // logical data size in bytes (authoritative on the VCN-0 fragment)
	// StartVCN/lastVCN bound the runlist fragment this Attribute record
	// carries. A single-record Attribute starts at VCN 0; Attribute-list
	// fragments carry successive VCN ranges that resolveAttributes stitches
	// back into one logical Attribute.
	StartVCN uint64
	LastVCN  uint64
	// CompUnit is the compression-unit size exponent from the non-resident
	// header (offset 0x22): the unit spans 2^compUnit clusters. Zero means the
	// stream is not compressed.
	CompUnit uint16
}

// IsCompressed reports whether this Attribute stores LZNT1-compressed data (a
// non-zero compression unit and the compressed flag set).
func (a *Attribute) IsCompressed() bool {
	return a.NonResident && a.CompUnit != 0 && a.Flags&AttrFlagCompressed != 0
}
