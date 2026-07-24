package btrfs

import (
	"encoding/binary"
	"fmt"
	"os"
	"sort"

	"github.com/kisun-bit/drpkg/define"
	"github.com/kisun-bit/drpkg/disk/filesystem/bitmap"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
)

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
	return fmt.Sprintf("<BTRFSBitmapParser(dev=%s,start=%d,size=%d)>",
		p.dev, p.start, p.size)
}

// ---------------- Debug logging ----------------

var debugEnabled = os.Getenv("BTRFS_BITMAP_DEBUG") != ""

func dbg(format string, args ...interface{}) {
	if debugEnabled {
		logger.Debugf("[btrfs-bitmap] "+format, args...)
	}
}

func hexdump(b []byte, max int) string {
	if len(b) > max {
		b = b[:max]
	}
	return fmt.Sprintf("% x", b)
}

type diskKey struct {
	Objectid uint64
	Type     uint8
	Offset   uint64
}

func parseDiskKey(b []byte) diskKey {
	return diskKey{
		Objectid: le64(b[0:8]),
		Type:     b[8],
		Offset:   le64(b[9:17]),
	}
}

func le64(b []byte) uint64 { return binary.LittleEndian.Uint64(b) }
func le32(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }
func le16(b []byte) uint16 { return binary.LittleEndian.Uint16(b) }

// ---------------- Reading ----------------

func (p *BitmapParser) readAt(off int64, n int) ([]byte, error) {
	buf := make([]byte, n)
	if _, err := p.fr.ReadAt(buf, off); err != nil {
		return nil, err
	}
	return buf, nil
}

// ---------------- superblock ----------------

type superblock struct {
	Root          uint64
	ChunkRoot     uint64
	TotalBytes    uint64
	SectorSize    uint32
	NodeSize      uint32
	SysChunkArray []byte
}

func (p *BitmapParser) readSuperblock() (*superblock, error) {
	raw, err := p.readAt(btrfsSuperInfoOffset, 4096)
	if err != nil {
		return nil, fmt.Errorf("read superblock: %w", err)
	}
	dbg("RAW first 128 bytes @ offset 0x%x: %s", btrfsSuperInfoOffset, hexdump(raw, 128))
	dbg("magic area raw[64:72] = %s (%q)", hexdump(raw[64:72], 8), string(raw[64:72]))
	if len(raw) < sbOffSysChunkArray || string(raw[sbOffMagic:sbOffMagic+8]) != btrfsSuperMagic {
		return nil, fmt.Errorf("not a valid btrfs superblock, magic=%q (want %q)",
			raw[sbOffMagic:sbOffMagic+8], btrfsSuperMagic)
	}
	sb := &superblock{
		Root:       le64(raw[sbOffRoot:]),
		ChunkRoot:  le64(raw[sbOffChunkRoot:]),
		TotalBytes: le64(raw[sbOffTotalBytes:]),
		SectorSize: le32(raw[sbOffSectorSize:]),
		NodeSize:   le32(raw[sbOffNodeSize:]),
	}
	dbg("sb.Root=%d sb.ChunkRoot=%d sb.TotalBytes=%d sb.SectorSize=%d sb.NodeSize=%d",
		sb.Root, sb.ChunkRoot, sb.TotalBytes, sb.SectorSize, sb.NodeSize)

	if sb.NodeSize == 0 || sb.TotalBytes == 0 || sb.NodeSize > 65536 || sb.NodeSize%4096 != 0 {
		return nil, fmt.Errorf("suspicious superblock values (nodesize=%d total=%d), offsets likely wrong",
			sb.NodeSize, sb.TotalBytes)
	}
	arrSize := le32(raw[sbOffSysChunkArraySize:])
	dbg("sys_chunk_array_size = %d (raw bytes at offset: %s)", arrSize,
		hexdump(raw[sbOffSysChunkArraySize:sbOffSysChunkArraySize+4], 4))
	if arrSize > btrfsSystemChunkArraySize {
		dbg("WARNING: arrSize %d exceeds max %d, clamping — this usually means the offset is WRONG", arrSize, btrfsSystemChunkArraySize)
		arrSize = btrfsSystemChunkArraySize
	}
	end := sbOffSysChunkArray + int(arrSize)
	if end > len(raw) {
		return nil, fmt.Errorf("sys_chunk_array overruns superblock buffer (end=%d len=%d)", end, len(raw))
	}
	sb.SysChunkArray = append([]byte(nil), raw[sbOffSysChunkArray:end]...)
	dbg("sys_chunk_array first 64 bytes: %s", hexdump(sb.SysChunkArray, 64))
	return sb, nil
}

func sbMirrorOffset(mirror int) int64 {
	if mirror == 0 {
		return btrfsSuperInfoOffset
	}
	return int64(16*1024) << uint(12*mirror) // mirror1: 64MiB, mirror2: 256GiB
}

// ---------------- chunk map ----------------

type stripe struct {
	Physical uint64
}

type chunkEntry struct {
	Logical uint64
	Length  uint64
	Type    uint64
	Stripes []stripe
}

type chunkMap struct {
	entries []chunkEntry
}

func (m *chunkMap) add(e chunkEntry) { m.entries = append(m.entries, e) }

func (m *chunkMap) sortAndDedup() {
	sort.Slice(m.entries, func(i, j int) bool { return m.entries[i].Logical < m.entries[j].Logical })
	// Deduplicate: chunk-tree traversal may encounter the same chunk twice
	// (e.g. once from sys_chunk_array and once from the chunk tree itself).
	// The later occurrence (usually from the chunk tree, which is more
	// authoritative) overwrites the earlier one.
	out := m.entries[:0]
	seen := map[uint64]int{}
	for _, e := range m.entries {
		if idx, ok := seen[e.Logical]; ok {
			out[idx] = e
			continue
		}
		seen[e.Logical] = len(out)
		out = append(out, e)
	}
	m.entries = out
	sort.Slice(m.entries, func(i, j int) bool { return m.entries[i].Logical < m.entries[j].Logical })
}

func (m *chunkMap) find(logical uint64) *chunkEntry {
	idx := sort.Search(len(m.entries), func(i int) bool {
		return m.entries[i].Logical+m.entries[i].Length > logical
	})
	if idx < len(m.entries) && m.entries[idx].Logical <= logical {
		return &m.entries[idx]
	}
	return nil
}

// parseChunkItem parses a single CHUNK_ITEM. logical must be key.Offset,
// NOT key.Objectid!
func parseChunkItem(b []byte, logical uint64) (chunkEntry, bool) {
	if len(b) < btrfsChunkHeaderSize {
		return chunkEntry{}, false
	}
	length := le64(b[0:8])
	typ := le64(b[24:32])
	numStripes := le16(b[44:46])
	need := btrfsChunkHeaderSize + int(numStripes)*btrfsStripeSize
	if len(b) < need || numStripes == 0 {
		return chunkEntry{}, false
	}
	e := chunkEntry{Logical: logical, Length: length, Type: typ}
	off := btrfsChunkHeaderSize
	for i := 0; i < int(numStripes); i++ {
		sb := b[off : off+btrfsStripeSize]
		e.Stripes = append(e.Stripes, stripe{Physical: le64(sb[8:16])})
		off += btrfsStripeSize
	}
	return e, true
}

func parseSysChunkArray(raw []byte) *chunkMap {
	cm := &chunkMap{}
	off := 0
	count := 0
	for off+btrfsDiskKeySize <= len(raw) {
		key := parseDiskKey(raw[off : off+btrfsDiskKeySize])
		dbg("sys_chunk_array[%d] key: objectid=%d type=%d offset=%d", count, key.Objectid, key.Type, key.Offset)
		off += btrfsDiskKeySize
		if key.Type != keyChunkItem {
			dbg("  -> not a CHUNK_ITEM (type=%d, want %d), stopping parse", key.Type, keyChunkItem)
			break
		}
		if off+btrfsChunkHeaderSize > len(raw) {
			dbg("  -> not enough bytes left for chunk header, stopping")
			break
		}
		if e, ok := parseChunkItem(raw[off:], key.Offset); ok {
			dbg("  -> chunk logical=%d length=%d type=0x%x stripes=%d physical[0]=%d",
				e.Logical, e.Length, e.Type, len(e.Stripes), e.Stripes[0].Physical)
			cm.add(e)
			numStripes := le16(raw[off+44 : off+46])
			off += btrfsChunkHeaderSize + int(numStripes)*btrfsStripeSize
		} else {
			dbg("  -> parseChunkItem FAILED")
			break
		}
		count++
	}
	cm.sortAndDedup()
	dbg("parseSysChunkArray done: %d chunks parsed", len(cm.entries))
	return cm
}

// ---------------- Marking ----------------

func (p *BitmapParser) markPhysical(bm *bitmap.FsBitmap, blockSize uint32, physical, length uint64) {
	if length == 0 || int64(physical) >= p.size {
		return
	}
	if physical+length > uint64(p.size) {
		length = uint64(p.size) - physical
	}
	start := physical / uint64(blockSize)
	end := (physical + length) / uint64(blockSize)
	if (physical+length)%uint64(blockSize) != 0 {
		end++
	}
	for b := start; b < end; b++ {
		bm.Set(b)
	}
}

func (p *BitmapParser) markLogicalRange(bm *bitmap.FsBitmap, cm *chunkMap, blockSize uint32, logical, length uint64) {
	remain := length
	cur := logical
	for remain > 0 {
		ce := cm.find(cur)
		if ce == nil {
			dbg("markLogicalRange: NO chunk mapping for logical=%d (requested range [%d,%d)), giving up on the rest",
				cur, logical, logical+length)
			return
		}
		avail := ce.Logical + ce.Length - cur
		segLen := remain
		if segLen > avail {
			segLen = avail
		}
		offInChunk := cur - ce.Logical

		if ce.Type&(bgDup|bgRaid1|bgRaid10) != 0 {
			for _, s := range ce.Stripes {
				p.markPhysical(bm, blockSize, s.Physical+offInChunk, segLen)
			}
		} else if len(ce.Stripes) > 0 {
			p.markPhysical(bm, blockSize, ce.Stripes[0].Physical+offInChunk, segLen)
		}

		cur += segLen
		remain -= segLen
		if avail == 0 {
			dbg("markLogicalRange: avail=0 at cur=%d, breaking to avoid an infinite loop", cur)
			break
		}
	}
}

// ---------------- B-tree walker ----------------

type walker struct {
	p        *BitmapParser
	bm       *bitmap.FsBitmap
	cm       *chunkMap
	nodeSize uint32
	visited  map[uint64]bool // guards against cycles in the tree causing an infinite loop
	// (shouldn't happen on a healthy image, but protects against corrupt data)
}

func newWalker(p *BitmapParser, bm *bitmap.FsBitmap, cm *chunkMap, nodeSize uint32) *walker {
	return &walker{p: p, bm: bm, cm: cm, nodeSize: nodeSize, visited: map[uint64]bool{}}
}

func (w *walker) readTreeBlock(logical uint64) ([]byte, error) {
	ce := w.cm.find(logical)
	if ce == nil || len(ce.Stripes) == 0 {
		return nil, fmt.Errorf("no chunk mapping for logical %d (chunk map has %d entries)", logical, len(w.cm.entries))
	}
	off := logical - ce.Logical
	physical := ce.Stripes[0].Physical + off
	buf, err := w.p.readAt(int64(physical), int(w.nodeSize))
	if err != nil {
		dbg("readTreeBlock: readAt(physical=%d, size=%d) failed: %v", physical, w.nodeSize, err)
	}
	return buf, err
}

func (w *walker) walkTree(logical uint64) {
	if logical == 0 {
		dbg("walkTree called with logical=0, skip")
		return
	}
	if w.visited[logical] {
		return
	}
	w.visited[logical] = true

	w.p.markLogicalRange(w.bm, w.cm, w.nodeSize, logical, uint64(w.nodeSize))

	buf, err := w.readTreeBlock(logical)
	if err != nil {
		dbg("walkTree: readTreeBlock(%d) FAILED: %v", logical, err)
		return
	}
	if len(buf) < btrfsHeaderSize {
		dbg("walkTree: block %d too short (%d bytes)", logical, len(buf))
		return
	}
	level := buf[btrfsHeaderSize-1]
	nritems := le32(buf[btrfsHeaderSize-5 : btrfsHeaderSize-1])
	maxItems := (w.nodeSize - btrfsHeaderSize) / btrfsItemSize
	dbg("walkTree: block=%d level=%d nritems=%d maxItems=%d", logical, level, nritems, maxItems)
	if nritems > maxItems {
		dbg("walkTree: nritems %d > maxItems %d, ABORTING this block (corrupt data or wrong offsets)", nritems, maxItems)
		return
	}

	if level == 0 {
		rootItemCount := 0
		extentCount := 0
		fileExtentCount := 0
		for i := uint32(0); i < nritems; i++ {
			itemOff := btrfsHeaderSize + int(i)*btrfsItemSize
			if itemOff+btrfsItemSize > len(buf) {
				break
			}
			key := parseDiskKey(buf[itemOff : itemOff+17])
			dataOff := le32(buf[itemOff+17 : itemOff+21])
			dataSize := le32(buf[itemOff+21 : itemOff+25])
			ds, de := btrfsHeaderSize+int(dataOff), btrfsHeaderSize+int(dataOff)+int(dataSize)
			if ds < 0 || de > len(buf) || ds > de {
				continue
			}
			item := buf[ds:de]

			switch key.Type {
			case keyExtentData:
				fileExtentCount++
				w.handleFileExtentItem(item)
			case keyExtentItem:
				extentCount++
				w.p.markLogicalRange(w.bm, w.cm, w.nodeSize, key.Objectid, key.Offset)
			case keyMetadataItem:
				extentCount++
				w.p.markLogicalRange(w.bm, w.cm, w.nodeSize, key.Objectid, uint64(w.nodeSize))
			case keyRootItem:
				rootItemCount++
				dbg("  ROOT_ITEM found: objectid=%d (leaf block %d)", key.Objectid, logical)
				w.handleRootItem(item)
			}
		}
		if rootItemCount+extentCount+fileExtentCount > 0 {
			dbg("walkTree leaf %d summary: ROOT_ITEM=%d EXTENT_ITEM/METADATA=%d EXTENT_DATA=%d",
				logical, rootItemCount, extentCount, fileExtentCount)
		}
		return
	}

	for i := uint32(0); i < nritems; i++ {
		ptrOff := btrfsHeaderSize + int(i)*btrfsKeyPtrSize
		if ptrOff+btrfsKeyPtrSize > len(buf) {
			break
		}
		blockptr := le64(buf[ptrOff+17 : ptrOff+25])
		w.walkTree(blockptr)
	}
}

func (w *walker) handleFileExtentItem(item []byte) {
	if len(item) < 21 || item[20] == fileExtentInline {
		return
	}
	if len(item) < 21+16 {
		return
	}
	diskBytenr := le64(item[21:29])
	diskNumBytes := le64(item[29:37])
	if diskBytenr == 0 || diskNumBytes == 0 {
		return
	}
	w.p.markLogicalRange(w.bm, w.cm, w.nodeSize, diskBytenr, diskNumBytes)
}

// handleRootItem parses btrfs_root_item: struct btrfs_inode_item (160 bytes)
// + generation (8) + root_dirid (8) + bytenr (8) + ...
func (w *walker) handleRootItem(item []byte) {
	const bytenrOffset = 160 + 8 + 8
	if len(item) < bytenrOffset+8 {
		dbg("  handleRootItem: item too short (%d bytes, need %d)", len(item), bytenrOffset+8)
		return
	}
	bytenr := le64(item[bytenrOffset : bytenrOffset+8])
	dbg("  handleRootItem: root bytenr=%d", bytenr)
	w.walkTree(bytenr)
}

// ---------------- Chunk-tree-only collection (bootstrap) ----------------

func collectChunks(w *walker, logical uint64, out *chunkMap, visited map[uint64]bool) {
	if logical == 0 || visited[logical] {
		return
	}
	visited[logical] = true
	buf, err := w.readTreeBlock(logical)
	if err != nil || len(buf) < btrfsHeaderSize {
		return
	}
	level := buf[btrfsHeaderSize-1]
	nritems := le32(buf[btrfsHeaderSize-5 : btrfsHeaderSize-1])
	maxItems := (w.nodeSize - btrfsHeaderSize) / btrfsItemSize
	if nritems > maxItems {
		return
	}
	if level == 0 {
		for i := uint32(0); i < nritems; i++ {
			itemOff := btrfsHeaderSize + int(i)*btrfsItemSize
			if itemOff+btrfsItemSize > len(buf) {
				break
			}
			key := parseDiskKey(buf[itemOff : itemOff+17])
			if key.Type != keyChunkItem {
				continue
			}
			dataOff := le32(buf[itemOff+17 : itemOff+21])
			dataSize := le32(buf[itemOff+21 : itemOff+25])
			ds, de := btrfsHeaderSize+int(dataOff), btrfsHeaderSize+int(dataOff)+int(dataSize)
			if ds < 0 || de > len(buf) {
				continue
			}
			if e, ok := parseChunkItem(buf[ds:de], key.Offset); ok { // fix: use key.Offset
				out.add(e)
			}
		}
		return
	}
	for i := uint32(0); i < nritems; i++ {
		ptrOff := btrfsHeaderSize + int(i)*btrfsKeyPtrSize
		if ptrOff+btrfsKeyPtrSize > len(buf) {
			break
		}
		blockptr := le64(buf[ptrOff+17 : ptrOff+25])
		collectChunks(w, blockptr, out, visited)
	}
}

// ---------------- Dump ----------------

func (p *BitmapParser) Dump() (*bitmap.FsBitmap, error) {
	defer func() {
		if p.fr != nil {
			_ = p.fr.Close()
		}
	}()

	sb, err := p.readSuperblock()
	if err != nil {
		return nil, err
	}

	bits := (int64(sb.TotalBytes) + int64(sb.NodeSize) - 1) / int64(sb.NodeSize)
	bm := bitmap.NewFsBitmap(define.FsTypeBtrfs, bitmap.BitmapFromFS, bits, int(sb.NodeSize))
	dbg("bitmap created: bits=%d blockSize=%d totalBytes=%d", bits, sb.NodeSize, sb.TotalBytes)

	bm.SetRange(0, uint32(btrfsSuperInfoOffset/int64(sb.NodeSize))+1)
	for mirror := 0; mirror < 3; mirror++ {
		off := sbMirrorOffset(mirror)
		if uint64(off)+4096 > sb.TotalBytes {
			dbg("mirror %d offset %d exceeds device size, skip", mirror, off)
			continue
		}
		p.markPhysical(bm, sb.NodeSize, uint64(off), uint64(sb.NodeSize))
	}
	dbg("after superblock marking: %d blocks set (%d bytes)", popcount(bm), popcount(bm)*int64(sb.NodeSize))

	bootCM := parseSysChunkArray(sb.SysChunkArray)
	if len(bootCM.entries) == 0 {
		return nil, fmt.Errorf("bootstrap chunk map is EMPTY — superblock offsets are almost certainly wrong")
	}
	ce := bootCM.find(sb.ChunkRoot)
	if ce == nil {
		dbg("ChunkRoot %d NOT found in bootstrap chunk map! entries logical ranges:", sb.ChunkRoot)
		for _, e := range bootCM.entries {
			dbg("  [%d, %d)", e.Logical, e.Logical+e.Length)
		}
		return nil, fmt.Errorf("chunk root %d not covered by sys_chunk_array, bootstrap failed", sb.ChunkRoot)
	}
	dbg("ChunkRoot %d found in bootstrap chunk, physical=%d", sb.ChunkRoot, ce.Stripes[0].Physical+(sb.ChunkRoot-ce.Logical))

	bootWalker := newWalker(p, bm, bootCM, sb.NodeSize)
	fullCM := &chunkMap{}
	collectChunks(bootWalker, sb.ChunkRoot, fullCM, map[uint64]bool{})
	dbg("collectChunks from chunk tree found %d chunk items", len(fullCM.entries))

	fullCM.entries = append(fullCM.entries, bootCM.entries...)
	fullCM.sortAndDedup()
	dbg("full chunk map after merge+dedup: %d entries", len(fullCM.entries))
	for i, e := range fullCM.entries {
		if i < 20 { // only print the first 20 to avoid flooding the log
			dbg("  chunk[%d]: logical=[%d,%d) type=0x%x stripes=%d", i, e.Logical, e.Logical+e.Length, e.Type, len(e.Stripes))
		}
	}

	if len(fullCM.entries) == 0 {
		return nil, fmt.Errorf("failed to build chunk map, got 0 chunks")
	}

	w := newWalker(p, bm, fullCM, sb.NodeSize)
	w.walkTree(sb.ChunkRoot)
	dbg("after walking chunk tree: %d blocks set (%d bytes)", popcount(bm), popcount(bm)*int64(sb.NodeSize))

	w.walkTree(sb.Root)
	dbg("after walking tree_root (incl. all ROOT_ITEM subtrees): %d blocks set (%d bytes)", popcount(bm), popcount(bm)*int64(sb.NodeSize))
	dbg("total root-item subtrees walked: %d (see rootItemCount log lines above)")

	return bm, nil
}

func popcount(bm *bitmap.FsBitmap) int64 {
	var n int64
	for _, b := range bm.Bitmap {
		for b != 0 {
			n += int64(b & 1)
			b >>= 1
		}
	}
	return n
}
