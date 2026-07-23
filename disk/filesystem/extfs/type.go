package extfs

// ============================================================
// 常量：来自 ext2/3/4 磁盘格式（对应 e2fsprogs 的 ext2_fs.h）
// ============================================================
const (
	superblockOffset = 1024 // 超级块固定位于设备偏移 1024 字节处
	superblockSize   = 1024

	ext2Magic = 0xEF53 // s_magic

	// s_feature_incompat 位
	featureIncompat64Bit = 0x0080 // EXT4_FEATURE_INCOMPAT_64BIT

	// bg_flags 位（group descriptor flags）
	bgFlagBlockUninit = 0x0002 // EXT2_BG_BLOCK_UNINIT：该组从未分配过，磁盘上无有效位图

	// s_feature_ro_compat 位
	featureRoCompatBigalloc    = 0x0200 // EXT4_FEATURE_RO_COMPAT_BIGALLOC
	featureRoCompatSparseSuper = 0x0001 // EXT2_FEATURE_RO_COMPAT_SPARSE_SUPER

	// s_feature_incompat 位（续）
	featureIncompatMetaBg = 0x0010 // EXT2_FEATURE_INCOMPAT_META_BG
)

// ============================================================
// 超级块（只解析我们需要的字段，偏移量与 ext2_fs.h 完全对应）
// ============================================================
type superBlock struct {
	blocksCountLo     uint32
	freeBlocksCountLo uint32
	firstDataBlock    uint32
	logBlockSize      uint32
	blocksPerGroup    uint32
	magic             uint16
	descSize          uint16
	featureIncompat   uint32
	featureRoCompat   uint32

	// 64bit 特性下才有意义
	blocksCountHi     uint32
	freeBlocksCountHi uint32

	// bigalloc 特性下才有意义（RO_COMPAT_BIGALLOC 未开启时，
	// logClusterSize 恒等于 logBlockSize，cluster 退化成等于 block）
	logClusterSize uint32 // s_log_cluster_size，与旧版 s_log_frag_size 复用同一偏移

	// META_BG 特性下才有意义：编号小于这个值的 meta block group 仍用老式
	// 连续布局存 GDT，从这个编号开始才采用 meta_bg 的分散存储方式
	firstMetaBg uint32 // s_first_meta_bg

	// 老式（非 meta_bg）布局下，每个携带超级块备份的 group 在 GDT 备份之后
	// 额外预留的、供未来在线扩容使用的空 block 数
	reservedGdtBlocks uint16 // s_reserved_gdt_blocks
}

// ============================================================
// Group Descriptor（32字节标准 / 64字节 64bit 扩展）
// ============================================================
type groupDesc struct {
	blockBitmap     uint64
	freeBlocksCount uint64
	flags           uint16
}
