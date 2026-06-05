package bitmap

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/kisun-bit/drpkg/disk/filesystem/types"
	"github.com/pkg/errors"
)

const (
	extSuperOffset          = 1024
	extSuperMagic           = 0xef53
	extFeatureIncompat64bit = 0x0080
)

type extBitmapParser struct {
	fr *fsRegionReader
}

type extSuper struct {
	inodesCount      uint32
	blocksCount      uint64
	freeBlocksCount  uint64
	firstDataBlock   uint32
	logBlockSize     uint32
	blocksPerGroup   uint32
	magic            uint16
	incompatFeatures uint32
	descSize         uint16
	blockSize        uint64
}

func NewExt234BitmapParser(device string, fsStart int64, fsSize int64) (FsBitmapParser, error) {
	fr, err := newFsRegionReader(device, fsStart, fsSize)
	if err != nil {
		return nil, err
	}
	return &extBitmapParser{fr: fr}, nil
}

func (p *extBitmapParser) String() string {
	return fmt.Sprintf("extBitmapParser(%s)", p.fr)
}

func (p *extBitmapParser) Dump() (*FsBitmap, error) {
	sb, err := p.readSuper()
	if err != nil {
		return nil, err
	}
	if sb.blocksCount > uint64(^uint(0)>>1) {
		return nil, errors.Errorf("ext block count too large: %d", sb.blocksCount)
	}
	bits := int64(sb.blocksCount)
	bm, err := bitmapBytes(bits)
	if err != nil {
		return nil, err
	}
	fillBitmap(bm, 0xff)

	groupCount := divRoundUp64(sb.blocksCount-uint64(sb.firstDataBlock), uint64(sb.blocksPerGroup))
	descs, err := p.readGroupDescriptors(sb, groupCount)
	if err != nil {
		return nil, err
	}
	for group := uint64(0); group < groupCount; group++ {
		desc := descs[group]
		if desc.blockBitmap == 0 {
			return nil, errors.Errorf("ext group %d has empty block bitmap", group)
		}
		groupBitmap := make([]byte, sb.blockSize)
		if _, err := p.fr.ReadAt(groupBitmap, int64(desc.blockBitmap*sb.blockSize)); err != nil && err != io.EOF {
			return nil, errors.Errorf("read ext block bitmap for group %d failed: %v", group, err)
		}

		groupStart := uint64(sb.firstDataBlock) + group*uint64(sb.blocksPerGroup)
		for i := uint64(0); i < uint64(sb.blocksPerGroup); i++ {
			block := groupStart + i
			if block >= sb.blocksCount {
				break
			}
			if testBit(groupBitmap, i) {
				if err := setBit(bm, bits, block); err != nil {
					return nil, err
				}
			} else if err := clearBit(bm, bits, block); err != nil {
				return nil, err
			}
		}
	}

	return &FsBitmap{
		Type:       types.FsTypeExt234,
		BitmapKind: BitmapFromFS,
		Bitmap:     bm,
		Bits:       bits,
		BlockSize:  int(sb.blockSize),
	}, nil
}

func (p *extBitmapParser) readSuper() (*extSuper, error) {
	buf := make([]byte, 1024)
	if _, err := p.fr.ReadAt(buf, extSuperOffset); err != nil && err != io.EOF {
		return nil, errors.Errorf("read ext superblock failed: %v", err)
	}
	sb := &extSuper{
		inodesCount:      binary.LittleEndian.Uint32(buf[0:4]),
		blocksCount:      uint64(binary.LittleEndian.Uint32(buf[4:8])),
		freeBlocksCount:  uint64(binary.LittleEndian.Uint32(buf[12:16])),
		firstDataBlock:   binary.LittleEndian.Uint32(buf[20:24]),
		logBlockSize:     binary.LittleEndian.Uint32(buf[24:28]),
		blocksPerGroup:   binary.LittleEndian.Uint32(buf[32:36]),
		magic:            binary.LittleEndian.Uint16(buf[56:58]),
		incompatFeatures: binary.LittleEndian.Uint32(buf[96:100]),
		descSize:         binary.LittleEndian.Uint16(buf[254:256]),
	}
	if sb.magic != extSuperMagic {
		return nil, errors.Errorf("invalid ext magic: 0x%x", sb.magic)
	}
	if sb.logBlockSize > 16 {
		return nil, errors.Errorf("invalid ext block size shift: %d", sb.logBlockSize)
	}
	sb.blockSize = 1024 << sb.logBlockSize
	if sb.blocksPerGroup == 0 || sb.blocksCount == 0 {
		return nil, errors.New("invalid ext block/group counts")
	}
	if sb.incompatFeatures&extFeatureIncompat64bit != 0 {
		sb.blocksCount |= uint64(binary.LittleEndian.Uint32(buf[336:340])) << 32
		sb.freeBlocksCount |= uint64(binary.LittleEndian.Uint32(buf[340:344])) << 32
		if sb.descSize < 64 {
			return nil, errors.Errorf("invalid ext64 descriptor size: %d", sb.descSize)
		}
	} else if sb.descSize == 0 {
		sb.descSize = 32
	}
	if sb.descSize < 32 {
		return nil, errors.Errorf("invalid ext descriptor size: %d", sb.descSize)
	}
	return sb, nil
}

type extGroupDesc struct {
	blockBitmap uint64
}

func (p *extBitmapParser) readGroupDescriptors(sb *extSuper, groupCount uint64) ([]extGroupDesc, error) {
	descTableBlock := uint64(2)
	if sb.blockSize > 1024 {
		descTableBlock = 1
	}
	totalBytes := groupCount * uint64(sb.descSize)
	buf := make([]byte, totalBytes)
	if _, err := p.fr.ReadAt(buf, int64(descTableBlock*sb.blockSize)); err != nil && err != io.EOF {
		return nil, errors.Errorf("read ext group descriptors failed: %v", err)
	}
	descs := make([]extGroupDesc, groupCount)
	for group := uint64(0); group < groupCount; group++ {
		off := group * uint64(sb.descSize)
		d := buf[off : off+uint64(sb.descSize)]
		blockBitmap := uint64(binary.LittleEndian.Uint32(d[0:4]))
		if len(d) >= 64 {
			blockBitmap |= uint64(binary.LittleEndian.Uint32(d[32:36])) << 32
		}
		descs[group] = extGroupDesc{blockBitmap: blockBitmap}
	}
	return descs, nil
}

func divRoundUp64(n, d uint64) uint64 {
	if d == 0 {
		return 0
	}
	return (n + d - 1) / d
}
