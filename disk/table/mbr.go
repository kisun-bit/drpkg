package table

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/dustin/go-humanize"
	"github.com/kisun-bit/drpkg/sys/ioctl"
	"github.com/lunixbochs/struc"
	"github.com/pkg/errors"
	"io"
	"log"
	"os"
	"strings"
)

// MBR MBR磁盘信息结构.
// 具体见 https://en.wikipedia.org/wiki/Master_boot_record.
// 参考现代MBR结构节(`Structure of a modern standard MBR`)的描述
type MBR struct {
	disk                     io.ReadSeeker                        `struc:"skip"` // MBR设备文件读句柄.
	DiskPath                 string                               `struc:"skip"`
	Signature                []byte                               `struc:"skip"`
	isEBR                    bool                                 `struc:"skip"`      // 若此MBR为EBR时, 此字段为true.
	Offset                   int64                                `struc:"skip"`      // MBR数据绝对起始偏移.
	Bin                      []byte                               `struc:"skip"`      // MBR二进制数据.
	BootLoader               []byte                               `struc:"[446]byte"` // 0x0000, 446.
	FullMainPartitionEntries [MBRPartitionEntryCount]MBRPartition // 0x01BE, 64, 所有多字节字段均为小端序.
	BootSignature            [2]byte                              `struc:"[2]byte"` // 0x01FE, 2.
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

func NewMBR(disk string, start int64, isEBR bool) (mbr MBR, err error) {
	fp, err := os.Open(disk)
	if err != nil {
		return MBR{}, err
	}
	mbr, err = newMBR(fp, start, isEBR)
	if err != nil {
		return MBR{}, err
	}
	mbr.DiskPath = disk
	return mbr, nil
}

// newMBR 解析磁盘并获取一个MBR结构信息.
func newMBR(disk io.ReadSeeker, start int64, isEBR bool) (mbr MBR, err error) {
	if _, err = disk.Seek(start, io.SeekStart); err != nil {
		return mbr, err
	}
	mbr.isEBR = isEBR
	mbr.Offset = start
	mbr.Bin = make([]byte, MBRDefaultLBASize)
	_, err = io.ReadFull(disk, mbr.Bin)
	if err != nil {
		return mbr, err
	}
	if err = struc.Unpack(bytes.NewReader(mbr.Bin), &mbr); err != nil {
		return mbr, err
	}
	if mbr.isValid() {
		mbr.disk = disk
		mbr.markIndexToMainPart()
		mbr.markSignature()
		return mbr, nil
	}
	return mbr, errors.New("invalid boot signature for mbr")
}

// newMBR 解析数据并获取一个EBR结构信息.
func newEBR(disk io.ReadSeeker, start int64) (_EBR EBR, err error) {
	_EBR, err = newMBR(disk, start, true)
	if err != nil {
		return _EBR, err
	}
	return _EBR, nil
}

// Hexdump 返回MBR/EBR的LBA0的hexdump格式输出.
func (mbr *MBR) Hexdump() string {
	return hex.Dump(mbr.Bin)
}

func (mbr *MBR) Size() (int64, error) {
	size, err := ioctl.QueryFileSize(mbr.DiskPath)
	return int64(size), err
}

// IsDynamic 判断硬盘是否为Windows平台的动态磁盘.
// 在Windows下跨区卷、带区卷、镜像卷、RAID-5卷均是基于动态磁盘.
func (mbr *MBR) IsDynamic() bool {
	for _, mp := range mbr.FullMainPartitionEntries {
		if mp.IsDynamic() {
			return true
		}
	}
	return false
}

// NotExistedValidParts 若MBR硬盘等效于"无分区", 则返回True.
// 什么情况下, 等效于"无分区"？
// 1. 非空非扩展分区的数量为0.
func (mbr *MBR) NotExistedValidParts() bool {
	parts, err := mbr.VolumePartitions()
	if err != nil {
		// logging.GLog.Errorf("NotExistedValidParts failed to call `VolumePartitions`: %v", err)
		return true
	}
	return len(parts) == 0
}

// EBRs 解析得到EBR的数据.
func (mbr *MBR) EBRs() ([]EBR, error) {
	if mbr.isEBR {
		return nil, errors.New("EBR has no EBR list")
	}
	EBRs := make([]EBR, 0)
	for _, DPT := range mbr.FullMainPartitionEntries {
		if !DPT.IsExtend() {
			continue
		}
		EBROffset := DPT.StartingLBA * MBRDefaultLBASize
		for EBRIndex := 1; ; EBRIndex++ {
			EBR_, err := newEBR(mbr.disk, EBROffset)
			if err != nil {
				log.Printf("EBR can not be parsed at Offset(%v): %v\n", EBROffset, err)
				break
			}
			EBRs = append(EBRs, EBR_)
			if !EBR_.FullMainPartitionEntries[MBREBRPartitionEntryIndex].IsExtend() {
				break
			}
			EBROffset = DPT.StartingLBA*MBRDefaultLBASize +
				EBR_.FullMainPartitionEntries[MBREBRPartitionEntryIndex].StartingLBA*MBRDefaultLBASize
		}
	}
	return EBRs, nil
}

// isValid 若为有效BR,则返回true.
func (mbr *MBR) isValid() bool {
	return mbr.BootSignature[0] == MBRSignature510 && mbr.BootSignature[1] == MBRSignature511
}

// indexExtendMainPartition 获取主扩展分区的分区索引.
func (mbr *MBR) indexExtendMainPartition() int {
	if mbr.isEBR {
		return -1
	}
	for i, p := range mbr.FullMainPartitionEntries {
		if p.IsExtend() {
			return i
		}
	}
	return -1
}

// notEmptyAndExtendMainPartitions 非空分区及非扩展分区的主分区集合.
func (mbr *MBR) notEmptyAndExtendMainPartitions() []MBRPartition {
	vp := make([]MBRPartition, 0)
	if mbr.isEBR {
		return vp
	}
	for _, mp := range mbr.FullMainPartitionEntries {
		if !mp.IsEmpty() && !mp.IsExtend() {
			vp = append(vp, mp)
		}
	}
	return vp
}

// VolumePartitions 获取所有非空白、非扩展的逻辑/主分区.
func (mbr *MBR) VolumePartitions() ([]MBRPartition, error) {
	if mbr.isEBR {
		return nil, errors.New("EBR has no not-empty and not-extend partitions for MBR")
	}
	ps := mbr.notEmptyAndExtendMainPartitions()
	lps, err := mbr.LogicalPartitionEntries()
	if err != nil {
		return nil, err
	}
	ps = append(ps, lps...)
	return ps, nil
}

// MainPartitionEntries 获取所有主分区表项.
func (mbr *MBR) MainPartitionEntries() ([]MBRPartition, error) {
	if mbr.isEBR {
		return nil, errors.New("EBR has no primary partitions for MBR")
	}
	MBRs := make([]MBRPartition, 0)
	for _, mp := range mbr.FullMainPartitionEntries {
		if !mp.IsEmpty() {
			MBRs = append(MBRs, mp)
		}
	}
	return MBRs, nil
}

// markIndexToMainPart 为非扩展分区非空白分区的主分区标记分区索引.
// 分区的索引规则是分区占用的哪一个分区表项, 那么他的分区索引就是分区表项的序号. 而对于MBR类型的设备而言,
// 其可能存在EBR(逻辑分区), 逻辑分区的分区索引也是
func (mbr *MBR) markIndexToMainPart() {
	for mpi := 1; mpi <= MBRPartitionEntryCount; mpi++ {
		mbr.FullMainPartitionEntries[mpi-1].Index = mpi
	}
}

func (mbr *MBR) markSignature() {
	signature := mbr.BootLoader[440 : 440+4]
	for i := 3; i >= 0; i-- {
		mbr.Signature = append(mbr.Signature, signature[i])
	}
}

// LogicalPartitionEntries 获取所有逻辑分区表项.
func (mbr *MBR) LogicalPartitionEntries() ([]MBRPartition, error) {
	if mbr.isEBR {
		return nil, errors.New("EBR has no logical partitions")
	}
	EBRs, err := mbr.EBRs()
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
	lastEBRStartingLBA := mbr.FullMainPartitionEntries[index].StartingLBA
	for lpi, EBR_ := range EBRs {
		lpi++
		peData := EBR_.FullMainPartitionEntries[MBRLogicalPartitionEntryIndex].Copy()
		peEBR := EBR_.FullMainPartitionEntries[MBREBRPartitionEntryIndex].Copy()
		// 修正相对LBA偏移为绝对LBA偏移.
		peData.StartingLBA += lastEBRStartingLBA
		peData.IsLogical = true
		peData.Index = lpi + 4
		if !peData.IsEmpty() {
			lps = append(lps, *peData)
		}
		if peEBR.IsExtend() {
			lastEBRStartingLBA = peEBR.StartingLBA + mbr.FullMainPartitionEntries[index].StartingLBA
		}
	}
	return lps, nil
}

type MBRJson struct {
	DiskLabelType  string        `json:"disk_label_type"`
	DiskIdentifier string        `json:"disk_identifier"`
	SectorSize     int           `json:"sector_size"`
	Sectors        int           `json:"sectors"`
	Size           int64         `json:"size"`
	Parts          []MBRPartJson `json:"parts"`
}

type MBRPartJson struct {
	Index       int    `json:"index"`
	StartSector int64  `json:"start_sector"`
	EndSector   int64  `json:"end_sector"`
	Sectors     int64  `json:"sectors"`
	Size        int64  `json:"size"`
	Boot        bool   `json:"boot"`
	Type        string `json:"type"`
	TypeDesc    string `json:"type_desc"`
}

// JSONFormat 以JSON格式获取显示输出.
//
// 示例：
// ```
//
//	{
//	       "disk_label_type": "MBR",
//	       "disk_identifier": "0e772c",
//	       "sector_size": 512,
//	       "sectors": 83886080,
//	       "size": 42949672960,
//	       "parts": [
//	               {
//	                       "index": 1,
//	                       "start_sector": 2048,
//	                       "end_sector": 2099199,
//	                       "sectors": 2097152,
//	                       "size": 1073741824,
//	                       "boot": true,
//	                       "type": "83",
//	                       "type_desc": "Linux"
//	               },
//	               {
//	                       "index": 2,
//	                       "start_sector": 2099200,
//	                       "end_sector": 83886079,
//	                       "sectors": 81786880,
//	                       "size": 41874882560,
//	                       "boot": false,
//	                       "type": "8e",
//	                       "type_desc": "Linux LVM"
//	               }
//	       ]
//	}
//
// ```
func (mbr *MBR) JSONFormat() (o string, err error) {
	mj := new(MBRJson)
	mj.DiskLabelType = string(DTypeMBR)
	mj.DiskIdentifier = fmt.Sprintf("%x%x%x%x", mbr.Signature[0], mbr.Signature[1], mbr.Signature[2], mbr.Signature[3])
	mj.SectorSize = MBRDefaultLBASize
	mj.Size, err = mbr.Size()
	if err != nil {
		return "", err
	}
	mj.Sectors = int(mj.Size / MBRDefaultLBASize)
	mj.Parts = make([]MBRPartJson, 0)
	mps := make([]MBRPartition, 0)
	mps = append(mps, mbr.FullMainPartitionEntries[:]...)
	mlps, err := mbr.LogicalPartitionEntries()
	if err != nil {
		return "", err
	}
	mps = append(mps, mlps...)
	for _, mp := range mps {
		if mp.IsEmpty() {
			continue
		}
		mpj := new(MBRPartJson)
		mpj.Index = mp.Index
		mpj.StartSector = mp.StartingLBA
		mpj.EndSector = mp.StartingLBA + mp.TotalSectors - 1
		mpj.Sectors = mp.TotalSectors
		mpj.Boot = mp.IsBootable()
		mpj.Size = mpj.Sectors * MBRDefaultLBASize
		mpj.Type = fmt.Sprintf("%02x", mp.PartitionType)
		mpj.TypeDesc = MBRPartitionTypeDesc[mp.PartitionType]
		mj.Parts = append(mj.Parts, *mpj)
	}
	o_, err := json.MarshalIndent(mj, "", "\t")
	return string(o_), err
}

// DebugFormat 以Debug模式获取显示输出.
//
// 示例:
// ```
// Found valid MBR.
// Disk: 40 GiB, 42949672960 bytes, 83886080 sectors
// Sector Size is 512 bytes
// Disk identifier <32-bit Signature>: 0e772c
//
// Number Boot      Start        End             Size    System
//
//	1    *       2048    2099199      1073741824B    Linux
//	2         2099200   83886079     41874882560B    Linux LVM
//
// ```
func (mbr *MBR) DebugFormat() (string, error) {
	o, err := mbr.JSONFormat()
	if err != nil {
		return "", err
	}
	mj := new(MBRJson)
	err = json.Unmarshal([]byte(o), mj)
	if err != nil {
		return "", err
	}
	fmt_ := `
Found valid MBR.
Disk: %s, %v bytes, %v sectors
Sector Size is %v bytes
Disk identifier <32-bit Signature>: %s

%s
`
	partsDescList := make([]string, 0)
	for i, p := range mj.Parts {
		if i == 0 {
			partsDescList = append(partsDescList,
				fmt.Sprintf("%6s %4s %10s %10s %16s    %s", "Number", "Boot", "Start", "End", "Size", "System"))
		}
		bootFlag := " "
		if p.Boot {
			bootFlag = "*"
		}
		partsDescList = append(partsDescList,
			fmt.Sprintf("%6d %4s %10d %10d %15dB    %s", p.Index, bootFlag, p.StartSector, p.EndSector, p.Size, p.TypeDesc))
	}
	out := fmt.Sprintf(fmt_,
		humanize.IBytes(uint64(mj.Size)),
		mj.Size,
		mj.Sectors,
		mj.SectorSize,
		mj.DiskIdentifier,
		strings.Join(partsDescList, "\n"))
	return out, nil
}

// MBRPartition MBR磁盘的主分区表项结构.
type MBRPartition struct {
	// Index 这一索引仅仅代表分区在分区表中的索引位置，
	// 注意！！！在Linux系统可代表应用层设备的尾部编号(也可叫分区ID)，在windows不能代表应用层设备的分区ID.
	Index            int              `struc:"skip"`
	IsLogical        bool             `struc:"skip"` // 若为逻辑分区, 此字段为true.
	BootIndicator    byte             // 0x00, 1.
	StartingHead     byte             // 0x01, 1.
	StartingSector   byte             // 0x02, 1, bit0-5表示起始扇区, bit6-7位表示起始柱面的高位.
	StartingCylinder byte             // 0x03, 1, (StartingSector-bit6-7 + StartingCylinder-bit0-8)表示起始柱面号.
	PartitionType    MBRPartitionType `struc:"byte"` // 0x04, 1. 见 https://en.wikipedia.org/wiki/Partition_type.
	EndingHead       byte             // 0x05, 1.
	EndingSector     byte             // 0x06, 1, bit0-5表示结束扇区, bit6-7位表示结束柱面的高位.
	EndingCylinder   byte             // 0x07, 1, (EndingSector-bit6-7 + EndingCylinder-bit0-8)表示起始柱面号.
	StartingLBA      int64            `struc:"uint32,little"` // 0x08, 4, 起始LBA(包含).
	TotalSectors     int64            `struc:"uint32,little"` // 0x0c, 4, 总扇区数.
}

// HumanReadablePartitionType 返回该分区用户可读的分区类型.
func (partition MBRPartition) HumanReadablePartitionType() string {
	v, ok := MBRPartitionTypeDesc[partition.PartitionType]
	if !ok {
		return "unknown"
	}
	return v
}

// Brief 返回该分区简短描述.
func (partition MBRPartition) Brief() string {
	if !partition.IsLogical && partition.IsExtend() {
		return "Extend PartitionIndex #0"
	}
	if partition.IsLogical && partition.IsExtend() {
		return "EBR Addressing PartitionIndex"
	}
	briefs := make([]string, 0)
	briefs = append(briefs, fmt.Sprintf("PART#%v OFF#%v SIZE#%v", partition.Index, partition.StartingLBA*512, partition.TotalSectors*512))
	return strings.Join(briefs, ", ")
}

// IsEmpty 若为空分区, 则返回true.
func (partition MBRPartition) IsEmpty() bool {
	return partition.PartitionType == Empty
}

// IsBootable 若为可启动设备，则返回true.
// 注意，此返回值仅返回磁盘层是否为可引导的，未涉及到操作系统层面，对于操作系统层面的可引导判定，需如下方法：
// 对于Windows而言, 可使用`wmic partition get DeviceID,type_,Bootable,Size`的Bootable字段得到.
// 对于Linux而言, 可观察其/boot及/EFI挂载点所对应的分区号.
func (partition MBRPartition) IsBootable() bool {
	return partition.BootIndicator == MBRPartitionBootable
}

// EndSector 分区的结束扇区(包含)
func (partition MBRPartition) EndSector() int64 {
	return partition.StartingLBA + partition.TotalSectors - 1
}

// IsExtend 若为扩展分区, 则返回true.
func (partition MBRPartition) IsExtend() bool {
	return bytes.IndexByte(MBRExtendPartTypes, partition.PartitionType) >= 0
}

// IsLVM 若为lvm分区, 则返回true.
func (partition MBRPartition) IsLVM() bool {
	return partition.PartitionType == LinuxLVM
}

// IsSwap 若为交换分区, 则返回true.
func (partition MBRPartition) IsSwap() bool {
	return partition.PartitionType == LinuxSwap || partition.PartitionType == VmwareSwap
}

// IsRecovery 若为恢复分区, 则返回true.
func (partition MBRPartition) IsRecovery() bool {
	return partition.PartitionType == WindowsRecoveryEnv
}

// IsProtectiveMBR 若为GPT磁盘的保护性MBR分区, 则返回true.
func (partition MBRPartition) IsProtectiveMBR() bool {
	return partition.PartitionType == EFIGPTProtectiveMBR
}

// IsDynamic 若为动态磁盘的元数据卷, 则返回true.
func (partition MBRPartition) IsDynamic() bool {
	return partition.PartitionType == WindowsDynamicExtendedPartitionMarker
}

// Copy 深拷贝当前 MBRPartition 对象.
func (partition MBRPartition) Copy() *MBRPartition {
	np := new(MBRPartition)
	np.Index = partition.Index
	np.IsLogical = partition.IsLogical
	np.BootIndicator = partition.BootIndicator
	np.StartingHead = partition.StartingHead
	np.StartingSector = partition.StartingSector
	np.PartitionType = partition.PartitionType
	np.EndingHead = partition.EndingHead
	np.EndingSector = partition.EndingSector
	np.EndingCylinder = partition.EndingCylinder
	np.StartingLBA = partition.StartingLBA
	np.TotalSectors = partition.TotalSectors
	return np
}
