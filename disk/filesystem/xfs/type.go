package xfs

const (
	XFS_SB_MAGIC   = 0x58465342 // 'XFSB'
	XFS_AGF_MAGIC  = 0x58414746 // 'XAGF'
	XFS_AGI_MAGIC  = 0x58414749 // 'XAGI'
	XFS_AGFL_MAGIC = 0x5841464c // 'XAFL'
)

const (
	XFS_ABTB_MAGIC     uint32 = 0x41425442 // 'ABTB'（v4，非CRC）
	XFS_ABTB_CRC_MAGIC uint32 = 0x41423342 // 'AB3B'（v5，CRC启用）
)

// SB 版本号（sb_versionnum 低 4 位表示版本号）
type XfsSbVersion uint16

const (
	XFS_SB_VERSION_1 XfsSbVersion = 1 // 5.3, 6.0.1, 6.1
	XFS_SB_VERSION_2 XfsSbVersion = 2 // 6.2 - attributes
	XFS_SB_VERSION_3 XfsSbVersion = 3 // 6.2 - new inode version
	XFS_SB_VERSION_4 XfsSbVersion = 4 // 6.2+ - bitmask version
	XFS_SB_VERSION_5 XfsSbVersion = 5 // CRC enabled filesystem
)

// SB 版本号相关的位掩码
type XfsSbVersionBit uint16

const (
	XFS_SB_VERSION_NUMBITS     XfsSbVersionBit = 0x000f // 版本号所占的位
	XFS_SB_VERSION_ALLFBITS    XfsSbVersionBit = 0xfff0 // 所有 feature bit 的掩码
	XFS_SB_VERSION_ATTRBIT     XfsSbVersionBit = 0x0010
	XFS_SB_VERSION_NLINKBIT    XfsSbVersionBit = 0x0020
	XFS_SB_VERSION_QUOTABIT    XfsSbVersionBit = 0x0040
	XFS_SB_VERSION_ALIGNBIT    XfsSbVersionBit = 0x0080
	XFS_SB_VERSION_DALIGNBIT   XfsSbVersionBit = 0x0100
	XFS_SB_VERSION_SHAREDBIT   XfsSbVersionBit = 0x0200
	XFS_SB_VERSION_LOGV2BIT    XfsSbVersionBit = 0x0400
	XFS_SB_VERSION_SECTORBIT   XfsSbVersionBit = 0x0800
	XFS_SB_VERSION_EXTFLGBIT   XfsSbVersionBit = 0x1000
	XFS_SB_VERSION_DIRV2BIT    XfsSbVersionBit = 0x2000
	XFS_SB_VERSION_BORGBIT     XfsSbVersionBit = 0x4000 // ASCII only case-insens.
	XFS_SB_VERSION_MOREBITSBIT XfsSbVersionBit = 0x8000
)

// SuperBlock 超级块
// 参考：内核源码的fs/xfs/libxfs/xfs_format.h
type SuperBlock struct {
	Magicnum   uint32   // magic number == XFS_SB_MAGIC(0x58465342即'XFSB')
	BlockSize  uint32   // logical block size, bytes
	Dblocks    uint64   // number of data blocks
	Rblocks    uint64   // number of realtime blocks
	Rextens    uint64   // number of realtime extents
	UUID       [16]byte // user-visible file system unique id
	Logstart   uint64   // starting block of log if internal
	Rootino    uint64   // root inode number
	Rbmino     uint64   // bitmap inode for realtime extents
	Rsmino     uint64   // summary inode for rt bitmap
	Rextsize   uint32   // realtime extent size, blocks
	Agblocks   uint32   // size of an allocation group
	Agcount    uint32   // number of allocation groups
	Rbblocks   uint32   // number of rt bitmap blocks
	Logblocks  uint32   // number of log blocks
	Versionnum uint16   // header version == XFS_SB_VERSION
	Sectsize   uint16   // volume sector size, bytes
	Inodesize  uint16   // inode size, bytes
	Inopblock  uint16   // inodes per block
	Fname      [12]byte // file system name
	Blocklog   uint8    // log2 of sb_blocksize
	Sectlog    uint8    // log2 of sb_sectsize
	Inodelog   uint8    // log2 of sb_inodesize
	Inopblog   uint8    // log2 of sb_inopblock
	Agblklog   uint8    // log2 of sb_agblocks (rounded up)
	Rextslog   uint8    // log2 of sb_rextents
	Inprogress uint8    // mkfs is in progress, don't mount
	ImaxPct    uint8    // max % of fs for inode space

	// statistics
	// 以下四个字段必须保持连续（对应内核注释：
	// These fields must remain contiguous. If you really want to
	// change their layout, make sure you fix the code in
	// xfs_trans_apply_sb_deltas()）
	Icount    uint64 // allocated inodes
	Ifree     uint64 // free inodes
	Fdblocks  uint64 // free data blocks
	Frextents uint64 // free realtime extents
	// End contiguous fields.

	Uqunotino   uint64 // user quota inode
	Gquotino    uint64 // group quota inode
	Qflags      uint16 // quota flags
	Flags       uint8  // misc. flags
	SharedVn    uint8  // shared version number
	Inoalignmt  uint32 // inode chunk alignment, fsblocks
	Unit        uint32 // stripe or raid unit
	Width       uint32 // stripe or raid width
	Dirblklog   uint8  // log2 of dir block size (fsbs)
	Logsectlog  uint8  // log2 of the log sector size
	Logsectsize uint16 // sector size for the log, bytes
	Logsunit    uint32 // stripe unit size for the log
	Features2   uint32 // additional feature bits

	// bad features2 field as a result of failing to pad the sb
	// structure to 64 bits. Some machines will be using this field
	// for features2 bits. Easiest just to mark it bad and not use
	// it for anything else.
	BadFeatures2 uint32

	// version 5 superblock fields start here
	// feature masks
	FeaturesCompat      uint32 // feature masks: compat
	FeaturesRoCompat    uint32 // feature masks: ro compat
	FeaturesIncompat    uint32 // feature masks: incompat
	FeaturesLogIncompat uint32 // feature masks: log incompat

	CRC        uint32 // superblock crc
	SpinoAlign uint32 // sparse inode chunk alignment

	Pquotino uint64   // project quota inode
	Lsn      int64    // last write sequence
	MetaUUID [16]byte // metadata file system unique id

	// must be padded to 64 bit alignment
}

// AG Allocation Group
// 每一组ag的第一块中都包含sb，agf，agi，agfl四个结构，每个结构占用512字节
type AG struct {
	SuperBlock SuperBlock
	Agf        AGF
	Agi        AGI
	Agfl       AGFL
}

type AGFL struct {
	Magicnum uint32
	Seqno    uint32
	UUID     [16]byte
	Lsn      uint64
	CRC      uint32
}

// AGF Allocation Group Free space information
// 参考：内核源码 fs/xfs/libxfs/xfs_format.h 中的 xfs_agf_t
type AGF struct {
	// Common allocation group header information
	Magicnum   uint32 // magic number == XFS_AGF_MAGIC
	Versionnum uint32 // header version == XFS_AGF_VERSION
	Seqno      uint32 // sequence # starting from 0
	Length     uint32 // size in blocks of a.g.

	// Freespace and rmap information
	Roots  [3]uint32 // bnobt root block, cntbt root block, rmapbt root block
	Levels [3]uint32 // bnobt btree levels, cntbt btree levels, rmapbt btree levels

	Flfirst   uint32   // first freelist block's index
	Fllast    uint32   // last freelist block's index
	Flcount   uint32   // count of blocks in freelist
	Freeblks  uint32   // total free blocks
	Longest   uint32   // longest free space
	Btreeblks uint32   // # of blocks held in AGF btrees
	UUID      [16]byte // uuid of filesystem

	RmapBlocks     uint32 // rmapbt blocks used
	RefcountBlocks uint32 // refcountbt blocks used
	RefcountRoot   uint32 // refcount tree root block
	RefcountLevel  uint32 // refcount btree levels

	// reserve some contiguous space for future logged fields before we add
	// the unlogged fields. This makes the range logging via flags and
	// structure offsets much simpler.
	Spare64 [112]byte // agf_spare64[14]

	// unlogged fields, written during buffer writeback.
	Lsn    uint64 // last write sequence
	CRC    uint32 // crc of agf sector
	Spare2 uint32

	// structure must be padded to 64 bit alignment
}

// AGI Allocation Group Inode information
// 参考：内核源码 fs/xfs/libxfs/xfs_format.h 中的 xfs_agi_t
type AGI struct {
	// Common allocation group header information
	Magicnum   uint32 // magic number == XFS_AGI_MAGIC
	Versionnum uint32 // header version == XFS_AGI_VERSION
	Seqno      uint32 // sequence # starting from 0
	Length     uint32 // size in blocks of a.g.

	// Inode information
	// Inodes are mapped by interpreting the inode number, so no
	// mapping data is needed here.
	Count     uint32 // count of allocated inodes
	Root      uint32 // root of inode btree
	Level     uint32 // levels in inode btree
	Freecount uint32 // number of free inodes
	Newino    uint32 // new inode just allocated
	Dirino    uint32 // last directory inode chunk

	// Hash table of inodes which have been unlinked but are
	// still being referenced.
	Unlinked [256]byte // agi_unlinked[XFS_AGI_UNLINKED_BUCKETS]

	// This marks the end of logging region 1 and start of logging region 2.
	UUID [16]byte // uuid of filesystem

	CRC       uint32 // crc of agi sector
	Pad32     uint32
	Lsn       uint64 // last write sequence
	FreeRoot  uint32 // root of the free inode btree
	FreeLevel uint32 // levels in free inode btree
	Iblocks   uint32 // inobt blocks used
	Fblocks   uint32 // finobt blocks used

	// structure must be padded to 64 bit alignment
}

type BtreeBlockShdr struct {
	Leftsib  uint32
	Rightsib uint32
	Blkno    uint64
	Lsn      uint64
	UUID     [16]byte
	Owner    uint32
	CRC      uint32
}

type BtreeBlockLhdr struct {
	Leftsib  uint64
	Rightsib uint64
	Blkno    uint64
	Lsn      uint64
	UUID     [16]byte
	Owner    uint64
	CRC      uint32
	Pad      uint32
}

type BtreeShortBlock struct {
	Magicnum uint32
	Level    uint16
	Numrecs  uint16
	BtreeBlockShdr
}

//
// struct xfs_btree_block {
//    __be32        bb_magic;    /* magic number for block type */
//    __be16        bb_level;    /* 0 is a leaf */
//    __be16        bb_numrecs;    /* current # of data records */
//    union {
//        struct xfs_btree_block_shdr s;
//        struct xfs_btree_block_lhdr l;
//    } bb_u;                /* rest */
// };
//

type BtreeLongBlock struct {
	Magicnum uint32
	Level    uint16
	Numrecs  uint16
	BtreeBlockLhdr
}
