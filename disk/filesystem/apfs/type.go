// apfs_types.go
package apfs

// On-disk APFS structures (little-endian), per Apple's APFS Reference.
// Only the fields this parser actually needs are included; each struct is
// read with encoding/binary against a raw block buffer, so struct order
// must match the real on-disk layout exactly up to the last field used.

const (
	apfsMagic    = 0x4253584E // "NXSB"
	minBlockSize = 4096
	maxBlockSize = 4096

	maxTotalBlocks    = uint64(1) << 50
	maxXPDescBlocks   = uint32(1) << 16
	maxSpacemanSize   = uint32(1) << 20
	maxChunkInfoCount = uint64(1) << 24
	maxChunkPerCIB    = uint32(1) << 16

	objEphemeral         = 0x80000000
	objPhysical          = 0x40000000
	objTypeCheckpointMap = 0x0000000c
	objTypeSpaceman      = 0x00000005
	objTypeSpacemanCIB   = 0x40000007 // OBJ_PHYSICAL | OBJECT_TYPE_SPACEMAN_CIB

	checkpointMapLast = 0x00000001

	sdCount = 2 // SD_MAIN, SD_TIER2
)

// ObjPhys is the 32-byte object header present at the start of every
// physical/ephemeral APFS object.
type ObjPhys struct {
	Cksum   uint64
	Oid     uint64
	Xid     uint64
	Type    uint32
	Subtype uint32
}

// NxSuperblock mirrors nx_superblock_t up through the fields we use.
// Anything after NxSpacemanOid is intentionally omitted; binary.Read only
// consumes len(struct) bytes so this is safe as long as field order/offsets
// before that point match the real layout.
type NxSuperblock struct {
	NxO                          ObjPhys
	NxMagic                      uint32
	NxBlockSize                  uint32
	NxBlockCount                 uint64
	NxFeatures                   uint64
	NxReadonlyCompatibleFeatures uint64
	NxIncompatibleFeatures       uint64
	NxUUID                       [16]byte
	NxNextOid                    uint64
	NxNextXid                    uint64
	NxXpDescBlocks               uint32
	NxXpDataBlocks               uint32
	NxXpDescBase                 int64
	NxXpDataBase                 int64
	NxXpDescNext                 uint32
	NxXpDataNext                 uint32
	NxXpDescIndex                uint32
	NxXpDescLen                  uint32
	NxXpDataIndex                uint32
	NxXpDataLen                  uint32
	NxSpacemanOid                uint64
	// nx_omap_oid, nx_reaper_oid, ... intentionally omitted (unused).
}

// CheckpointMapping mirrors checkpoint_mapping_t (32 bytes).
type CheckpointMapping struct {
	CpmType    uint32
	CpmSubtype uint32
	CpmSize    uint32
	CpmPad     uint32
	CpmFsOid   uint64
	CpmOid     uint64
	CpmPaddr   uint64
}

// CheckpointMapPhys header (cpm_map entries are parsed separately since
// they're a variable-length trailing array).
type CheckpointMapPhysHeader struct {
	CpmO     ObjPhys
	CpmFlags uint32
	CpmCount uint32
}

// SpacemanDevice mirrors spaceman_device_t (48 bytes).
type SpacemanDevice struct {
	SmBlockCount  uint64
	SmChunksCount uint64
	SmCibCount    uint32
	SmCabCount    uint32
	SmFreeCount   uint64
	SmAddrOffset  uint32
	SmReserved    uint32
	SmReserved2   uint64
}

// SpacemanPhysHeader mirrors spaceman_phys_t up through sm_dev[sdCount].
// Fields after sm_dev (flags, internal-pool info, free queues, ...) are
// not needed for bitmap reconstruction and are omitted.
type SpacemanPhysHeader struct {
	SmO              ObjPhys
	SmBlockSize      uint32
	SmBlocksPerChunk uint32
	SmChunksPerCib   uint32
	SmCibsPerCab     uint32
	SmDev            [sdCount]SpacemanDevice
}

// ChunkInfo mirrors chunk_info_t (32 bytes).
type ChunkInfo struct {
	CiXid        uint64
	CiAddr       uint64
	CiBlockCount uint32
	CiFreeCount  uint32
	CiBitmapAddr int64
}

// ChunkInfoBlockHeader mirrors chunk_info_block_t's fixed header
// (cib_chunk_info entries are parsed separately).
type ChunkInfoBlockHeader struct {
	CibO              ObjPhys
	CibIndex          uint32
	CibChunkInfoCount uint32
}
