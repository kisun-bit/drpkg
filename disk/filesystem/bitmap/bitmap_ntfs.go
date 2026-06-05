package bitmap

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/kisun-bit/drpkg/disk/filesystem/types"
	"github.com/pkg/errors"
)

type ntfsBitmapParser struct {
	fr *fsRegionReader
}

func NewNtfsBitmapParser(device string, fsStart int64, fsSize int64) (FsBitmapParser, error) {
	fr, err := newFsRegionReader(device, fsStart, fsSize)
	if err != nil {
		return nil, err
	}
	return &ntfsBitmapParser{fr: fr}, nil
}

func (p *ntfsBitmapParser) String() string {
	return fmt.Sprintf("ntfsBitmapParser(%s)", p.fr)
}

func (p *ntfsBitmapParser) Dump() (*FsBitmap, error) {
	sb, err := p.readNTFSBoot()
	if err != nil {
		return nil, err
	}
	record, err := p.readMFTRecord(sb, 6)
	if err != nil {
		return nil, err
	}
	rawBitmap, err := p.extractNTFSBitmap(sb, record)
	if err != nil {
		return nil, err
	}
	bits := int64(sb.totalClusters)
	bm, err := bitmapBytes(bits)
	if err != nil {
		return nil, err
	}
	copy(bm, rawBitmap)
	return &FsBitmap{
		Type:       types.FsTypeNtfs,
		BitmapKind: BitmapFromFS,
		Bitmap:     bm,
		Bits:       bits,
		BlockSize:  int(sb.clusterSize),
	}, nil
}

type ntfsBoot struct {
	sectorSize    uint16
	clusterSize   uint64
	totalSectors  uint64
	totalClusters uint64
	mftLCN        uint64
	recordSize    uint32
}

func (p *ntfsBitmapParser) readNTFSBoot() (*ntfsBoot, error) {
	buf := make([]byte, 512)
	if _, err := p.fr.ReadAt(buf, 0); err != nil && err != io.EOF {
		return nil, errors.Errorf("read NTFS boot sector failed: %v", err)
	}
	if string(buf[3:11]) != "NTFS    " {
		return nil, errors.New("invalid NTFS OEM name")
	}
	sectorSize := binary.LittleEndian.Uint16(buf[11:13])
	sectorsPerCluster := buf[13]
	totalSectors := binary.LittleEndian.Uint64(buf[40:48])
	mftLCN := binary.LittleEndian.Uint64(buf[48:56])
	clustersPerRecord := int8(buf[64])
	if sectorSize == 0 || sectorsPerCluster == 0 || totalSectors == 0 {
		return nil, errors.New("invalid NTFS boot fields")
	}
	clusterSize := uint64(sectorSize) * uint64(sectorsPerCluster)
	var recordSize uint32
	if clustersPerRecord < 0 {
		recordSize = uint32(1) << uint8(-clustersPerRecord)
	} else {
		recordSize = uint32(clustersPerRecord) * uint32(clusterSize)
	}
	if recordSize == 0 {
		return nil, errors.New("invalid NTFS file record size")
	}
	return &ntfsBoot{
		sectorSize:    sectorSize,
		clusterSize:   clusterSize,
		totalSectors:  totalSectors,
		totalClusters: totalSectors / uint64(sectorsPerCluster),
		mftLCN:        mftLCN,
		recordSize:    recordSize,
	}, nil
}

func (p *ntfsBitmapParser) readMFTRecord(sb *ntfsBoot, index uint64) ([]byte, error) {
	off := sb.mftLCN*sb.clusterSize + index*uint64(sb.recordSize)
	record := make([]byte, sb.recordSize)
	if _, err := p.fr.ReadAt(record, int64(off)); err != nil && err != io.EOF {
		return nil, errors.Errorf("read NTFS MFT record %d failed: %v", index, err)
	}
	if string(record[0:4]) != "FILE" {
		return nil, errors.Errorf("invalid NTFS MFT record %d signature", index)
	}
	if err := applyNTFSFixup(record, int(sb.sectorSize)); err != nil {
		return nil, err
	}
	return record, nil
}

func applyNTFSFixup(record []byte, sectorSize int) error {
	if sectorSize <= 0 || len(record)%sectorSize != 0 {
		return errors.New("invalid NTFS fixup sector size")
	}
	usaOff := int(binary.LittleEndian.Uint16(record[4:6]))
	usaCount := int(binary.LittleEndian.Uint16(record[6:8]))
	if usaOff <= 0 || usaOff+usaCount*2 > len(record) || usaCount == 0 {
		return errors.New("invalid NTFS update sequence array")
	}
	seq := binary.LittleEndian.Uint16(record[usaOff : usaOff+2])
	for i := 1; i < usaCount; i++ {
		sectorEnd := i*sectorSize - 2
		if sectorEnd+2 > len(record) {
			return errors.New("NTFS fixup exceeds record")
		}
		if binary.LittleEndian.Uint16(record[sectorEnd:sectorEnd+2]) != seq {
			return errors.New("NTFS fixup sequence mismatch")
		}
		copy(record[sectorEnd:sectorEnd+2], record[usaOff+i*2:usaOff+i*2+2])
	}
	return nil
}

func (p *ntfsBitmapParser) extractNTFSBitmap(sb *ntfsBoot, record []byte) ([]byte, error) {
	attrOff := int(binary.LittleEndian.Uint16(record[20:22]))
	for attrOff+16 <= len(record) {
		attrType := binary.LittleEndian.Uint32(record[attrOff : attrOff+4])
		if attrType == 0xffffffff {
			break
		}
		attrLen := int(binary.LittleEndian.Uint32(record[attrOff+4 : attrOff+8]))
		if attrLen <= 0 || attrOff+attrLen > len(record) {
			return nil, errors.New("invalid NTFS attribute length")
		}
		if attrType != 0x80 {
			attrOff += attrLen
			continue
		}
		nonResident := record[attrOff+8] != 0
		if !nonResident {
			size := int(binary.LittleEndian.Uint32(record[attrOff+16 : attrOff+20]))
			dataOff := int(binary.LittleEndian.Uint16(record[attrOff+20 : attrOff+22]))
			if dataOff+size > attrLen {
				return nil, errors.New("invalid resident NTFS $Bitmap DATA")
			}
			return append([]byte(nil), record[attrOff+dataOff:attrOff+dataOff+size]...), nil
		}
		runOff := int(binary.LittleEndian.Uint16(record[attrOff+32 : attrOff+34]))
		dataSize := binary.LittleEndian.Uint64(record[attrOff+48 : attrOff+56])
		if runOff <= 0 || runOff >= attrLen {
			return nil, errors.New("invalid NTFS runlist offset")
		}
		return p.readNTFSRunlist(sb, record[attrOff+runOff:attrOff+attrLen], dataSize)
	}
	return nil, errors.New("NTFS $Bitmap DATA attribute not found")
}

func (p *ntfsBitmapParser) readNTFSRunlist(sb *ntfsBoot, runlist []byte, dataSize uint64) ([]byte, error) {
	out := make([]byte, 0, dataSize)
	var lcn int64
	for i := 0; i < len(runlist) && runlist[i] != 0; {
		header := runlist[i]
		i++
		lenBytes := int(header & 0x0f)
		offBytes := int(header >> 4)
		if lenBytes == 0 || lenBytes > 8 || offBytes > 8 || i+lenBytes+offBytes > len(runlist) {
			return nil, errors.New("invalid NTFS runlist")
		}
		runLen := decodeUnsignedLE(runlist[i : i+lenBytes])
		i += lenBytes
		lcn += decodeSignedLE(runlist[i : i+offBytes])
		i += offBytes
		if runLen == 0 {
			continue
		}
		if lcn < 0 {
			return nil, errors.New("NTFS sparse $Bitmap run is invalid")
		}
		size := runLen * sb.clusterSize
		chunk := make([]byte, size)
		if _, err := p.fr.ReadAt(chunk, int64(uint64(lcn)*sb.clusterSize)); err != nil && err != io.EOF {
			return nil, errors.Errorf("read NTFS $Bitmap run failed: %v", err)
		}
		out = append(out, chunk...)
		if uint64(len(out)) >= dataSize {
			return out[:dataSize], nil
		}
	}
	if uint64(len(out)) < dataSize {
		return nil, errors.Errorf("NTFS $Bitmap shorter than declared: %d < %d", len(out), dataSize)
	}
	return out[:dataSize], nil
}

func decodeUnsignedLE(b []byte) uint64 {
	var v uint64
	for i := range b {
		v |= uint64(b[i]) << (8 * i)
	}
	return v
}

func decodeSignedLE(b []byte) int64 {
	if len(b) == 0 {
		return 0
	}
	u := decodeUnsignedLE(b)
	shift := uint(64 - len(b)*8)
	return int64(u<<shift) >> shift
}
