package lvm2meta

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/pkg/errors"
	"io"
	"log"
	"os"
)

func NewPhysicalVolume(dev string) (*PhysicalVolume, error) {
	if dev == "" {
		return nil, errors.New("nil device")
	}
	blksize, err := extend.DevBlockSize(dev)
	if err != nil {
		return nil, errors.Wrapf(err, "read blocksize for %s", dev)
	}
	phyBlkSize, err := extend.DevPhysicalBlockSize(dev)
	if err != nil {
		return nil, errors.Wrapf(err, "read physical-blocksize for %s", dev)
	}
	fd, err := os.Open(dev)
	if err != nil {
		return nil, errors.Wrapf(err, "open %s", dev)
	}
	defer fd.Close()
	pv := &PhysicalVolume{
		device:       dev,
		deviceHandle: fd,
		BlkSize:      blksize,
		PhyBlkSize:   phyBlkSize,
	}
	if blksize < 4096 {
		fmt.Printf("BlkSize: Change from %v to %v\n", blksize, 4096)
		pv.BlkSize = 4096
	}
	fmt.Println("BlkSize:", blksize)
	fmt.Println("PhyBlkSize:", phyBlkSize)

	b, err := pv.ReadBlock(0)
	if err != nil {
		return nil, errors.Wrapf(err, "fail to read block 0")
	}
	// 加入元数据块（label）.
	pv.MetadataBlocks = append(pv.MetadataBlocks, MetaDataBlock{
		Offset: 0,
		Length: len(b),
		Bytes:  b,
	})

	reader := bytes.NewReader(b)
	labelHeader, err := pv.ReadLabelHeader(reader)
	if err != nil {
		return nil, errors.Wrapf(err, "fail to read label Header")
	}
	if crc32checksum, ok := labelHeader.CheckCRC32(b[labelHeader.Sector*SectorSize:]); !ok {
		log.Printf("Fail to check label Header checksum, got %v, expected: %v", crc32checksum, labelHeader.CRC)
		// FIXME： 严格意义上讲这里要返回错误.
	}
	pv.LabelHeader = labelHeader

	pvHeader, err := pv.ReadHeader(reader)
	if err != nil {
		return nil, errors.Wrapf(err, "fail to read pv Header")
	}
	pv.Header = pvHeader

	pvHeaderExt, err := pv.ReadHeaderExt(reader)
	if err != nil {
		return nil, errors.Wrapf(err, "fail to reade pv Header ext")
	}
	pv.HeaderExt = pvHeaderExt

	metadataHeaders, err := pv.ReadMetadataHeaders()
	if err != nil {
		return nil, errors.Wrapf(err, "fail to read metadata Header areas")
	}
	pv.MetadataHeaders = metadataHeaders

	fmt.Println("Label-Header: ", labelHeader)
	fmt.Println("PV-Header: ", pvHeader)
	fmt.Println("PV-Header-Ext: ", pvHeaderExt)
	fmt.Println("Metadata-header: ", metadataHeaders)
	fmt.Println()

	for _, ma := range pv.Header.MetadataAreas {
		fmt.Printf("### metadata-area: start=%v size=%v\n", ma.Offset, ma.Size)
	}
	for _, da := range pv.Header.DiskAreas {
		fmt.Printf("### disk-area: start=%v size=%v\n", da.Offset, da.Size)
	}
	fmt.Println()

	for _, h := range metadataHeaders {
		m, err := pv.ReadMetadata(h)
		if err != nil {
			return nil, errors.Wrapf(err, "fail to read pv metadata")
		}
		_ = m
		fmt.Println("NewPhysicalVolume: ", h.String(), "\n", string(m))
	}

	return pv, nil
}

func (pv *PhysicalVolume) ReadBlock(offset uint64) ([]byte, error) {
	block := make([]byte, pv.BlkSize)
	if _, err := pv.deviceHandle.ReadAt(block, int64(offset)); err != nil {
		return nil, errors.Wrapf(err, "fail to read from device")
	}
	return block, nil
}

func (h PhysicalVolumeHeader) UUIDToString() string {
	buf := bytes.NewBuffer(make([]byte, 0, 38))
	buf.Write(h.UUID[0:6])
	buf.Write([]byte{'-'})
	buf.Write(h.UUID[6:10])
	buf.Write([]byte{'-'})
	buf.Write(h.UUID[14:18])
	buf.Write([]byte{'-'})
	buf.Write(h.UUID[18:22])
	buf.Write([]byte{'-'})
	buf.Write(h.UUID[22:26])
	buf.Write([]byte{'-'})
	buf.Write(h.UUID[26:32])
	return buf.String()
}

func (h PhysicalVolumeHeader) String() string {
	return fmt.Sprintf(
		"PVHeader[ID:%s Size:%d DiskAreas:%d MetadataAreas:%d]",
		h.UUIDToString(), h.DeviceSize, len(h.DiskAreas), len(h.MetadataAreas),
	)
}

func (ext PhysicalVolumeHeaderExtension) String() string {
	return fmt.Sprintf("Ext[Version:%d Flags:%d, BootloaderAreas:%d]", ext.Version, ext.Flags, len(ext.BooloaderAreas))
}

func (pv *PhysicalVolume) ReadHeaderExt(reader io.Reader) (PhysicalVolumeHeaderExtension, error) {
	var ext PhysicalVolumeHeaderExtension
	err := binary.Read(reader, binary.LittleEndian, &(ext.StaticPhysicalVolumeHeaderExtension))
	if err != nil {
		return ext, errors.Wrapf(err, "fail to parse pv Header extension")
	}

	areas, err := ReadDataAreaDescriptorList(reader)
	if err != nil {
		return ext, errors.Wrapf(err, "fail to parse data area descriptors for bootloader areas")
	}
	ext.BooloaderAreas = areas

	return ext, nil
}

func (pv *PhysicalVolume) ReadHeader(reader io.Reader) (PhysicalVolumeHeader, error) {
	var header PhysicalVolumeHeader
	err := binary.Read(reader, binary.LittleEndian, &(header.StaticPhysicalVolumeHeader))
	if err != nil {
		return header, errors.Wrapf(err, "fail to parse pv Header")
	}

	areas, err := ReadDataAreaDescriptorList(reader)
	if err != nil {
		return header, errors.Wrapf(err, "fail to parse data area descriptors for disk areas")
	}
	header.DiskAreas = areas

	areas, err = ReadDataAreaDescriptorList(reader)
	if err != nil {
		return header, errors.Wrapf(err, "fail to parse data area descriptors for metadata areas")
	}
	header.MetadataAreas = areas

	return header, nil
}

func (pv *PhysicalVolume) ReadLabelHeader(reader io.Reader) (LabelHeader, error) {
	var header LabelHeader

	for sector := 0; sector < LabelScanSectors; sector++ {
		err := binary.Read(reader, binary.LittleEndian, &header)
		if err != nil {
			log.Println("error reading error: ", err, "continuing..")
			continue
		}

		if string(header.ID[:]) == LabelID {
			if header.Sector != uint64(sector) {
				return header, errors.Errorf("Header sector does not match: (%v, expected: %v)", header.Sector, sector)
			}
			break
		} else {
			toNextSector := make([]byte, SectorSize-LabelHeaderSize)
			_, err := reader.Read(toNextSector)
			if err != nil {
				return header, errors.Wrapf(err, "fail to read to next sector")
			}
		}
	}
	if header.Sector == 0 {
		return header, errors.New("label Header not found")
	}
	return header, nil
}
