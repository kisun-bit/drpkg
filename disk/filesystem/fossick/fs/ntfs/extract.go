package ntfs

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"github.com/kisun-bit/drpkg/util/logger"
	"github.com/pkg/errors"
	"io"
	"os"
)

const bitmapMFTIndex = 6

// Extract 导出含有NTFS文件系统的设备的位图.
func Extract(device string) (clusterSize int, bitmapBinary []byte, err error) {
	logger.Debugf("NTFS Extract(%s). Enter", device)

	deviceHandle, err := os.Open(device)
	if err != nil {
		return 0, nil, err
	}
	defer func() {
		_ = deviceHandle.Close()
	}()

	bh, err := bootHeader(deviceHandle)
	if err != nil {
		return 0, nil, errors.Errorf("failed to parse boot header, %v", err)
	}
	logger.Debugf("NTFS Extract(%s). boot header is\n%s", device, bh.DebugString())
	mft, err := getMFTEntry(deviceHandle, bh, bitmapMFTIndex)
	if err != nil {
		return 0, nil, errors.Errorf("failed to parse bitmap MFT(%v)", bitmapMFTIndex)
	}

	// 校验$30属性.
	AttrFileName := mft.FindAttributes(AttributeTypeFileName)
	logger.Debugf("NTFS Extract(%s). $30 %s, length of attrs is %v ",
		device, AttributeTypeFileName.Name(), len(AttrFileName))
	for _, a := range AttrFileName {
		fn, err := ParseFileName(a.Data)
		if err != nil {
			return 0, nil, errors.Errorf("failed to parse filename of bitmap attr")
		}
		if fn.Name != "$Bitmap" {
			return 0, nil, errors.New("invalid bitmap MFT, name is not $Bitmap")
		}
	}

	// 解析$80属性.
	AttrDatas := mft.FindAttributes(AttributeTypeData)
	logger.Debugf("NTFS Extract(%s). $80 %s, length of attrs is %v ",
		device, AttributeTypeData.Name(), len(AttrDatas))
	if len(AttrDatas) == 0 {
		return 0, nil, errors.New("ntfs lack $80 attr")
	}
	AttrData := AttrDatas[0]

	ds, err := ParseDataRuns(AttrData.Data)
	if err != nil {
		return 0, nil, errors.New("failed to parse dataruns")
	}
	segs := DataRunsToFragments(ds, bh.ClusterSize())

	var count int64 = 0
	var bitmapBuffer bytes.Buffer

	for _, oneSeg := range segs {
		if oneSeg.Length == 0 {
			continue
		}
		logger.Debugf("NTFS Extract(%s). seg(offset=%v,length=%v)",
			device, oneSeg.Offset, oneSeg.Length)
		data80 := io.NewSectionReader(deviceHandle, oneSeg.Offset, oneSeg.Length)
		bitmapContent := make([]byte, oneSeg.Length)
		_, err = io.ReadFull(data80, bitmapContent)
		if err != nil && err != io.EOF {
			return 0, nil, errors.Errorf(
				"failed to read bitmap content from seg(offset=%v,length=%v)",
				oneSeg.Offset, oneSeg.Length)
		}
		bitmapBuffer.Write(bitmapContent)
		count += oneSeg.Length
	}
	logger.Debugf("ntfs bitmap size: %v", count)

	return bh.ClusterSize(), bitmapBuffer.Bytes(), nil
}

func getMFTEntry(r io.ReadSeeker, bh BootHeader, index int) (me MFTEntry, err error) {
	MftEntrySize, _ := bh.BytesPerFileRecordSegment()
	MftStartOffset := bh.MFTClusterStartNo * int64(bh.ClusterSize())
	_, err = r.Seek(MftStartOffset+int64(index*MftEntrySize), io.SeekStart)
	if err != nil {
		return me, err
	}
	MFTEntryData := make([]byte, MftEntrySize)
	nr, err := io.ReadFull(r, MFTEntryData)
	if err != nil {
		return me, err
	}
	if nr != MftEntrySize {
		err = fmt.Errorf("expected a least %d bytes but got %d", MftEntrySize, nr)
		return
	}
	return ParseMFTEntry(MFTEntryData)
}

func bootHeader(r io.ReadSeeker) (h BootHeader, err error) {
	boot := make([]byte, 512)
	_, err = r.Seek(0, io.SeekStart)
	if err != nil {
		return h, err
	}
	_, err = r.Read(boot)
	if err != nil {
		return h, err
	}
	bootBuf := bytes.NewBuffer(boot)
	read := func(data interface{}) {
		if err == nil {
			err = binary.Read(bootBuf, binary.LittleEndian, data)
		}
	}
	read(&h.JMP)
	read(&h.OEM)
	read(&h.BytesPerSector)
	read(&h.SectorsPerCluster)
	read(&h.RetainSectors)
	read(&h.Unused0x10)
	read(&h.Unused0x13)
	read(&h.MediaDesc)
	read(&h.Unused0x16)
	read(&h.SectorsPerTrack)
	read(&h.NumberOfHeads)
	read(&h.HiddenSectors)
	read(&h.Unused0x20)
	read(&h.Unused0x24)
	read(&h.TotalSectors)
	read(&h.MFTClusterStartNo)
	read(&h.MFTMirrClusterStartNo)
	read(&h.BytesOrClustersPerRecord)
	read(&h.Unused0x41)
	read(&h.ClustersPerIndexBuffer)
	read(&h.Unused0x45)
	read(&h.VolumeSerialNumber)
	read(&h.Checksum)
	read(&h.BootstrapCode)
	read(&h.EndMarker)
	if err != nil {
		return h, err
	}
	err = h.Check()
	return h, err
}
