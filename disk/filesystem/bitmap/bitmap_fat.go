package bitmap

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/kisun-bit/drpkg/disk/filesystem/types"
	"github.com/pkg/errors"
)

const (
	fat12Threshold = 4085
	fat16Threshold = 65525
	fat12          = 12
	fat16          = 16
	fat32          = 32
)

type fatBitmapParser struct {
	fr *fsRegionReader
}

type fatBoot struct {
	sectorSize       uint16
	sectorsPerClus   uint8
	reservedSectors  uint16
	fats             uint8
	rootEntries      uint16
	totalSectors16   uint16
	fatSize16        uint16
	totalSectors32   uint32
	fatSize32        uint32
	fat32RootCluster uint32
	kind             int
}

func NewFatBitmapParser(device string, fsStart int64, fsSize int64) (FsBitmapParser, error) {
	fr, err := newFsRegionReader(device, fsStart, fsSize)
	if err != nil {
		return nil, err
	}
	return &fatBitmapParser{fr: fr}, nil
}

func (p *fatBitmapParser) String() string {
	return fmt.Sprintf("fatBitmapParser(%s)", p.fr)
}

func (p *fatBitmapParser) Dump() (*FsBitmap, error) {
	sb, err := p.readBoot()
	if err != nil {
		return nil, err
	}
	totalSectors := sb.totalSectors()
	bits := int64(totalSectors)
	bm, err := bitmapBytes(bits)
	if err != nil {
		return nil, err
	}
	fillBitmap(bm, 0xff)

	block := uint64(0)
	for ; block < uint64(sb.reservedSectors); block++ {
		if err := setBit(bm, bits, block); err != nil {
			return nil, err
		}
	}
	for copyIdx := uint8(0); copyIdx < sb.fats; copyIdx++ {
		for i := uint32(0); i < sb.fatSize(); i++ {
			if err := setBit(bm, bits, block); err != nil {
				return nil, err
			}
			block++
		}
	}
	for i := uint32(0); i < sb.rootDirSectors(); i++ {
		if err := setBit(bm, bits, block); err != nil {
			return nil, err
		}
		block++
	}

	clusterCount, err := sb.clusterCount()
	if err != nil {
		return nil, err
	}
	fatBytes := make([]byte, int64(sb.fatSize())*int64(sb.sectorSize))
	if _, err := p.fr.ReadAt(fatBytes, int64(sb.reservedSectors)*int64(sb.sectorSize)); err != nil && err != io.EOF {
		return nil, errors.Errorf("read FAT failed: %v", err)
	}

	for cluster := uint64(0); cluster < clusterCount && block < totalSectors; cluster++ {
		used, err := sb.clusterUsed(fatBytes, cluster+2)
		if err != nil {
			return nil, err
		}
		for i := uint8(0); i < sb.sectorsPerClus && block < totalSectors; i++ {
			if used {
				if err := setBit(bm, bits, block); err != nil {
					return nil, err
				}
			} else if err := clearBit(bm, bits, block); err != nil {
				return nil, err
			}
			block++
		}
	}

	return &FsBitmap{
		Type:       types.FsTypeFat,
		BitmapKind: BitmapFromFS,
		Bitmap:     bm,
		Bits:       bits,
		BlockSize:  int(sb.sectorSize),
	}, nil
}

func (p *fatBitmapParser) readBoot() (*fatBoot, error) {
	buf := make([]byte, 512)
	if _, err := p.fr.ReadAt(buf, 0); err != nil && err != io.EOF {
		return nil, errors.Errorf("read FAT boot sector failed: %v", err)
	}
	if buf[510] != 0x55 || buf[511] != 0xaa {
		return nil, errors.New("invalid FAT boot signature")
	}
	sb := &fatBoot{
		sectorSize:       binary.LittleEndian.Uint16(buf[11:13]),
		sectorsPerClus:   buf[13],
		reservedSectors:  binary.LittleEndian.Uint16(buf[14:16]),
		fats:             buf[16],
		rootEntries:      binary.LittleEndian.Uint16(buf[17:19]),
		totalSectors16:   binary.LittleEndian.Uint16(buf[19:21]),
		fatSize16:        binary.LittleEndian.Uint16(buf[22:24]),
		totalSectors32:   binary.LittleEndian.Uint32(buf[32:36]),
		fatSize32:        binary.LittleEndian.Uint32(buf[36:40]),
		fat32RootCluster: binary.LittleEndian.Uint32(buf[44:48]),
	}
	if sb.sectorSize == 0 || sb.sectorsPerClus == 0 || sb.reservedSectors == 0 || sb.fats == 0 {
		return nil, errors.New("invalid FAT BPB")
	}
	if sb.totalSectors() == 0 || sb.fatSize() == 0 {
		return nil, errors.New("invalid FAT size fields")
	}
	clusters, err := sb.clusterCount()
	if err != nil {
		return nil, err
	}
	switch {
	case clusters < fat12Threshold:
		sb.kind = fat12
	case clusters < fat16Threshold:
		sb.kind = fat16
	default:
		sb.kind = fat32
	}
	return sb, nil
}

func (b *fatBoot) totalSectors() uint64 {
	if b.totalSectors16 != 0 {
		return uint64(b.totalSectors16)
	}
	return uint64(b.totalSectors32)
}

func (b *fatBoot) fatSize() uint32 {
	if b.fatSize16 != 0 {
		return uint32(b.fatSize16)
	}
	return b.fatSize32
}

func (b *fatBoot) rootDirSectors() uint32 {
	return (uint32(b.rootEntries)*32 + uint32(b.sectorSize) - 1) / uint32(b.sectorSize)
}

func (b *fatBoot) clusterCount() (uint64, error) {
	reserved := uint64(b.reservedSectors) + uint64(b.fats)*uint64(b.fatSize()) + uint64(b.rootDirSectors())
	total := b.totalSectors()
	if reserved > total {
		return 0, errors.New("FAT reserved area exceeds total sectors")
	}
	return (total - reserved) / uint64(b.sectorsPerClus), nil
}

func (b *fatBoot) clusterUsed(fat []byte, cluster uint64) (bool, error) {
	switch b.kind {
	case fat12:
		pos := cluster + cluster/2
		if pos+1 >= uint64(len(fat)) {
			return false, errors.Errorf("FAT12 entry %d out of range", cluster)
		}
		raw := uint16(fat[pos]) | uint16(fat[pos+1])<<8
		if cluster&1 != 0 {
			raw >>= 4
		}
		raw &= 0x0fff
		return raw != 0 && raw != 0x0ff7, nil
	case fat16:
		pos := cluster * 2
		if pos+1 >= uint64(len(fat)) {
			return false, errors.Errorf("FAT16 entry %d out of range", cluster)
		}
		raw := binary.LittleEndian.Uint16(fat[pos : pos+2])
		return raw != 0 && raw != 0xfff7, nil
	case fat32:
		pos := cluster * 4
		if pos+3 >= uint64(len(fat)) {
			return false, errors.Errorf("FAT32 entry %d out of range", cluster)
		}
		raw := binary.LittleEndian.Uint32(fat[pos:pos+4]) & 0x0fffffff
		return raw != 0 && raw != 0x0ffffff7, nil
	default:
		return false, errors.Errorf("unknown FAT kind: %d", b.kind)
	}
}
