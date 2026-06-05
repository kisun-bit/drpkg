package bitmap

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/kisun-bit/drpkg/disk/filesystem/types"
	"github.com/pkg/errors"
)

const (
	exfatFirstDataCluster = 2
	exfatEntryBitmap      = 0x81
)

type exfatBitmapParser struct {
	fr *fsRegionReader
}

type exfatBoot struct {
	fatOffset      uint32
	fatLength      uint32
	clusterHeapOff uint32
	clusterCount   uint32
	rootCluster    uint32
	sectorSize     uint32
	sectorsPerClus uint32
	sectorCount    uint64
}

func NewExFatBitmapParser(device string, fsStart int64, fsSize int64) (FsBitmapParser, error) {
	fr, err := newFsRegionReader(device, fsStart, fsSize)
	if err != nil {
		return nil, err
	}
	return &exfatBitmapParser{fr: fr}, nil
}

func (p *exfatBitmapParser) String() string {
	return fmt.Sprintf("exfatBitmapParser(%s)", p.fr)
}

func (p *exfatBitmapParser) Dump() (*FsBitmap, error) {
	sb, err := p.readBoot()
	if err != nil {
		return nil, err
	}
	bits := int64(sb.sectorCount)
	bm, err := bitmapBytes(bits)
	if err != nil {
		return nil, err
	}
	fillBitmap(bm, 0x00)

	bitmapCluster, bitmapSize, err := p.findAllocationBitmap(sb)
	if err != nil {
		return nil, err
	}
	clusterBitmap := make([]byte, bitmapSize)
	if _, err := p.fr.ReadAt(clusterBitmap, int64(sb.clusterOffset(bitmapCluster))); err != nil && err != io.EOF {
		return nil, errors.Errorf("read exFAT allocation bitmap failed: %v", err)
	}
	for cluster := uint32(exfatFirstDataCluster); cluster < sb.clusterCount+exfatFirstDataCluster; cluster++ {
		idx := uint64(cluster - exfatFirstDataCluster)
		if !testBit(clusterBitmap, idx) {
			continue
		}
		firstSector := uint64(sb.clusterHeapOff) + uint64(cluster-exfatFirstDataCluster)*uint64(sb.sectorsPerClus)
		for s := uint32(0); s < sb.sectorsPerClus; s++ {
			sector := firstSector + uint64(s)
			if sector >= sb.sectorCount {
				break
			}
			if err := setBit(bm, bits, sector); err != nil {
				return nil, err
			}
		}
	}

	return &FsBitmap{
		Type:       types.FsTypeExFat,
		BitmapKind: BitmapFromFS,
		Bitmap:     bm,
		Bits:       bits,
		BlockSize:  int(sb.sectorSize),
	}, nil
}

func (p *exfatBitmapParser) readBoot() (*exfatBoot, error) {
	buf := make([]byte, 512)
	if _, err := p.fr.ReadAt(buf, 0); err != nil && err != io.EOF {
		return nil, errors.Errorf("read exFAT boot sector failed: %v", err)
	}
	if string(buf[3:11]) != "EXFAT   " {
		return nil, errors.New("invalid exFAT OEM name")
	}
	sectorBits := buf[108]
	spcBits := buf[109]
	if sectorBits > 20 || spcBits > 25 {
		return nil, errors.Errorf("invalid exFAT sector/cluster shift: %d/%d", sectorBits, spcBits)
	}
	sb := &exfatBoot{
		fatOffset:      binary.LittleEndian.Uint32(buf[80:84]),
		fatLength:      binary.LittleEndian.Uint32(buf[84:88]),
		clusterHeapOff: binary.LittleEndian.Uint32(buf[88:92]),
		clusterCount:   binary.LittleEndian.Uint32(buf[92:96]),
		rootCluster:    binary.LittleEndian.Uint32(buf[96:100]),
		sectorSize:     uint32(1) << sectorBits,
		sectorsPerClus: uint32(1) << spcBits,
		sectorCount:    binary.LittleEndian.Uint64(buf[72:80]),
	}
	if sb.sectorSize == 0 || sb.sectorsPerClus == 0 || sb.sectorCount == 0 ||
		sb.clusterHeapOff == 0 || sb.clusterCount == 0 || sb.rootCluster < exfatFirstDataCluster {
		return nil, errors.New("invalid exFAT boot fields")
	}
	return sb, nil
}

func (p *exfatBitmapParser) findAllocationBitmap(sb *exfatBoot) (uint32, uint64, error) {
	clusterSize := uint64(sb.sectorSize) * uint64(sb.sectorsPerClus)
	if clusterSize == 0 || clusterSize > 128*1024*1024 {
		return 0, 0, errors.Errorf("invalid exFAT cluster size: %d", clusterSize)
	}
	cluster := sb.rootCluster
	visited := uint32(0)
	for cluster >= exfatFirstDataCluster && cluster < sb.clusterCount+exfatFirstDataCluster {
		buf := make([]byte, clusterSize)
		if _, err := p.fr.ReadAt(buf, int64(sb.clusterOffset(cluster))); err != nil && err != io.EOF {
			return 0, 0, errors.Errorf("read exFAT root directory cluster failed: %v", err)
		}
		for off := 0; off+32 <= len(buf); off += 32 {
			entryType := buf[off]
			if entryType == 0x00 {
				return 0, 0, errors.New("exFAT allocation bitmap entry not found")
			}
			if entryType != exfatEntryBitmap {
				continue
			}
			startCluster := binary.LittleEndian.Uint32(buf[off+20 : off+24])
			size := binary.LittleEndian.Uint64(buf[off+24 : off+32])
			if startCluster < exfatFirstDataCluster || size == 0 {
				return 0, 0, errors.New("invalid exFAT allocation bitmap entry")
			}
			minSize := divRoundUp64(uint64(sb.clusterCount), 8)
			if size < minSize {
				return 0, 0, errors.Errorf("exFAT allocation bitmap too small: %d < %d", size, minSize)
			}
			return startCluster, minSize, nil
		}
		visited++
		if visited > sb.clusterCount {
			break
		}
		next, err := p.nextCluster(sb, cluster)
		if err != nil {
			return 0, 0, err
		}
		if next >= 0xfffffff8 {
			break
		}
		cluster = next
	}
	return 0, 0, errors.New("exFAT allocation bitmap entry not found")
}

func (p *exfatBitmapParser) nextCluster(sb *exfatBoot, cluster uint32) (uint32, error) {
	off := uint64(sb.fatOffset)*uint64(sb.sectorSize) + uint64(cluster)*4
	buf := make([]byte, 4)
	if _, err := p.fr.ReadAt(buf, int64(off)); err != nil && err != io.EOF {
		return 0, errors.Errorf("read exFAT FAT entry failed: %v", err)
	}
	return binary.LittleEndian.Uint32(buf), nil
}

func (b *exfatBoot) clusterOffset(cluster uint32) uint64 {
	return (uint64(b.clusterHeapOff) + uint64(cluster-exfatFirstDataCluster)*uint64(b.sectorsPerClus)) * uint64(b.sectorSize)
}
