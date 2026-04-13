package table

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"github.com/kisun-bit/drpkg/extend"
	"github.com/lunixbochs/struc"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
)

// MBR MBR磁盘信息结构.
// 具体见 https://en.wikipedia.org/wiki/Master_boot_record.
// 参考现代MBR中关于结构节(`Structure of a modern standard MBR`)的描述
type MBR struct {
	Path string `struc:"skip"`

	// disk MBR设备文件读句柄
	disk io.ReadSeeker `struc:"skip"`
	// isEBR 若此MBR为EBR时, 此字段为true
	isEBR bool `struc:"skip"`

	// SectorSize 扇区大小（单位：字节）
	SectorSize int    `struc:"skip"`
	Signature  []byte `struc:"skip"`
	// Offset MBR或EBR的签名起始偏移位置
	Offset int64 `struc:"skip"`
	// Bin MBR二进制数据（长度512字节）
	// 若您有备份磁盘需求，请备份此字段
	Bin []byte `struc:"skip"`

	// BootLoader 起始0x0000, 长度为446字节
	BootLoader []byte `struc:"[446]byte"`
	// MainPartitionEntries 0x01BE, 长度为64字节, 所有多字节字段均为小端序
	MainPartitionEntries [4]MBRPartition
	// TailSignature 起始0x01FE，长度问哦2字节
	TailSignature [2]byte `struc:"[2]byte"`
}

// EBR MBR磁盘扩展BootRecorder信息结构.
// 具体见 https://en.wikipedia.org/wiki/Extended_boot_record.
// 值得说明的是, 有以下几点:
//  1. 每一个逻辑分区，均持有一个EBR, 且EBR都位于它所描述的逻辑分区之前.
//  2. EBR 的 BootLoader 一般未使用; 通常使用0填充, 但也有可能包含一个引导加载程序.
//  3. EBR 的 分区表项共4个表项, 其中前1、2表项在使用, 后3、4表项未使用.
//  4. EBR 的 第1个表项表示相对起始扇区, StartingLBA=该EBR扇区与逻辑分区第一个扇区之间的"相对"偏移量(
//     绝对偏移量 = 扩展分区的绝对偏移量 + 此EBR之前所有的EBR的相对偏移量之和), TotalSectors=逻辑分区总扇区数.
//     (lba = Offset from ebr, sectors = size of partition).
//  5. EBR 的 第2个表项表示此EBR的下一个EBR, StartingLBA=扩展分区内下一个EBR的相对地址,
//     TotalSectors=下一个逻辑分区的 EBR 起始扇区 到 逻辑分区结束扇区 这之间的扇区总数.
//     (lba = Offset from extended partition, sectors = next EBR + next PartitionIndex size).
type EBR = MBR

// MBRPartition MBR磁盘的主分区表项结构.
type MBRPartition struct {
	// Index 这一索引仅仅代表分区在分区表中的索引位置，
	// 注意！！！在Linux系统可代表应用层所展现的设备的尾部编号(也可叫分区ID)，在windows不能代表应用层所展现的设备的分区ID.
	Index int `struc:"skip"`
	// isLogical 若为逻辑分区, 此字段为true
	IsLogical bool `struc:"skip"`

	// BootIndicator 0x00, 1
	BootIndicator byte
	// StartingHead 0x01, 1
	StartingHead byte
	// StartingSector 0x02, 1, bit0-5表示起始扇区, bit6-7位表示起始柱面的高位
	StartingSector byte
	// StartingCylinder 0x03, 1, (StartingSector-bit6-7 + StartingCylinder-bit0-8)表示起始柱面号
	StartingCylinder byte
	// PartitionType 0x04, 1. 见 https://en.wikipedia.org/wiki/Partition_type
	PartitionType byte `struc:"byte"`
	// EndingHead 0x05, 1
	EndingHead byte
	// EndingSector 0x06, 1, bit0-5表示结束扇区, bit6-7位表示结束柱面的高位
	EndingSector byte
	// EndingCylinder 0x07, 1, (EndingSector-bit6-7 + EndingCylinder-bit0-8)表示起始柱面号
	EndingCylinder byte
	// StartingLBA 0x08, 4, 起始LBA(包含)
	StartingLBA int64 `struc:"uint32,little"`
	// TotalSectors 0x0c, 4, 总扇区数
	TotalSectors int64 `struc:"uint32,little"`
}

func NewMBR(disk string, start int64, isEBR bool) (mbr *MBR, err error) {
	ss, err := extend.BytesPerSector(disk)
	if err != nil {
		return nil, err
	}
	fp, err := os.Open(disk)
	if err != nil {
		return nil, errors.Errorf("open disk fail: %s", err)
	}
	mbr, err = newMBR(fp, start, isEBR, ss)
	if err != nil {
		_ = fp.Close()
		return nil, err
	}
	mbr.Path = disk
	return mbr, nil
}

// newMBR 解析磁盘并获取一个MBR结构信息.
func newMBR(disk io.ReadSeeker, start int64, isEBR bool, sectorSize int) (mbr *MBR, err error) {
	mbr = new(MBR)

	mbr.disk = disk
	mbr.isEBR = isEBR
	mbr.Offset = start
	mbr.SectorSize = sectorSize
	mbr.Bin = make([]byte, sectorSize)

	if sectorSize <= 0 {
		return nil, errors.New("invalid sector size")
	}
	if _, err = disk.Seek(start, io.SeekStart); err != nil {
		return mbr, errors.Errorf("seek fail: %s", err)
	}
	if _, err = io.ReadFull(disk, mbr.Bin); err != nil {
		return mbr, errors.Errorf("read disk fail: %s", err)
	}
	if err = struc.Unpack(bytes.NewReader(mbr.Bin), &mbr); err != nil {
		return mbr, errors.Errorf("unpack mbr fail: %s", err)
	}
	if !(mbr.TailSignature[0] == 0x55 && mbr.TailSignature[1] == 0xAA) {
		return mbr, errors.New("invalid tail signature for mbr")
	}

	for mpi := 1; mpi <= 4; mpi++ {
		mbr.MainPartitionEntries[mpi-1].Index = mpi
	}

	signature := mbr.BootLoader[440 : 440+4]
	for i := 3; i >= 0; i-- {
		mbr.Signature = append(mbr.Signature, signature[i])
	}

	return mbr, nil
}

func newEBR(disk io.ReadSeeker, start int64, sectorSize int) (ebr *EBR, err error) {
	return newMBR(disk, start, true, sectorSize)
}

func (mbr *MBR) Close() error {
	if extend.IsNilType(mbr.disk) {
		return nil
	}
	if fd, ok := mbr.disk.(*os.File); ok {
		return fd.Close()
	}
	return nil
}

func (mbr *MBR) Identifier() string {
	return hex.EncodeToString(mbr.Signature)
}

func (mbr *MBR) Size() (int64, error) {
	size, err := extend.FileSize(mbr.Path)
	return int64(size), err
}

func (mbr *MBR) ContainsProtectiveMBR() bool {
	for _, p := range mbr.MainPartitionEntries {
		if p.Type() == MBR_GPT_PARTITION {
			return true
		}
	}
	return false
}

func (mbr *MBR) ContainsBootFlag() bool {
	for _, p := range mbr.MainPartitionEntries {
		if p.BootIndicator == 0x80 {
			return true
		}
	}
	return false
}

func (mbr *MBR) EBRList() ([]*EBR, error) {
	if mbr.isEBR {
		return nil, errors.New("EBR has no EBR list")
	}
	EBRs := make([]*EBR, 0)
	for _, DPT := range mbr.MainPartitionEntries {
		if !DPT.IsExtend() {
			continue
		}
		EBROffset := DPT.StartingLBA * int64(mbr.SectorSize)
		for EBRIndex := 1; ; EBRIndex++ {
			ebr, err := newEBR(mbr.disk, EBROffset, mbr.SectorSize)
			if err != nil {
				//log.Printf("EBR can not be parsed at Offset(%v): %v\n", EBROffset, err)
				break
			}
			EBRs = append(EBRs, ebr)
			// EBR首个分区表项是数据分区的表项，次个分区表项是下一个EBR的地址
			if !ebr.MainPartitionEntries[1].IsExtend() {
				break
			}
			EBROffset = DPT.StartingLBA*int64(mbr.SectorSize) +
				ebr.MainPartitionEntries[1].StartingLBA*int64(mbr.SectorSize)
		}
	}
	return EBRs, nil
}

// LogicalPartitionEntries 获取所有逻辑分区表项
func (mbr *MBR) LogicalPartitionEntries() ([]MBRPartition, error) {
	if mbr.isEBR {
		return nil, errors.New("EBR has no logical partitions")
	}
	EBRs, err := mbr.EBRList()
	if err != nil {
		return nil, err
	}
	if len(EBRs) == 0 {
		return nil, nil
	}
	index := mbr.indexExtendMainPartition()
	if index == -1 {
		return nil, errors.New("failed to fetch the index of main extend partition")
	}
	lps := make([]MBRPartition, 0)
	lastEBRStartingLBA := mbr.MainPartitionEntries[index].StartingLBA
	for lpi, EBR_ := range EBRs {
		lpi++
		// EBR首个分区表项是数据分区的表项，次个分区表项是下一个EBR的地址
		peData := EBR_.MainPartitionEntries[0].Copy()
		peEBR := EBR_.MainPartitionEntries[0].Copy()
		// 修正相对LBA偏移为绝对LBA偏移.
		peData.StartingLBA += lastEBRStartingLBA
		peData.IsLogical = true
		peData.Index = lpi + 4
		if peData.Type() != MBR_EMPTY_PARTITION {
			lps = append(lps, *peData)
		}
		if peEBR.IsExtend() {
			lastEBRStartingLBA = peEBR.StartingLBA + mbr.MainPartitionEntries[index].StartingLBA
		}
	}
	return lps, nil
}

func (mbr *MBR) indexExtendMainPartition() int {
	if mbr.isEBR {
		return -1
	}
	for i, p := range mbr.MainPartitionEntries {
		if p.IsExtend() {
			return i
		}
	}
	return -1
}

func (part *MBRPartition) Type() PartType {
	return fmt.Sprintf("%02x", part.PartitionType)
}

func (part *MBRPartition) Description() string {
	if desc, ok := TypeDescMapping[part.Type()]; ok {
		return desc
	}
	return "Unknown"
}

func (part *MBRPartition) IsExtend() bool {
	return funk.InStrings(MBRExtendPartTypes, part.Type())
}

func (part *MBRPartition) Copy() *MBRPartition {
	np := new(MBRPartition)
	np.Index = part.Index
	np.IsLogical = part.IsLogical
	np.BootIndicator = part.BootIndicator
	np.StartingHead = part.StartingHead
	np.StartingSector = part.StartingSector
	np.PartitionType = part.PartitionType
	np.EndingHead = part.EndingHead
	np.EndingSector = part.EndingSector
	np.EndingCylinder = part.EndingCylinder
	np.StartingLBA = part.StartingLBA
	np.TotalSectors = part.TotalSectors
	return np
}
