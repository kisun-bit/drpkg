package ntfs

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// BootHeader
// @Description: NTFS分区的启动扇区的字节数据分块
type BootHeader struct {
	JMP                      [3]byte   // 0x00   | JMP 指令
	OEM                      [8]byte   // 0x03   | OEM 标
	BytesPerSector           int16     // 0x0B   | 每扇区字节数
	SectorsPerCluster        uint8     // 0x0D   | 每簇扇区数
	RetainSectors            int16     // 0x0E   | 保留扇区数
	Unused0x10               [3]byte   // 0x10   | --
	Unused0x13               [2]byte   // 0x13   | --
	MediaDesc                int8      // 0x15   | 介质描述符
	Unused0x16               [2]byte   // 0x16   | --
	SectorsPerTrack          [2]byte   // 0x18   | --
	NumberOfHeads            int16     // 0x1A   | 磁头数
	HiddenSectors            int32     // 0x1C   | 隐藏扇区数
	Unused0x20               [4]byte   // 0x20   | --
	Unused0x24               [4]byte   // 0x24   | --
	TotalSectors             int64     // 0x28   | 总扇区数
	MFTClusterStartNo        int64     // 0x30   | $MFT簇号
	MFTMirrClusterStartNo    int64     // 0x38   | $MFTMirr簇号
	BytesOrClustersPerRecord [1]byte   // 0x40   | 每一项文件记录的字节/簇数, 正数为簇数, 如果为负数,则表示2的-value幂字节
	Unused0x41               [3]byte   // 0x41   | --
	ClustersPerIndexBuffer   int8      // 0x44   | 每个索引缓冲占据的簇数目
	Unused0x45               [3]byte   // 0x45   | --
	VolumeSerialNumber       [8]byte   // 0x48   | 卷序列号
	Checksum                 [4]byte   // 0x50   | 校验和,未使用
	BootstrapCode            [426]byte // 0x54   | 启动指令码
	EndMarker                uint16    // 0x01FE | 扇区结束标记
}

func (bh *BootHeader) DebugString() string {
	result := fmt.Sprintf("(0x00, 3)JMP:%#0x\n", bh.JMP)
	result += fmt.Sprintf("(0x03, 8)OEM:%s\n", string(bh.OEM[:]))
	result += fmt.Sprintf("(0x0B, 2)Bytes Per Sector:%d\n", bh.BytesPerSector)
	result += fmt.Sprintf("(0x0D, 1)Sectors Per Cluster:%d\n", bh.SectorsPerCluster)
	result += fmt.Sprintf("(0x0E, 2)Rev Sectors:%d\n", bh.RetainSectors)
	result += fmt.Sprintf("(0x15, 1)Media Desc:%#0x\n", bh.MediaDesc)
	result += fmt.Sprintf("(0x1A, 2)Heads:%d\n", bh.NumberOfHeads)
	result += fmt.Sprintf("(0x1C, 4)Hidden Sectors:%d\n", bh.HiddenSectors)
	result += fmt.Sprintf("(0x28, 8)Total Sectors:%d\n", bh.TotalSectors)
	result += fmt.Sprintf("(0x30, 8)$MFT Cluster:%d\n", bh.MFTClusterStartNo)
	result += fmt.Sprintf("(0x38, 8)$MFTMirr Cluster:%d\n", bh.MFTMirrClusterStartNo)
	result += fmt.Sprintf("(0x40, 1)BytesOrClustersPerRecord:%#0x\n", bh.BytesOrClustersPerRecord)
	result += fmt.Sprintf("(0x44, 1)bh.ClustersPerIndexBuffer:%d\n", bh.ClustersPerIndexBuffer)
	result += fmt.Sprintf("(0x48, 8)Serial:%0x\n", bh.VolumeSerialNumber)
	result += fmt.Sprintf("(0x50, 4)CheckSum:%0x\n", bh.Checksum)
	result += fmt.Sprintf("(0x54, 426)Bootstrap:%0x\n", bh.BootstrapCode)
	result += fmt.Sprintf("(0x01FE, 2)End Marker:%#0x", bh.EndMarker)
	return result
}

func (bh *BootHeader) BytesPerFileRecordSegment() (bss int, err error) {
	buffer := bytes.NewReader(bh.BytesOrClustersPerRecord[:])

	var value int8
	if err = binary.Read(buffer, binary.LittleEndian, &value); err != nil {
		return bss, err
	}

	if value > 0 {
		bss = int(bh.SectorsPerCluster) * int(bh.BytesPerSector) * int(value)
	} else if value < 0 {
		bss = IntPow(2, int(-value))
	} else {
		return 0, errors.New("invalid length of file record segment")
	}
	return bss, err
}

func (bh *BootHeader) TotalClusters() int64 {
	return int64(bh.TotalSectors) / int64(bh.SectorsPerCluster)
}

func (bh *BootHeader) ClusterSize() int {
	return int(bh.BytesPerSector) * int(bh.SectorsPerCluster)
}

func (bh *BootHeader) Check() (err error) {
	if bh.EndMarker != 0xAA55 {
		err = errors.New("invalid End-of-sector Marker at 0x01FE")
		return
	}
	switch bh.ClusterSize() {
	case 0x01, 0x02, 0x04, 0x08, 0x10, 0x20, 0x40, 0x80, 0x100, 0x200, 0x400, 0x800, 0x1000, 0x2000, 0x4000, 0x8000,
		0x10000:
		break
	default:
		err = fmt.Errorf("invalid cluster size: %d", bh.ClusterSize())
		return
	}

	if bh.BytesPerSector == 0 || bh.BytesPerSector%512 != 0 {
		err = fmt.Errorf("invalid bytes per sector: %d", bh.BytesPerSector)
	}

	if bh.TotalClusters() == 0 {
		err = errors.New("cluster number is 0")
		return
	}
	return
}

func IntPow(x int, y int) (start int) {
	start = 1
	for i := 0; i < y; i++ {
		start *= x
	}
	return start
}

func ParseBootHeader(reader io.Reader) (h BootHeader, err error) {
	boot := make([]byte, 512)
	_, err = reader.Read(boot)
	if err != nil {
		fmt.Println(1)
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
