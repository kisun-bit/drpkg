package fat

import (
	"encoding/binary"
	"fmt"

	"github.com/kisun-bit/drpkg/disk/filesystem/bitmap"
	"github.com/kisun-bit/drpkg/extend"
)

// FAT variants
const (
	fatUnknown = iota
	fat12
	fat16
	fat32
)

const (
	fat12Threshold = 4085
	fat16Threshold = 65525
	// Maximum valid FAT32 cluster count, guarding against a corrupted or
	// malicious superblock causing memory exhaustion or an infinite loop.
	maxFatClusters = uint64(0x0FFFFFF6)
)

// fatBootSector mirrors struct FatBootSector in the C code (parsed at the
// standard BPB offsets).
type fatBootSector struct {
	sectorSize  uint16 // offset 11, bytes per sector
	clusterSize uint8  // offset 13, sectors per cluster
	reserved    uint16 // offset 14, number of reserved sectors
	fats        uint8  // offset 16, number of FAT copies
	dirEntries  uint16 // offset 17, number of root directory entries (FAT12/16)
	sectors     uint16 // offset 19, total sectors (16-bit, small volumes)
	fatLength   uint16 // offset 22, sectors per FAT (FAT12/16)
	sectorCount uint32 // offset 32, total sectors (32-bit, large volumes)

	fat32Length uint32  // offset 36, sectors per FAT (FAT32)
	rootCluster uint32  // offset 44, FAT32 root directory start cluster (currently unused)
	extSig16    uint8   // offset 38, FAT12/16 boot signature
	extSig32    uint8   // offset 66, FAT32 boot signature
	fatName32   [8]byte // offset 82, FAT32 fs_type string "FAT32   "
}

// BitmapParser parses a used-sector bitmap from a FAT12/16/32 filesystem.
type BitmapParser struct {
	dev   string
	start int64
	size  int64
	fr    *extend.FsRegionReader

	sb     fatBootSector
	fsType int
	fsName string

	// Sequential read cursor (offset relative to the region's start),
	// mirroring the current file position maintained by read()/lseek() in C.
	cursor int64

	// FAT12 nibble buffer: hasNibble=false corresponds to nibble==0xFF
	// (empty buffer) in the C code.
	nibble    uint16
	hasNibble bool
}

func NewBitmapParser(dev string, start int64, size int64) (bitmap.FsBitmapParser, error) {
	fr, e := extend.NewFsRegionReader(dev, start, size)
	if e != nil {
		return nil, e
	}
	return &BitmapParser{dev: dev, start: start, size: size, fr: fr}, nil
}

func (p *BitmapParser) String() string {
	return fmt.Sprintf("<FATBitmapParser(dev=%s,start=%d,size=%d)>",
		p.dev, p.start, p.size)
}

// ---- Low-level sequential read helpers (equivalent to read()/lseek() in C) ----

func (p *BitmapParser) seek(off int64) {
	p.cursor = off
}

func (p *BitmapParser) readSeq(n int) ([]byte, error) {
	buf := make([]byte, n)
	m, err := p.fr.ReadAt(buf, p.cursor)
	if err != nil {
		return nil, err
	}
	if m != n {
		return nil, fmt.Errorf("short read at offset %d: want %d got %d", p.cursor, n, m)
	}
	p.cursor += int64(n)
	return buf, nil
}

// read12 reads a single 12-bit FAT entry, maintaining the internal nibble
// buffer state. Equivalent to read12() in C: FAT12 packs two 12-bit
// entries into every 3 bytes.
func (p *BitmapParser) read12() (uint16, error) {
	if !p.hasNibble {
		buf, err := p.readSeq(2)
		if err != nil {
			return 0, err
		}
		buffer := binary.LittleEndian.Uint16(buf)
		p.nibble = buffer >> 12
		p.hasNibble = true
		return buffer & 0xFFF, nil
	}
	buf, err := p.readSeq(1)
	if err != nil {
		return 0, err
	}
	out := (uint16(buf[0]) << 4) | p.nibble
	p.hasNibble = false
	return out, nil
}

// ---- Boot sector parsing ----

func (p *BitmapParser) parseBootSector() error {
	buf, err := p.readSeq(90) // need to read up through offset 82~89 (FAT32 fs_type)
	if err != nil {
		return fmt.Errorf("read boot sector: %w", err)
	}
	sb := &p.sb
	sb.sectorSize = binary.LittleEndian.Uint16(buf[11:13])
	sb.clusterSize = buf[13]
	sb.reserved = binary.LittleEndian.Uint16(buf[14:16])
	sb.fats = buf[16]
	sb.dirEntries = binary.LittleEndian.Uint16(buf[17:19])
	sb.sectors = binary.LittleEndian.Uint16(buf[19:21])
	sb.fatLength = binary.LittleEndian.Uint16(buf[22:24])
	sb.sectorCount = binary.LittleEndian.Uint32(buf[32:36])

	sb.extSig16 = buf[38] // FAT12/16 boot signature

	sb.fat32Length = binary.LittleEndian.Uint32(buf[36:40])
	sb.rootCluster = binary.LittleEndian.Uint32(buf[44:48])
	sb.extSig32 = buf[66] // FAT32 boot signature
	copy(sb.fatName32[:], buf[82:90])

	if sb.sectorSize == 0 {
		return fmt.Errorf("invalid sector size: 0")
	}
	if sb.clusterSize == 0 {
		return fmt.Errorf("invalid cluster size: 0")
	}
	return nil
}

// ---- Various size calculations, equivalent to get_total_sector /
// get_sec_per_fat / get_root_sec / get_cluster_count in C ----

func (p *BitmapParser) getTotalSector() (uint64, error) {
	if p.sb.sectors != 0 {
		return uint64(p.sb.sectors), nil
	}
	if p.sb.sectorCount != 0 {
		return uint64(p.sb.sectorCount), nil
	}
	return 0, fmt.Errorf("total_sector error: sectors and sector_count are both zero")
}

func (p *BitmapParser) getSecPerFat() (uint64, error) {
	if p.sb.fatLength != 0 {
		return uint64(p.sb.fatLength), nil
	}
	if p.sb.fat32Length != 0 {
		return uint64(p.sb.fat32Length), nil
	}
	return 0, fmt.Errorf("sec_per_fat is zero")
}

func (p *BitmapParser) getRootSec() uint64 {
	return (uint64(p.sb.dirEntries)*32 + uint64(p.sb.sectorSize) - 1) / uint64(p.sb.sectorSize)
}

func roundToMultiple(n, m uint64) uint64 {
	if n == 0 || m == 0 {
		return 0
	}
	return n + m - 1 - (n-1)%m
}

func (p *BitmapParser) getClusterCount() (uint64, error) {
	totalSector, err := p.getTotalSector()
	if err != nil {
		return 0, err
	}
	rootSec := p.getRootSec()
	secPerFat, err := p.getSecPerFat()
	if err != nil {
		return 0, err
	}
	reserved := uint64(p.sb.reserved) + uint64(p.sb.fats)*secPerFat + rootSec
	if reserved > totalSector {
		return 0, nil
	}
	dataSec := totalSector - reserved
	return dataSec / uint64(p.sb.clusterSize), nil
}

// getFatType determines FAT12/16/32, equivalent to get_fat_type() in C.
func (p *BitmapParser) getFatType() error {
	sb := &p.sb
	if sb.extSig16 == 0x29 || (sb.fatLength != 0 && sb.fat32Length == 0) {
		totalSector, err := p.getTotalSector()
		if err != nil {
			return err
		}
		logicalSectorSize := uint64(sb.sectorSize)
		secPerFat, err := p.getSecPerFat()
		if err != nil {
			return err
		}
		rootStart := (uint64(sb.reserved) + uint64(sb.fats)*secPerFat) * logicalSectorSize
		dataStart := rootStart + roundToMultiple(uint64(sb.dirEntries)<<5, logicalSectorSize)
		dataSize := int64(totalSector*logicalSectorSize) - int64(dataStart)
		if dataSize <= 0 {
			return fmt.Errorf("data_size count error")
		}
		clusters := uint64(dataSize) / (uint64(sb.clusterSize) * logicalSectorSize)
		if clusters == 0 {
			return fmt.Errorf("clusters count error")
		}
		if clusters >= fat12Threshold {
			p.fsType = fat16
			p.fsName = "FAT16"
			if clusters >= fat16Threshold {
				// Equivalent to the log_mesg warning in C: cluster count
				// exceeds the FAT16 limit; log only, don't abort.
			}
		} else {
			p.fsType = fat12
			p.fsName = "FAT12"
		}
	} else if sb.fatName32[4] == '2' || (sb.fatLength == 0 && sb.fat32Length != 0) {
		p.fsType = fat32
		p.fsName = "FAT32"
	} else {
		return fmt.Errorf("unknown fat type")
	}
	return nil
}

// ---- Volume state check, equivalent to check_fat_status() in C ----
// Return value: 0 clean, 1 not cleanly unmounted, 2 I/O error.
func (p *BitmapParser) checkFatStatus() (int, error) {
	switch p.fsType {
	case fat16:
		if _, err := p.readSeq(2); err != nil { // FAT[0] media byte
			return 2, err
		}
		buf, err := p.readSeq(2) // FAT[1] dirty flag
		if err != nil {
			return 2, err
		}
		entry := binary.LittleEndian.Uint16(buf)
		if entry&0x8000 == 0 {
			return 1, nil
		}
		if entry&0x4000 == 0 {
			return 2, nil
		}
		return 0, nil

	case fat32:
		if _, err := p.readSeq(4); err != nil {
			return 2, err
		}
		buf, err := p.readSeq(4)
		if err != nil {
			return 2, err
		}
		entry := binary.LittleEndian.Uint32(buf)
		if entry&0x08000000 == 0 {
			return 1, nil
		}
		if entry&0x04000000 == 0 {
			return 2, nil
		}
		return 0, nil

	case fat12:
		if _, err := p.read12(); err != nil { // FAT[0]
			return 2, err
		}
		if _, err := p.read12(); err != nil { // FAT[1]; FAT12 has no dirty-bit flag, just skipped over
			return 2, err
		}
		return 0, nil

	default:
		return 2, fmt.Errorf("wrong fs type")
	}
}

// ---- Marking the reserved area, equivalent to mark_reserved_sectors() in C ----
func (p *BitmapParser) markReservedSectors(fb *bitmap.FsBitmap, block uint64) (uint64, error) {
	secPerFat, err := p.getSecPerFat()
	if err != nil {
		return block, err
	}
	rootSec := p.getRootSec()

	// A) reserved sectors
	for i := uint64(0); i < uint64(p.sb.reserved); i++ {
		fb.Set(block)
		block++
	}
	// B) sectors occupied by the FAT table(s)
	for j := uint8(0); j < p.sb.fats; j++ {
		for i := uint64(0); i < secPerFat; i++ {
			fb.Set(block)
			block++
		}
	}
	// C) sectors occupied by the root directory (FAT32 has no dedicated root directory area)
	if rootSec > 0 {
		for i := uint64(0); i < rootSec; i++ {
			fb.Set(block)
			block++
		}
	}
	return block, nil
}

// ---- Per-cluster status check, equivalent to check_fat12/16/32_entry() ----

func (p *BitmapParser) markCluster(fb *bitmap.FsBitmap, block uint64, used bool) uint64 {
	clusterSize := uint64(p.sb.clusterSize)
	for i := uint64(0); i < clusterSize; i++ {
		if used {
			fb.Set(block)
		} else {
			fb.Clear(block)
		}
		block++
	}
	return block
}

func (p *BitmapParser) checkFat32Entry(fb *bitmap.FsBitmap, block uint64) (uint64, error) {
	buf, err := p.readSeq(4)
	if err != nil {
		return block, err
	}
	entry := binary.LittleEndian.Uint32(buf)
	switch entry {
	case 0x0FFFFFF7: // bad cluster
		return p.markCluster(fb, block, false), nil
	case 0x00000000: // free
		return p.markCluster(fb, block, false), nil
	default: // in use
		return p.markCluster(fb, block, true), nil
	}
}

func (p *BitmapParser) checkFat16Entry(fb *bitmap.FsBitmap, block uint64) (uint64, error) {
	buf, err := p.readSeq(2)
	if err != nil {
		return block, err
	}
	entry := binary.LittleEndian.Uint16(buf)
	switch entry {
	case 0xFFF7:
		return p.markCluster(fb, block, false), nil
	case 0x0000:
		return p.markCluster(fb, block, false), nil
	default:
		return p.markCluster(fb, block, true), nil
	}
}

func (p *BitmapParser) checkFat12Entry(fb *bitmap.FsBitmap, block uint64) (uint64, error) {
	entry, err := p.read12()
	if err != nil {
		return block, err
	}
	switch entry {
	case 0xFF7:
		return p.markCluster(fb, block, false), nil
	case 0x000:
		return p.markCluster(fb, block, false), nil
	default:
		return p.markCluster(fb, block, true), nil
	}
}

// ---- Main flow, equivalent to read_super_blocks() + read_bitmap() ----

func (p *BitmapParser) Dump() (bitmapOut *bitmap.FsBitmap, err error) {
	defer func() {
		if p.fr != nil {
			_ = p.fr.Close()
		}
	}()

	p.seek(0)
	if err := p.parseBootSector(); err != nil {
		return nil, err
	}
	if err := p.getFatType(); err != nil {
		return nil, err
	}

	totalSector, err := p.getTotalSector()
	if err != nil {
		return nil, err
	}

	clusterCount, err := p.getClusterCount()
	if err != nil {
		return nil, err
	}
	if clusterCount > maxFatClusters {
		return nil, fmt.Errorf(
			"ERROR: maliciously large cluster_count detected: %d, max allowed: %d",
			clusterCount, maxFatClusters)
	}

	fb := bitmap.NewFsBitmap(p.fsName, bitmap.BitmapFromFS, int64(totalSector), int(p.sb.sectorSize))

	// Start with everything marked "used", equivalent to
	// pc_init_bitmap(bitmap, 0xFF, total_sector) in C.
	fb.SetAll()

	// A) B) C): mark reserved sectors / FAT table(s) / root directory as used
	block, err := p.markReservedSectors(fb, 0)
	if err != nil {
		return nil, err
	}

	// Jump to the start of the first FAT table (right after the reserved sectors)
	fatReservedBytes := int64(p.sb.sectorSize) * int64(p.sb.reserved)
	p.seek(fatReservedBytes)
	p.hasNibble = false

	// Use the first FAT table's first two entries to check the volume state (dirty flag)
	fatStat, err := p.checkFatStatus()
	if err != nil {
		return nil, fmt.Errorf("check fat status: %w", err)
	}
	switch fatStat {
	case 1:
		return nil, fmt.Errorf("filesystem isn't in a valid state (not cleanly unmounted)")
	case 2:
		return nil, fmt.Errorf("I/O error while checking fat status")
	}

	// D) scan cluster by cluster, starting from the first data cluster (cluster 2)
	for i := uint64(0); i < clusterCount; i++ {
		if block >= totalSector {
			return nil, fmt.Errorf("block too large: block=%d total_sector=%d", block, totalSector)
		}
		switch p.fsType {
		case fat16:
			block, err = p.checkFat16Entry(fb, block)
		case fat32:
			block, err = p.checkFat32Entry(fb, block)
		case fat12:
			block, err = p.checkFat12Entry(fb, block)
		default:
			err = fmt.Errorf("unknown fs type")
		}
		if err != nil {
			return nil, fmt.Errorf("read fat entry %d: %w", i, err)
		}
	}

	// Any remaining trailing sectors after the cluster scan (alignment/padding
	// area) are all marked as used, equivalent to the
	// `while(block < total_sector) pc_set_bit(...)` loop in get_used_block() in C.
	for ; block < totalSector; block++ {
		fb.Set(block)
	}

	return fb, nil
}
