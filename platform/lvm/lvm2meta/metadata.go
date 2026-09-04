package lvm2meta

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"github.com/pkg/errors"
	"io"
	"log"
)

func (h MetadataHeader) String() string {
	return fmt.Sprintf(
		"MetadataHeader[CRC:%d Signature:%s Version:%d Offset:%d Size:%d Locations:%d]",
		h.CRC, string(h.Signature[:]), h.Version, h.Offset, h.Size, len(h.Locations),
	)
}

func (h MetadataHeader) CheckCRC32(block []byte) (uint32, bool) {
	headerBytes := block[4:MetadataHeaderSize]
	crc32checksum := Calc(InitialCRC, headerBytes)
	return crc32checksum, crc32checksum == h.CRC
}

func (pv *PhysicalVolume) ReadMetadataHeaders() ([]MetadataHeader, error) {
	var headers []MetadataHeader

	for _, area := range pv.Header.MetadataAreas {
		b := make([]byte, area.Size)
		n, e := pv.deviceHandle.ReadAt(b, int64(area.Offset))
		if e != nil {
			return nil, e
		}
		if uint64(n) != area.Size {
			return nil, errors.Errorf("area of metadata dis-matched, expected to be %s, actual to be %s",
				area.Size, uint64(n))
		}
		// 加入元数据块（metadata区域）.
		pv.MetadataBlocks = append(pv.MetadataBlocks, MetaDataBlock{
			Offset: int64(area.Offset),
			Length: len(b),
			Bytes:  b,
		})
	}

	for _, area := range pv.Header.MetadataAreas {
		b, err := pv.ReadBlock(area.Offset)
		if err != nil {
			return nil, errors.Wrapf(err, "fail to read device block for metadata area")
		}
		reader := bytes.NewReader(b)

		header, err := pv.ReadMetadataHeader(reader)
		if err != nil {
			return nil, errors.Wrapf(err, "fail to read metadata Header")
		}

		crc32checksum, ok := header.CheckCRC32(b)
		if !ok {
			log.Println("Fail to check metadata Header checksum, got", crc32checksum, "expected", header.CRC)
		}
		headers = append(headers, header)
	}
	return headers, nil
}

func (pv *PhysicalVolume) ReadMetadataHeader(reader io.Reader) (MetadataHeader, error) {
	var header MetadataHeader
	err := binary.Read(reader, binary.LittleEndian, &(header.StaticMetadataHeader))
	if err != nil {
		return header, errors.Wrapf(err, "fail to parse pv Header")
	}

	locations, err := ReadRawLocationDescriptorList(reader)
	if err != nil {
		return header, errors.Wrapf(err, "fail to parse data area descriptors for disk areas")
	}
	header.Locations = locations
	return header, nil
}

func (pv *PhysicalVolume) ReadMetadata(h MetadataHeader) ([]byte, error) {
	for _, loc := range h.Locations {
		off := h.Offset + loc.Offset
		block, err := pv.ReadBlock(off)
		if err != nil {
			return nil, errors.Wrapf(err, "fail to read block")
		}
		fmt.Println("metadata: ", off)
		return block, nil
	}
	return nil, nil
}
