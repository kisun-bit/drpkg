package exfat

import (
	"encoding/binary"
	"fmt"

	"github.com/kisun-bit/drpkg/disk/filesystem/bitmap"
	"github.com/kisun-bit/drpkg/extend"
)

// exFAT-related constants
const (
	direntTypeBitmap   = 0x81 // Allocation Bitmap directory entry
	direntTypeEndOfDir = 0x00 // End-of-directory marker

	exfatBadCluster = 0xFFFFFFF7 // Bad-cluster marker (also used as the upper bound when walking a cluster chain)

	// Mirrors MAX_EXFAT_SECTORS in the C code: guards against a corrupted/malicious
	// superblock causing memory exhaustion.
	maxExfatSectors = uint64(1) << 40

	// Safety limit when walking the root directory's cluster chain, to prevent an
	// infinite loop caused by a malformed FAT chain.
	maxDirTraverseEntries = 1 << 20
)

// ExfatBitmapParser parses used-cluster information from an exFAT filesystem
// and expands it into a sector-level bitmap.
type ExfatBitmapParser struct {
	dev   string
	start int64
	size  int64
	fr    *extend.FsRegionReader
}

// NewExfatBitmapParser creates a new exFAT bitmap parser.
func NewExfatBitmapParser(dev string, start int64, size int64) (bitmap.FsBitmapParser, error) {
	fr, e := extend.NewFsRegionReader(dev, start, size)
	if e != nil {
		return nil, e
	}
	return &ExfatBitmapParser{dev: dev, start: start, size: size, fr: fr}, nil
}

func (p *ExfatBitmapParser) String() string {
	return fmt.Sprintf("<ExfatBitmapParser(dev=%s,start=%d,size=%d)>",
		p.dev, p.start, p.size)
}

func (p *ExfatBitmapParser) readAt(off int64, n int) ([]byte, error) {
	buf := make([]byte, n)
	if _, err := p.fr.ReadAt(buf, off); err != nil {
		return nil, err
	}
	return buf, nil
}

func (p *ExfatBitmapParser) Dump() (*bitmap.FsBitmap, error) {
	defer func() {
		if p.fr != nil {
			_ = p.fr.Close()
		}
	}()

	// ---- 1. Read and parse the boot sector (equivalent to read_super_blocks in C) ----
	bs, err := p.readAt(0, 512)
	if err != nil {
		return nil, fmt.Errorf("read boot sector: %w", err)
	}
	if string(bs[3:11]) != "EXFAT   " {
		return nil, fmt.Errorf("not an exfat filesystem (bad signature)")
	}

	volumeLength := binary.LittleEndian.Uint64(bs[72:80])      // sector_count
	fatOffset := uint64(binary.LittleEndian.Uint32(bs[80:84])) // sector offset
	clusterHeapOffset := uint64(binary.LittleEndian.Uint32(bs[88:92]))
	clusterCount := binary.LittleEndian.Uint32(bs[92:96])
	rootDirCluster := binary.LittleEndian.Uint32(bs[96:100])
	bytesPerSectorShift := bs[108]
	sectorsPerClusterShift := bs[109]

	if bytesPerSectorShift < 9 || bytesPerSectorShift > 12 {
		return nil, fmt.Errorf("invalid bytes-per-sector-shift: %d", bytesPerSectorShift)
	}
	if sectorsPerClusterShift > 25 {
		return nil, fmt.Errorf("invalid sectors-per-cluster-shift: %d", sectorsPerClusterShift)
	}

	sectorSize := 1 << bytesPerSectorShift
	sectorsPerCluster := uint64(1) << sectorsPerClusterShift
	clusterSize := sectorsPerCluster * uint64(sectorSize)

	// Equivalent to the sector_count bounds check in the C code, guarding against
	// a malicious/corrupted superblock.
	if volumeLength == 0 || volumeLength > maxExfatSectors {
		return nil, fmt.Errorf(
			"ERROR: maliciously large or zero sector_count detected: %d, max allowed: %d",
			volumeLength, maxExfatSectors)
	}
	if clusterCount == 0 || uint64(clusterCount) > maxExfatSectors {
		return nil, fmt.Errorf("invalid or malicious cluster count: %d", clusterCount)
	}

	// Looks up a FAT entry on demand (used while walking the root directory's
	// cluster chain; the FAT table is not loaded in full).
	nextCluster := func(cluster uint32) (uint32, error) {
		off := int64(fatOffset)*int64(sectorSize) + int64(cluster)*4
		buf, err := p.readAt(off, 4)
		if err != nil {
			return 0, err
		}
		return binary.LittleEndian.Uint32(buf), nil
	}

	clusterToByteOffset := func(cluster uint32) int64 {
		firstSector := clusterHeapOffset + (uint64(cluster)-2)*sectorsPerCluster
		return int64(firstSector) * int64(sectorSize)
	}

	// ---- 2. Walk the root directory's cluster chain to find the Allocation Bitmap entry ----
	var bitmapFirstCluster uint32
	var bitmapDataLength uint64
	found := false

	cluster := rootDirCluster
	entriesPerCluster := clusterSize / 32
	visited := 0

outer:
	for cluster >= 2 && cluster < exfatBadCluster {
		visited++
		if visited > maxDirTraverseEntries {
			return nil, fmt.Errorf("root directory chain too long or corrupted (possible loop)")
		}

		data, err := p.readAt(clusterToByteOffset(cluster), int(clusterSize))
		if err != nil {
			return nil, fmt.Errorf("read root dir cluster %d: %w", cluster, err)
		}

		for i := uint64(0); i < entriesPerCluster; i++ {
			ent := data[i*32 : i*32+32]
			entryType := ent[0]
			if entryType == direntTypeEndOfDir {
				break outer
			}
			if entryType == direntTypeBitmap {
				bitmapFlags := ent[1]
				if bitmapFlags&0x01 == 0 { // Only take the first bitmap copy (not the TexFAT mirror)
					bitmapFirstCluster = binary.LittleEndian.Uint32(ent[20:24])
					bitmapDataLength = binary.LittleEndian.Uint64(ent[24:32])
					found = true
					break outer
				}
			}
		}

		next, err := nextCluster(cluster)
		if err != nil {
			return nil, fmt.Errorf("read FAT entry for cluster %d: %w", cluster, err)
		}
		cluster = next
	}

	if !found {
		return nil, fmt.Errorf("allocation bitmap directory entry not found")
	}
	if bitmapFirstCluster < 2 {
		return nil, fmt.Errorf("invalid allocation bitmap first cluster: %d", bitmapFirstCluster)
	}

	expectedMinLen := (uint64(clusterCount) + 7) / 8
	if bitmapDataLength < expectedMinLen {
		return nil, fmt.Errorf(
			"allocation bitmap too small: got %d bytes, need at least %d",
			bitmapDataLength, expectedMinLen)
	}

	// ---- 3. Read the raw allocation bitmap (stored contiguously, no need to walk the FAT chain again) ----
	rawBitmap, err := p.readAt(clusterToByteOffset(bitmapFirstCluster), int(expectedMinLen))
	if err != nil {
		return nil, fmt.Errorf("read allocation bitmap data: %w", err)
	}

	// ---- 4. Build the sector-level FsBitmap (equivalent to the read_bitmap / pc_set_bit loop in C) ----
	result := bitmap.NewFsBitmap("exfat", bitmap.BitmapFromFS, int64(volumeLength), sectorSize)

	for c := uint32(0); c < clusterCount; c++ {
		byteIdx := c / 8
		bitOff := c % 8
		if rawBitmap[byteIdx]&(1<<bitOff) != 0 {
			startSector := clusterHeapOffset + uint64(c)*sectorsPerCluster
			result.SetRange(startSector, uint32(sectorsPerCluster))
		}
	}

	return result, nil
}
