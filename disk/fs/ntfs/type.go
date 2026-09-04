package ntfs

const (
	// oemIDOffset is the byte offset of the 8-byte OEM identifier in the
	// boot sector. Real NTFS volumes carry the literal "NTFS    ".
	oemIDOffset   = 3
	oemIDNTFS     = "NTFS    "
	bootSignature = 0xAA55

	// Well-known MFT record numbers.
	mftRecordMFT    = 0 // $MFT itself
	mftRecordRoot   = 5 // the root directory "."
	mftRecordBitmap = 6 // Mft $BITMAP

	// FILE record header flags.
	fileRecordInUse     = 0x0001
	fileRecordDirectory = 0x0002

	// attribute type codes.
	attrStandardInformation = 0x10
	attrAttributeList       = 0x20
	attrFileName            = 0x30
	attrData                = 0x80
	attrIndexRoot           = 0x90
	attrIndexAllocation     = 0xA0
	attrReparsePoint        = 0xC0
	attrEnd                 = 0xFFFFFFFF

	// attribute flags word (record header offset 0x0C).
	attrFlagCompressed = 0x0001 // LZNT1-compressed non-resident data
	attrFlagEncrypted  = 0x4000 // EFS-encrypted (not decodable without keys)
	attrFlagSparse     = 0x8000 // sparse non-resident data

	// minBytesPerSector is the smallest sane sector size; NTFS uses 512 and
	// up. Anything below this (notably 1, which makes sectorEnd-2 negative)
	// is rejected. The boot field is a uint16 so the value is already
	// bounded above by 64 KiB; we only need a floor plus a power-of-two
	// requirement.
	minBytesPerSector = 512
	// maxRecordSize caps a single MFT FILE record / INDX block. The NTFS
	// spec allows 512B..64KiB; we allow up to 1 MiB of slack and reject
	// anything larger (e.g. the ~2 GiB a signed-shift byte can produce).
	maxRecordSize = 1 << 20 // 1 MiB
	// maxRunLengthClusters bounds the cluster count of a single data run so
	// an 8-byte length field of 0x7FFF...FF cannot drive run arithmetic to
	// overflow or a huge allocation. 2^40 clusters is a petabyte-scale
	// ceiling that no legitimate run reaches.
	maxRunLengthClusters = int64(1) << 40
)

// bootSector holds the parsed BIOS Parameter Block fields the reader
// needs. All multi-byte fields are little-endian on disk.
type bootSector struct {
	bytesPerSector    uint32
	sectorsPerCluster uint32
	mftCluster        uint64 // $MftClusterNumber
	mftRecordSize     uint32 // bytes per FILE record
	indexRecordSize   uint32 // bytes per INDX block
	totalSectors      int64
}

func (b *bootSector) clusterSize() int64 {
	return int64(b.bytesPerSector) * int64(b.sectorsPerCluster)
}

func (b *bootSector) totalCluster() int64 {
	return b.totalSectors / int64(b.sectorsPerCluster)
}

// dataRun is one extent of a non-resident attribute: lengthClusters
// clusters starting at startCluster. A sparse run has startCluster == -1
// and represents a hole (zeroes).
type dataRun struct {
	lengthClusters int64
	startCluster   int64
	sparse         bool
}

type fileRecord struct {
	flags uint16
	attrs []attribute
}

func (fr *fileRecord) isDir() bool { return fr.flags&fileRecordDirectory != 0 }

type attribute struct {
	typeCode    uint32
	name        string
	nonResident bool

	// flags is the attribute flags word (record header offset 0x0C):
	// bit 0x0001 = compressed, 0x4000 = encrypted, 0x8000 = sparse.
	flags uint16
	// attrID is the per-record attribute instance id (record header offset
	// 0x0E), used to disambiguate attribute-list fragments.
	attrID uint16

	// resident payload
	residentData []byte

	// non-resident payload
	runs     []dataRun
	realSize uint64 // logical data size in bytes (authoritative on the VCN-0 fragment)
	// startVCN/lastVCN bound the runlist fragment this attribute record
	// carries. A single-record attribute starts at VCN 0; attribute-list
	// fragments carry successive VCN ranges that resolveAttributes stitches
	// back into one logical attribute.
	startVCN uint64
	lastVCN  uint64
	// compUnit is the compression-unit size exponent from the non-resident
	// header (offset 0x22): the unit spans 2^compUnit clusters. Zero means the
	// stream is not compressed.
	compUnit uint16
}

// IsCompressed reports whether this attribute stores LZNT1-compressed data (a
// non-zero compression unit and the compressed flag set).
func (a *attribute) IsCompressed() bool {
	return a.nonResident && a.compUnit != 0 && a.flags&attrFlagCompressed != 0
}
