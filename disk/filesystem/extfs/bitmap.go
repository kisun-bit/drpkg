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
// Bit manipulation (equivalent to in_use()/ext2fs_test_bit in the C code:
// little-endian; within each byte, bit 0 (LSB) corresponds to the first
// block in the group).
// ============================================================
func testBit(buf []byte, i uint64) bool {
	return buf[i>>3]&(1<<(i&7)) != 0
}

// readFull reads a full buf from the absolute offset off of r, similar to
// io.ReadFull(io.NewSectionReader(...)).
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

// isPowerOf reports whether n is an integer power of base (n=1 counts as
// base^0 and is considered true).
func isPowerOf(n, base uint64) bool {
	if n == 0 {
		return false
	}
	for n%base == 0 {
		n /= base
	}
	return n == 1
}

// hasSuperblockBackup reports whether a given group carries a superblock
// backup (group 0's copy is the primary superblock itself).
// Follows the standard sparse_super rule: groups 0, 1, and any group whose
// number is an integer power of 3, 5, or 7 have a backup; if the filesystem
// does not have sparse_super enabled (a very old, rare configuration), then
// every group has one.
func hasSuperblockBackup(group uint64, sparseSuper bool) bool {
	if group == 0 || group == 1 {
		return true
	}
	if !sparseSuper {
		return true
	}
	return isPowerOf(group, 3) || isPowerOf(group, 5) || isPowerOf(group, 7)
}

// gdtParams bundles the read-only parameters needed by gdtLocation /
// groupReservedBlocks, to keep function signatures manageable.
type gdtParams struct {
	firstDataBlock    uint64
	blocksPerGroup    uint64
	blockSize         uint64
	descSize          uint32
	hasMetaBg         bool
	firstMetaBg       uint64 // meta block group number threshold (not a group number)
	sparseSuper       bool
	groupCount        uint64
	reservedGdtBlocks uint64
}

// gdtLocation computes the (block number, byte offset within that block)
// where the g-th group descriptor is stored.
//
//   - If META_BG is not enabled, or g's meta block group number is below
//     first_meta_bg: fall back to the classic layout — the GDT is stored
//     contiguously starting at (first_data_block+1).
//   - Otherwise: the meta block group that g belongs to stores the
//     descriptors for every group it covers in a single block, located
//     right after the superblock backup (if any) of the first group in
//     that meta block group. This is the defining trait of meta_bg — each
//     meta group occupies exactly one block for its GDT chunk.
func gdtLocation(g uint64, p gdtParams) (blockNum uint64, byteOffset uint32) {
	gdpb := p.blockSize / uint64(p.descSize) // number of descriptors that fit in one block

	if !p.hasMetaBg || g < p.firstMetaBg*gdpb {
		// Classic contiguous layout: starting at block (first_data_block+1),
		// descriptors are laid out back-to-back by descSize, possibly
		// spanning multiple blocks.
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
		chunkBlock++ // the chunk immediately follows this group's own superblock backup
	}

	blockNum = chunkBlock
	byteOffset = uint32(idxInChunk) * p.descSize // always within one block, safe to cast to uint32
	return blockNum, byteOffset
}

// groupReservedBlocks computes, for a BLOCK_UNINIT group, the number of
// blocks that are necessarily in use because they hold a superblock and/or
// GDT backup copy (these blocks fall within the group's own physical
// range).
//
// Background: BLOCK_UNINIT only means "no full bitmap has been written to
// disk for this group" — it does NOT mean every block in the group is
// actually free. If the group happens to carry a superblock backup and/or
// a GDT backup, those blocks are still genuinely in use. Verified against
// real images: ignoring this causes the free-block count to be off by a
// handful of blocks (small in absolute terms, but it means those blocks
// would be wrongly treated as "free" and skipped during cloning).
//
//   - Classic (non-meta_bg) layout: a group carrying a superblock backup is
//     followed by a full copy of the GDT backup (gdtBlocksFull blocks),
//     followed by s_reserved_gdt_blocks empty blocks reserved for future
//     online resizing.
//   - meta_bg layout: within each meta block group, only the first, second,
//     and last groups each carry a 1-block GDT chunk backup; reserved_gdt_blocks
//     doesn't apply here (replacing that mechanism is one of the reasons
//     meta_bg exists).
//
// Under flex_bg, a group's own inode bitmap / inode table may have been
// relocated into another group's physical range, so it is not counted here
// — this function only cares about reserved blocks that physically fall
// within this group's own range.
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
			return 1 // only a GDT chunk backup, no superblock (common e.g. for the "last group" in a meta_bg)
		}
		return 0
	}

	reserved := uint64(1) // the superblock itself
	if inMetaBgRegion {
		if isChunkCarrier(g) {
			reserved++ // meta_bg's 1-block-sized chunk backup
		}
	} else {
		gdtBlocksFull := (p.groupCount*uint64(p.descSize) + p.blockSize - 1) / p.blockSize
		reserved += gdtBlocksFull + p.reservedGdtBlocks
	}
	return reserved
}

// lastBlockCache caches only the most recently read block, used while
// sequentially walking group descriptors to avoid re-reading the same
// block repeatedly. Whether using the classic contiguous layout or a
// meta_bg chunk, adjacent groups are very likely to land in the same
// block, so accessing groups in increasing order of g naturally gets a
// high hit rate — a more elaborate multi-block cache isn't needed.
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

	// 1. Read the superblock (equivalent to fs_open -> ext2fs_open reading
	//    the superblock internally in C).
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

	// bigalloc: the bitmap's granularity shifts from "block" to "cluster" —
	// a cluster is made up of 2^clusterRatioBits consecutive blocks whose
	// used/free status is always identical (this is inherent to bigalloc's
	// allocation granularity). When the feature is off, clusterRatioBits=0
	// and blocksPerCluster=1, which is equivalent to per-block processing,
	// so there's no need for a separate code path for ordinary filesystems.
	isBigalloc := sb.featureRoCompat&featureRoCompatBigalloc != 0
	var clusterRatioBits uint
	if isBigalloc {
		clusterRatioBits = uint(sb.logClusterSize - sb.logBlockSize)
	}
	blocksPerCluster := uint64(1) << clusterRatioBits

	// Defensive sanity check: the rule for s_first_data_block is —
	// when block_size == 1024, block 0 is entirely occupied by the boot
	//   sector, so the superblock lives in block 1 and first_data_block
	//   should be 1;
	// when block_size >= 2048, the boot sector and superblock share block
	//   0, so first_data_block should be 0.
	// This value is written to disk by mkfs and we never derive it
	// ourselves — this check exists purely to catch a corrupted superblock
	// or a non-standard image early.
	expectedFDB := uint64(0)
	if blockSize == 1024 {
		expectedFDB = 1
	}
	if firstDataBlock != expectedFDB {
		logger.Warnf("%s.Dump(): unexpected s_first_data_block=%d for block_size=%d (expected %d), "+
			"superblock may be corrupted or from a non-standard image", p, firstDataBlock, blockSize, expectedFDB)
	}

	// 2. Parameters for locating group descriptors.
	// When META_BG is not enabled, hasMetaBg=false and gdtLocation falls
	// back to the classic "contiguous starting at (first_data_block+1)"
	// layout, behaving exactly as before.
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

	// 3. Create the result bitmap, defaulting every bit to "used" (a
	//    conservative choice, exactly matching partclone's
	//    pc_init_bitmap(bitmap, 0xFF, ...)); confirmed-free bits are
	//    cleared afterward.
	//
	//    Note: this default isn't just a conservative fallback — it also
	//    carries a specific responsibility. When block_size == 1024,
	//    first_data_block == 1, and the group loop below always starts
	//    at startBlock = firstDataBlock, so index 0 (the block occupied
	//    by the boot sector on disk) is never visited by the loop at all.
	//    The fact that it correctly ends up marked "used" relies entirely
	//    on this default initialization, not on any explicit "mark block
	//    0 as used" branch. If this initialization logic is ever changed,
	//    make sure block 0's status is still accounted for somewhere.
	//
	fsType := define.FsTypeExtFs
	kind := bitmap.BitmapFromFS
	fsBitmap := bitmap.NewFsBitmap(fsType, kind, int64(totalBlocks), int(blockSize))
	fsBitmap.SetAll()

	blockBitmapBuf := make([]byte, blockSize)
	var totalFreeCounted uint64

	// 4. Process each block group (equivalent to
	//    for (group = 0; group < group_desc_count; group++) in the C code).
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
			// Equivalent to the BLOCK_UNINIT branch in the C code: this
			// group has never actually had data blocks allocated, so no
			// full bitmap was ever written to disk for it. But note this
			// does NOT mean the entire group is free — if it happens to
			// carry a superblock backup and/or a GDT backup, those blocks
			// are still genuinely in use (verified against real images,
			// see the function comment above).
			reserved := groupReservedBlocks(g, gp)
			fsBitmap.ClearRange(startBlock, uint32(blocksInGroup))
			if reserved > 0 {
				// Re-mark the leading blocks that hold the superblock/GDT
				// backup as "used".
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

		// Read this group's own block/cluster bitmap (located at the block
		// pointed to by gd.blockBitmap).
		bmOffset := int64(gd.blockBitmap) * int64(blockSize)
		if err := readFull(p.fr, bmOffset, blockBitmapBuf); err != nil {
			return nil, fmt.Errorf("read block bitmap of group %d failed: %w", g, err)
		}

		var gfree uint64
		if isBigalloc && clusterRatioBits > 0 {
			// bigalloc: bit c in the bitmap represents one cluster, which
			// covers the whole range [startBlock+c*blocksPerCluster,
			// +blocksPerCluster) — those blocks always share the same
			// status, so ClearRange is applied per cluster in bulk. This
			// is both faster than a per-block check and more directly
			// reflects that this range is a single allocation unit.
			clustersInGroup := (blocksInGroup + blocksPerCluster - 1) / blocksPerCluster
			for c := uint64(0); c < clustersInGroup; c++ {
				clusterStart := startBlock + c*blocksPerCluster
				clusterLen := blocksPerCluster
				if clusterStart+clusterLen > startBlock+blocksInGroup {
					clusterLen = startBlock + blocksInGroup - clusterStart
				}
				if !testBit(blockBitmapBuf, c) {
					// free cluster: clear the whole range at once
					fsBitmap.ClearRange(clusterStart, uint32(clusterLen))
					gfree += clusterLen
				}
				// used cluster: already "used" by default (SetAll), nothing to do
			}
		} else {
			for i := uint64(0); i < blocksInGroup; i++ {
				global := startBlock + i
				if !testBit(blockBitmapBuf, i) {
					fsBitmap.Clear(global) // free
					gfree++
				}
				// used: already "used" by default, nothing to do
			}
		}
		totalFreeCounted += gfree

		// Per-group validation (equivalent to the gfree != bg_free_blocks_count
		// check in the C code). Under bigalloc, different e2fsprogs versions
		// disagree on whether this group-descriptor field counts blocks or
		// clusters, so this check is skipped here just as in the original C code.
		if !isBigalloc && gfree != gd.freeBlocksCount {
			// Only a warning is emitted here; to strictly reproduce the C
			// version's "abort with an error" behavior, replace this with
			// return nil, fmt.Errorf(...).
			logger.Warnf("%s.Dump(): group %d free blocks mismatch: counted=%d meta=%d",
				p, g, gfree, gd.freeBlocksCount)
		}
	}

	// 5. Global validation (equivalent to lfree != ext2fs_free_blocks_count(fs->super)
	//    in the C code). Also skipped for bigalloc, informational only.
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
