package extfs

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/kisun-bit/drpkg/define"
	"github.com/kisun-bit/drpkg/disk/filesystem/bitmap"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
)

func le16(b []byte, off int) uint16 { return binary.LittleEndian.Uint16(b[off : off+2]) }
func le32(b []byte, off int) uint32 { return binary.LittleEndian.Uint32(b[off : off+4]) }

func parseSuperBlock(buf []byte) (*superBlock, error) {
	if len(buf) < superblockSize {
		return nil, fmt.Errorf("superblock buffer too small: %d", len(buf))
	}
	magic := le16(buf, 56)
	if magic != ext2Magic {
		return nil, fmt.Errorf("not an ext2/3/4 filesystem (magic=0x%x)", magic)
	}
	sb := &superBlock{
		blocksCountLo:     le32(buf, 4),
		freeBlocksCountLo: le32(buf, 12),
		firstDataBlock:    le32(buf, 20),
		logBlockSize:      le32(buf, 24),
		blocksPerGroup:    le32(buf, 32),
		magic:             magic,
		featureIncompat:   le32(buf, 96),
		featureRoCompat:   le32(buf, 100),
		descSize:          le16(buf, 254),
		blocksCountHi:     le32(buf, 336),
		freeBlocksCountHi: le32(buf, 344),
		logClusterSize:    le32(buf, 28),
		firstMetaBg:       le32(buf, 260),
		reservedGdtBlocks: le16(buf, 206),
	}
	return sb, nil
}

func parseGroupDesc(buf []byte, is64 bool) groupDesc {
	lo := le32(buf, 0)
	freeLo := le16(buf, 12)
	flags := le16(buf, 18)

	var hi uint32
	var freeHi uint16
	if is64 && len(buf) >= 64 {
		hi = le32(buf, 32)
		freeHi = le16(buf, 44)
	}

	return groupDesc{
		blockBitmap:     uint64(hi)<<32 | uint64(lo),
		freeBlocksCount: uint64(freeHi)<<16 | uint64(freeLo),
		flags:           flags,
	}
}

// ============================================================
// 位操作（对应 C 代码里的 in_use()/ext2fs_test_bit：
// 小端字节序，每字节内 bit0（LSB）对应组内第一个 block）
// ============================================================
func testBit(buf []byte, i uint64) bool {
	return buf[i>>3]&(1<<(i&7)) != 0
}

// readFull 从 r 的绝对偏移 off 处读满 buf，类似 io.ReadFull(io.NewSectionReader(...))
func readFull(r io.ReaderAt, off int64, buf []byte) error {
	n, err := r.ReadAt(buf, off)
	if err != nil && err != io.EOF {
		return err
	}
	if n != len(buf) {
		return fmt.Errorf("short read at offset %d: got %d want %d bytes", off, n, len(buf))
	}
	return nil
}

// isPowerOf 判断 n 是否是 base 的整数次幂（n=1 视为 base^0，成立）
func isPowerOf(n, base uint64) bool {
	if n == 0 {
		return false
	}
	for n%base == 0 {
		n /= base
	}
	return n == 1
}

// hasSuperblockBackup 判断某个 group 是否携带超级块副本（含 group 0 的主超级块）。
// 遵循标准 sparse_super 规则：group 0、1，以及 3/5/7 的整数次幂才有；
// 若文件系统未开启 sparse_super（非常古老、罕见的配置），则每个 group 都有。
func hasSuperblockBackup(group uint64, sparseSuper bool) bool {
	if group == 0 || group == 1 {
		return true
	}
	if !sparseSuper {
		return true
	}
	return isPowerOf(group, 3) || isPowerOf(group, 5) || isPowerOf(group, 7)
}

// gdtParams 打包 gdtLocation / groupReservedBlocks 计算所需的只读参数，避免函数签名过长
type gdtParams struct {
	firstDataBlock    uint64
	blocksPerGroup    uint64
	blockSize         uint64
	descSize          uint32
	hasMetaBg         bool
	firstMetaBg       uint64 // meta block group 编号阈值（不是 group 编号）
	sparseSuper       bool
	groupCount        uint64
	reservedGdtBlocks uint64
}

// gdtLocation 计算第 g 个 group descriptor 存放的 (block 号, block 内字节偏移)。
//
//   - 未开启 META_BG，或 g 所在的 meta block group 编号小于 first_meta_bg：
//     沿用老式布局——GDT 从 (first_data_block+1) 开始连续存放。
//   - 否则：g 所在的 meta block group 只在其覆盖范围内第一个 group 的
//     超级块（如果有）后面紧跟的那一个 block 里，存放这个 meta block group
//     覆盖的所有 group 的 descriptor（这就是 meta_bg 的"每个 meta 组恰好一个
//     block"的设计）。
func gdtLocation(g uint64, p gdtParams) (blockNum uint64, byteOffset uint32) {
	gdpb := p.blockSize / uint64(p.descSize) // 每个 block 能塞下的 descriptor 数

	if !p.hasMetaBg || g < p.firstMetaBg*gdpb {
		// 老式连续布局：从 (first_data_block+1) 这个 block 开始，
		// 按 descSize 依次排列，可能跨越多个 block。
		byteIdx := g * uint64(p.descSize)
		blockNum = p.firstDataBlock + 1 + byteIdx/p.blockSize
		byteOffset = uint32(byteIdx % p.blockSize)
		return
	}

	metaGroup := g / gdpb
	firstGroupInMeta := metaGroup * gdpb
	idxInChunk := g % gdpb

	groupStartBlock := p.firstDataBlock + firstGroupInMeta*p.blocksPerGroup
	chunkBlock := groupStartBlock
	if hasSuperblockBackup(firstGroupInMeta, p.sparseSuper) {
		chunkBlock++ // chunk 紧跟在这个 group 自己的超级块副本后面
	}

	blockNum = chunkBlock
	byteOffset = uint32(idxInChunk) * p.descSize // 值域在一个 block 内，转 uint32 安全
	return blockNum, byteOffset
}

// groupReservedBlocks 计算一个 BLOCK_UNINIT 组里，因为存放超级块/GDT 备份而
// 必然被占用的 block 数量（这些 block 属于该 group 自己的物理范围）。
//
// 背景：BLOCK_UNINIT 只表示"磁盘上没有为这个组写一份完整的位图数据"，
// 不代表这个组里所有 block 都真的空闲——如果这个组恰好携带超级块备份
// 和/或 GDT 备份，这几个 block 依然是真实被占用的。用真实镜像验证过：
// 忽略这一点会导致空闲块统计出现几个 block 量级的偏差（虽然数值很小，
// 但意味着这几个 block 会被错误地当成"空闲"从而在克隆时被跳过）。
//
//   - 老式（非 meta_bg）布局：一个携带超级块备份的 group，backup 超级块之后
//     紧跟着一份完整 GDT 的备份拷贝（占 gdtBlocksFull 个 block），
//     再之后是 s_reserved_gdt_blocks 个为未来在线扩容预留的空 block。
//   - meta_bg 布局：每个 meta block group 只有"第一个、第二个、最后一个"
//     这三个 group 各携带 1 个 block 大小的 GDT chunk 备份，不涉及
//     reserved_gdt_blocks（该机制是 meta_bg 出现的目的之一，就是替掉它）。
//
// flex_bg 下，本组自己的 inode bitmap / inode table 可能被挪到了别的 group
// 物理范围内，因此不计入这里——这里只关心"物理落在本组范围内"的保留 block。
func groupReservedBlocks(g uint64, p gdtParams) uint64 {
	gdpb := p.blockSize / uint64(p.descSize)
	inMetaBgRegion := p.hasMetaBg && g >= p.firstMetaBg*gdpb

	isChunkCarrier := func(group uint64) bool {
		if !inMetaBgRegion {
			return false
		}
		metaGroup := group / gdpb
		firstInMeta := metaGroup * gdpb
		lastInMeta := firstInMeta + gdpb - 1
		if lastInMeta > p.groupCount-1 {
			lastInMeta = p.groupCount - 1
		}
		return group == firstInMeta || group == firstInMeta+1 || group == lastInMeta
	}

	if !hasSuperblockBackup(g, p.sparseSuper) {
		if isChunkCarrier(g) {
			return 1 // 只有 GDT chunk 备份，没有超级块（比如 meta_bg 里"最后一个 group"常见的情况）
		}
		return 0
	}

	reserved := uint64(1) // 超级块自身
	if inMetaBgRegion {
		if isChunkCarrier(g) {
			reserved++ // meta_bg 的 1 个 block 大小 chunk 备份
		}
	} else {
		gdtBlocksFull := (p.groupCount*uint64(p.descSize) + p.blockSize - 1) / p.blockSize
		reserved += gdtBlocksFull + p.reservedGdtBlocks
	}
	return reserved
}

// lastBlockCache 只缓存"最近一次读到的那个 block"，用于顺序遍历 group descriptor 时
// 避免对同一个 block 重复发起读请求。因为无论是老式连续布局还是 meta_bg 的 chunk，
// 相邻的 group 大概率落在同一个 block 里，按 g 递增顺序访问时天然命中率很高，
// 不需要更复杂的多 block 缓存。
type lastBlockCache struct {
	blockNum uint64
	buf      []byte
	valid    bool
}

func (c *lastBlockCache) read(r io.ReaderAt, blockNum uint64, blockSize uint32) error {
	if c.valid && c.blockNum == blockNum {
		return nil
	}
	if c.buf == nil {
		c.buf = make([]byte, blockSize)
	}
	if err := readFull(r, int64(blockNum)*int64(blockSize), c.buf); err != nil {
		c.valid = false
		return err
	}
	c.blockNum = blockNum
	c.valid = true
	return nil
}

type BitmapParser struct {
	dev   string
	start int64
	size  int64
	fr    *extend.FsRegionReader
}

func NewBitmapParser(dev string, start int64, size int64) (bitmap.FsBitmapParser, error) {
	fr, e := extend.NewFsRegionReader(dev, start, size)
	if e != nil {
		return nil, e
	}
	return &BitmapParser{dev: dev, start: start, size: size, fr: fr}, nil
}

func (p *BitmapParser) String() string {
	return fmt.Sprintf("<EXTFSBitmapParser(dev=%s,start=%d,size=%d)>",
		p.dev, p.start, p.size)
}

func (p *BitmapParser) Dump() (*bitmap.FsBitmap, error) {
	defer func() {
		if p.fr != nil {
			_ = p.fr.Close()
		}
	}()

	// 1. 读超级块（对应 C: fs_open -> ext2fs_open 内部读 superblock）
	sbBuf := make([]byte, superblockSize)
	if err := readFull(p.fr, superblockOffset, sbBuf); err != nil {
		return nil, fmt.Errorf("read superblock failed: %w", err)
	}
	sb, err := parseSuperBlock(sbBuf)
	if err != nil {
		return nil, err
	}

	is64 := sb.featureIncompat&featureIncompat64Bit != 0
	blockSize := uint32(1024) << sb.logBlockSize
	if blockSize == 0 || sb.blocksPerGroup == 0 {
		return nil, errors.New("invalid superblock: block_size or blocks_per_group is zero")
	}

	totalBlocks := uint64(sb.blocksCountLo)
	totalFreeFromSB := uint64(sb.freeBlocksCountLo)
	if is64 {
		totalBlocks |= uint64(sb.blocksCountHi) << 32
		totalFreeFromSB |= uint64(sb.freeBlocksCountHi) << 32
	}

	descSize := uint32(32)
	if is64 && uint32(sb.descSize) >= 64 {
		descSize = uint32(sb.descSize)
	}

	firstDataBlock := uint64(sb.firstDataBlock)
	blocksPerGroup := uint64(sb.blocksPerGroup)
	groupCount := (totalBlocks - firstDataBlock + blocksPerGroup - 1) / blocksPerGroup

	// bigalloc: 位图粒度从"block"变成"cluster"，一个 cluster 由 2^clusterRatioBits 个
	// 连续 block 组成，它们的已用/空闲状态永远一致（这是 bigalloc 的分配粒度决定的）。
	// 未开启该特性时 clusterRatioBits=0，blocksPerCluster=1，等价于逐 block 处理，
	// 不需要为普通文件系统写两套代码路径。
	isBigalloc := sb.featureRoCompat&featureRoCompatBigalloc != 0
	var clusterRatioBits uint
	if isBigalloc {
		clusterRatioBits = uint(sb.logClusterSize - sb.logBlockSize)
	}
	blocksPerCluster := uint64(1) << clusterRatioBits

	// 防御性校验：s_first_data_block 的取值规则是——
	// block_size == 1024 时，block 0 整块被引导扇区占用，超级块落在 block 1，
	//   所以 first_data_block 应为 1；
	// block_size >= 2048 时，引导扇区和超级块同属 block 0，
	//   所以 first_data_block 应为 0。
	// 这个值本身是 mkfs 写死在磁盘上的，我们从不自己推导，只在这里做个健全性检查，
	// 用来及早发现"超级块损坏 / 非标准镜像"这类情况。
	expectedFDB := uint64(0)
	if blockSize == 1024 {
		expectedFDB = 1
	}
	if firstDataBlock != expectedFDB {
		logger.Warnf("%s.Dump(): unexpected s_first_data_block=%d for block_size=%d (expected %d), "+
			"superblock may be corrupted or from a non-standard image", p, firstDataBlock, blockSize, expectedFDB)
	}

	// 2. group descriptor 的定位参数。
	// 未开启 META_BG 时 hasMetaBg=false，gdtLocation 会退化成老式的
	// "从 (first_data_block+1) 开始连续存放"，行为和之前完全一致。
	hasMetaBg := sb.featureIncompat&featureIncompatMetaBg != 0
	sparseSuper := sb.featureRoCompat&featureRoCompatSparseSuper != 0
	gp := gdtParams{
		firstDataBlock:    firstDataBlock,
		blocksPerGroup:    blocksPerGroup,
		blockSize:         uint64(blockSize),
		descSize:          descSize,
		hasMetaBg:         hasMetaBg,
		firstMetaBg:       uint64(sb.firstMetaBg),
		sparseSuper:       sparseSuper,
		groupCount:        groupCount,
		reservedGdtBlocks: uint64(sb.reservedGdtBlocks),
	}
	var gdtCache lastBlockCache

	// 3. 创建结果位图，并默认全部标记为"已用"（保守策略，与 partclone 的
	//    pc_init_bitmap(bitmap, 0xFF, ...) 完全一致），后面再把确认空闲的位清零。
	//
	//    注意：这个默认值不仅是"保守兜底"，它还承担了一个具体职责——
	//    当 block_size == 1024 时 first_data_block == 1，下面的 group 循环
	//    永远从 startBlock = firstDataBlock 开始，index 0（对应磁盘上被
	//    引导扇区占用的 block 0）根本不会被循环访问到。它最终能正确地
	//    体现为"已用"，靠的就是这里的默认初始化，而不是某个显式的
	//    "标记 block 0 已用"的分支。如果以后要改这个初始化逻辑，
	//    必须同时想清楚 block 0 的状态从哪里来。
	//
	fsType := define.FsTypeExtFs
	kind := bitmap.BitmapFromFS
	fsBitmap := bitmap.NewFsBitmap(fsType, kind, int64(totalBlocks), int(blockSize))
	fsBitmap.SetAll()

	blockBitmapBuf := make([]byte, blockSize)
	var totalFreeCounted uint64

	// 4. 逐个 block group 处理（对应 C 代码里的 for (group = 0; group < group_desc_count; group++)）
	for g := uint64(0); g < groupCount; g++ {
		blockNum, byteOffset := gdtLocation(g, gp)
		if err := gdtCache.read(p.fr, blockNum, blockSize); err != nil {
			return nil, fmt.Errorf("read group descriptor of group %d (block %d): %w", g, blockNum, err)
		}
		gd := parseGroupDesc(gdtCache.buf[byteOffset:], is64)

		startBlock := firstDataBlock + g*blocksPerGroup
		blocksInGroup := blocksPerGroup
		if startBlock+blocksInGroup > totalBlocks {
			blocksInGroup = totalBlocks - startBlock
		}

		if gd.flags&bgFlagBlockUninit != 0 {
			// 对应 C 代码里的 B_UN_INIT 分支：该组从未真正分配过数据 block，
			// 磁盘上没有为它写一份完整的位图数据。但注意——这不等于整组都空闲：
			// 如果这个组恰好携带超级块备份和/或 GDT 备份，这几个 block 依然是
			// 真实占用的（用真实镜像验证过这一点，见函数注释）。
			reserved := groupReservedBlocks(g, gp)
			fsBitmap.ClearRange(startBlock, uint32(blocksInGroup))
			if reserved > 0 {
				// 把开头那几个属于超级块/GDT备份的 block 重新标记回"已用"
				fsBitmap.SetRange(startBlock, uint32(reserved))
			}
			gfree := blocksInGroup - reserved
			totalFreeCounted += gfree

			if !isBigalloc && gfree != gd.freeBlocksCount {
				logger.Warnf("%s.Dump(): group %d (BLOCK_UNINIT) free blocks mismatch: "+
					"counted=%d meta=%d (reserved=%d)", p, g, gfree, gd.freeBlocksCount, reserved)
			}
			continue
		}

		// 读取该组自己的 block/cluster bitmap（位于 gd.blockBitmap 指向的那个 block）
		bmOffset := int64(gd.blockBitmap) * int64(blockSize)
		if err := readFull(p.fr, bmOffset, blockBitmapBuf); err != nil {
			return nil, fmt.Errorf("read block bitmap of group %d failed: %w", g, err)
		}

		var gfree uint64
		if isBigalloc && clusterRatioBits > 0 {
			// bigalloc: 位图里第 c 位对应的是一个 cluster，
			// 覆盖 [startBlock+c*blocksPerCluster, +blocksPerCluster) 这一整段 block，
			// 它们的状态永远一致，所以按 cluster 为单位批量 ClearRange，
			// 既比逐 block 判断更快，也更直接地体现"这段 block 是同一个分配单元"。
			clustersInGroup := (blocksInGroup + blocksPerCluster - 1) / blocksPerCluster
			for c := uint64(0); c < clustersInGroup; c++ {
				clusterStart := startBlock + c*blocksPerCluster
				clusterLen := blocksPerCluster
				if clusterStart+clusterLen > startBlock+blocksInGroup {
					clusterLen = startBlock + blocksInGroup - clusterStart
				}
				if !testBit(blockBitmapBuf, c) {
					// 空闲 cluster：整段 block 一起清零
					fsBitmap.ClearRange(clusterStart, uint32(clusterLen))
					gfree += clusterLen
				}
				// 已用 cluster：默认值（SetAll）已经是已用，无需处理
			}
		} else {
			for i := uint64(0); i < blocksInGroup; i++ {
				global := startBlock + i
				if !testBit(blockBitmapBuf, i) {
					fsBitmap.Clear(global) // 空闲
					gfree++
				}
				// 已用: 默认值已经是已用，无需处理
			}
		}
		totalFreeCounted += gfree

		// 组内校验（对应 C 代码里 gfree != bg_free_blocks_count 的检查）。
		// bigalloc 下 group 描述符里这个字段的统计口径（block 还是 cluster）
		// 在不同版本 e2fsprogs 里不完全一致，因此和原始 C 代码一样跳过这项校验。
		if !isBigalloc && gfree != gd.freeBlocksCount {
			// 这里选择只警告不中断；如果你想严格复现 C 版本"直接报错退出"的行为，
			// 把下面这行换成 return nil, fmt.Errorf(...)
			logger.Warnf("%s.Dump(): group %d free blocks mismatch: counted=%d meta=%d",
				p, g, gfree, gd.freeBlocksCount)
		}
	}

	// 5. 全局校验（对应 C 代码里 lfree != ext2fs_free_blocks_count(fs->super)）。
	// bigalloc 同样跳过严格比对，只给出提示信息。
	if isBigalloc {
		logger.Debugf("%s.Dump(): bigalloc filesystem (cluster_ratio_bits=%d, blocks_per_cluster=%d), "+
			"skip strict free-block validation, counted %d free blocks",
			p, clusterRatioBits, blocksPerCluster, totalFreeCounted)
	} else if totalFreeCounted != totalFreeFromSB {
		logger.Warnf("%s.Dump(): total free blocks mismatch: counted=%d superblock=%d "+
			"(filesystem may not have been fsck'd, or may use a feature not handled here, such as metadata_csum)",
			p, totalFreeCounted, totalFreeFromSB)
	}

	return fsBitmap, nil
}
