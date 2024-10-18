package table

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/dustin/go-humanize"
	"github.com/kisun-bit/drpkg/sys/ioctl"
	"github.com/lunixbochs/struc"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
	"io"
	"os"
	"strings"
	"unicode/utf16"
)

// GPT GPT磁盘信息结构.
// 具体见：https://en.wikipedia.org/wiki/GUID_Partition_Table.
type GPT struct {
	disk             io.ReadSeeker                             `struc:"skip"`      // GPT设备文件读句柄.
	DiskPath         string                                    `struc:"skip"`      // GPT设备路径.
	Offset           int64                                     `struc:"skip"`      // GPT数据绝对起始偏移.
	Bin              []byte                                    `struc:"skip"`      // GPT数据的LBA0，LBA1, LBA2-34的二进制数据.
	BinProtectiveMBR []byte                                    `struc:"[512]byte"` // GPT数据-LBA0的保护性MBR数据.
	Header           GPTHeader                                 // GPT数据-LBA1的GPT头数据.
	PartitionEntries [GPTPartitionEntryCount]GPTPartitionEntry // GPT分区表项.
}

func (gpt *GPT) IsValid() bool {
	return gpt.Header.SignatureString() == GPTSignature
}

func (gpt *GPT) Size() (int64, error) {
	size, err := ioctl.QueryFileSize(gpt.DiskPath)
	return int64(size), err
}

// IsDynamic 若是Windows动态磁盘, 则返回true.
// 在Windows下跨区卷、带区卷、镜像卷、RAID-5卷均是基于动态磁盘.
func (gpt *GPT) IsDynamic() bool {
	hasLDMMetaPart := false
	hasLDMDataPart := false
	for _, gp := range gpt.PartitionEntries {
		if gp.PartTypeGUIDInMixedEndian() == LDMMetaDataPartition {
			hasLDMMetaPart = true
		} else if gp.PartTypeGUIDInMixedEndian() == LDMDataPartition {
			hasLDMDataPart = true
		}
	}
	return hasLDMMetaPart && hasLDMDataPart
}

// NotExistedValidParts 若GPT硬盘等效于"无分区", 则返回True.
// 什么情况下, 等效于"无分区"？
// 1. Windows平台下, 仅有一个MSR分区或无非空非MSR分区.
// 2. 无非空分区.
func (gpt *GPT) NotExistedValidParts() bool {
	notEmptyPartsNum := 0
	msrPartNum := 0
	for _, p := range gpt.PartitionEntries {
		if p.IsEmpty() {
			continue
		}
		notEmptyPartsNum += 1
		if p.PartTypeGUIDInMixedEndian() == MicroMSR {
			msrPartNum += 1
		}
	}
	// Linux平台硬盘若存在一个MSR，那么很有可能此盘会插到Windows存储中.
	return (notEmptyPartsNum == 0) || (notEmptyPartsNum == 1 && msrPartNum == 1)
}

type BackupGPT struct {
	Offset           int64                                     `struc:"skip"` // 次要GPT数据起始偏移.
	Bin              []byte                                    `struc:"skip"` // 次要GPT数据的LBA-1, LBA-2 - -33的二进制数据.
	PartitionEntries [GPTPartitionEntryCount]GPTPartitionEntry // 次要GPT分区表项.
	Header           GPTHeader                                 // 次要GPT数据的GPT头数据
}

// GPTHeader 位于GPT磁盘的LBA1数据(若为Backup GPT，则是LBA-1).
type GPTHeader struct {
	Signature                 []byte `struc:"[8]byte"`       // 0x00, 8, EFI签名 ("EFI PART", 45h 46h 49h 20h 50h 41h 52h 54h or 0x5452415020494645ULL on little-endian machines).
	Revision                  uint32 `struc:"uint32,little"` // 0x08, 4, little, 版本号信息.
	HeaderSize                uint32 `struc:"uint32,little"` // 0x0C, 4, little, 主分区表头数据字节大小.
	HeaderCRC32               uint32 `struc:"uint32,little"` // 0x10, 4, little, 主分区表头数据0x00-0x5B之间数据的校验和.
	Reserved                  []byte `struc:"[4]byte"`       // 0x14, 4, 保留.
	CurrentLBA                int64  `struc:"int64,little"`  // 0x18, 8, little, 当前分区表头数据所处的LBA.
	BackupLBA                 int64  `struc:"int64,little"`  // 0x20, 8, little, 备份分区表头数据所处的LBA.
	FirstUnUsableLBAIndex     int64  `struc:"int64,little"`  // 0x28, 8, little, 首个未使用的LBA, 其值等于主分区表最后一个LBA + 1.
	LastUnUsableLBAIndex      int64  `struc:"int64,little"`  // 0x30, 8, little, 最后一个未使用的LBA, 其值等于备份分区表第一个LBA - 1.
	GUID                      []byte `struc:"[16]byte"`      // 0x38, 16, mixed endian, 磁盘GUID, 此字段解析出来的值未以mixed endian返回，若要获取其mixed endian读取形式，请调用 GUIDInMixedEndian.
	StartingLBAForPartEntries int64  `struc:"int64,little"`  // 0x48, 8, little, 分区表项起始LBA（通常为2）.
	NumberOfPartEntriesArray  int    `struc:"int32,little"`  // 0x50, 4, little, 分区表项数组的成员个数.
	PartEntrySize             int    `struc:"int32,little"`  // 0x54, 4, little, 一个分区表项数据的字节长度.
	PartEntriesArrayCRC32     uint32 `struc:"uint32,little"` // 0x58, 4, little, 分区表项数组数据的校验和.
	TailReversed              []byte `struc:"[420]byte"`     // 0x5C, 420, 分区表预留数据区.
}

func (gh *GPTHeader) GUIDInMixedEndian() string {
	return GUIDToString(gh.GUID)
}

func (gh *GPTHeader) SignatureString() string {
	return string(gh.Signature)
}

// GPTPartitionEntry GPT磁盘的一项分区表项数据.
type GPTPartitionEntry struct {
	Index         int      `struc:"skip"`              // 分区位置索引.
	PartTypeGUID  []byte   `struc:"[16]byte"`          // 0x00, 16, mixed endian, 分区类型GUID.
	UniqGUID      []byte   `struc:"[16]byte"`          // 0x10, 16, mixed endian, 唯一编码GUID.
	FirstLBAIndex int64    `struc:"int64,little"`      // 0x20, 8, little endian, 起始LBA(包含).
	LastLBAIndex  int64    `struc:"int64,little"`      // 0x28, 8, little endian, 结束LBA(包含).
	AttrFlags     []byte   `struc:"[8]byte"`           // 0x30, 8, 属性, 例如位 60 表示只读.
	PartitionName []uint16 `struc:"[36]uint16,little"` // 0x38, 72, 分区名称, 36 个 UTF-16LE 代码单元.
}

func (gpe *GPTPartitionEntry) PartTypeGUIDInMixedEndian() string {
	return GUIDToString(gpe.PartTypeGUID)
}

func (gpe *GPTPartitionEntry) UniqGUIDInMixedEndian() string {
	return GUIDToString(gpe.UniqGUID)
}

func (gpe *GPTPartitionEntry) DecodedPartitionName() string {
	s := string(utf16.Decode(gpe.PartitionName))
	return strings.ReplaceAll(s, "\u0000", "")
}

// IsEmpty 若是空分区, 则返回True.
func (gpe *GPTPartitionEntry) IsEmpty() bool {
	return gpe.PartTypeGUIDInMixedEndian() == BlankEmptyPart
}

// IsBootable 若是启动分区, 则返回True.
func (gpe *GPTPartitionEntry) IsBootable() bool {
	return funk.InStrings(
		[]string{
			BIOSBootPartition,
			BootPartition,
			BootPartition2,
			BootPartition3,
			GEFISystemPartition,
		},
		gpe.PartTypeGUIDInMixedEndian())
}

// IsRecovery 若是恢复环境分区, 则返回True.
func (gpe *GPTPartitionEntry) IsRecovery() bool {
	return funk.InStrings(
		[]string{
			AppleTVRecoveryPartition,
			MicroMRE,
		},
		gpe.PartTypeGUIDInMixedEndian())
}

// IsSwap 若是交换分区, 则返回True.
func (gpe *GPTPartitionEntry) IsSwap() bool {
	return funk.InStrings(
		[]string{
			SwapPartition,
			SwapPartition2,
			SwapPartition3,
			SwapPartition4,
			SwapPartition5,
		},
		gpe.PartTypeGUIDInMixedEndian())
}

// IsLVM 若是LVM分区, 则返回True.
func (gpe *GPTPartitionEntry) IsLVM() bool {
	return funk.InStrings(
		[]string{
			LVMPartition,
		},
		gpe.PartTypeGUIDInMixedEndian())
}

// IsReversed 若是预留分区, 则返回True.
func (gpe *GPTPartitionEntry) IsReversed() bool {
	return funk.InStrings(
		[]string{
			MicroMSR,
			Reserved,
			ReservedPartition,
			ReservedPartition2,
			ReservedPartition4,
			ReservedPartition5,
		},
		gpe.PartTypeGUIDInMixedEndian())
}

func (gpe *GPTPartitionEntry) PartTypeDesc() string {
	v, ok := GPTPartitionTypeDesc[gpe.PartTypeGUIDInMixedEndian()]
	if !ok {
		v = "UNKNOWN"
	}
	return v
}

func NewGPT(disk string, start int64) (gpt GPT, err error) {
	fp, err := os.Open(disk)
	if err != nil {
		return GPT{}, err
	}
	gpt, err = newGPT(fp, start)
	if err != nil {
		return GPT{}, err
	}
	gpt.DiskPath = disk
	return gpt, nil
}

// newGPT 获取GPT硬盘的磁盘结构.
func newGPT(disk io.ReadSeeker, start int64) (gpt GPT, err error) {
	if _, err = disk.Seek(start, io.SeekStart); err != nil {
		return gpt, err
	}
	gpt.disk = disk
	gpt.Offset = start
	gpt.Bin = make([]byte, (1+1+32)*GPTDefaultLBASize)
	_, err = io.ReadFull(disk, gpt.Bin)
	if err != nil {
		return GPT{}, err
	}
	err = struc.Unpack(bytes.NewReader(gpt.Bin), &gpt)
	if err != nil {
		return GPT{}, err
	}
	if gpt.IsValid() {
		gpt.markIndex()
		return gpt, nil
	}
	return GPT{}, errors.New("invalid gpt signature")
}

func (gpt *GPT) markIndex() {
	for i := 0; i < GPTPartitionEntryCount; i++ {
		gpt.PartitionEntries[i].Index = i + 1
	}
}

// BackupGPT 备份分区表.
func (gpt *GPT) BackupGPT() (bgpt BackupGPT, err error) {
	ptSize := int64(32) * GPTDefaultLBASize
	bgpt.Offset = gpt.Header.BackupLBA*GPTDefaultLBASize - 1*GPTDefaultLBASize - ptSize
	_, err = gpt.disk.Seek(bgpt.Offset, io.SeekStart)
	if err != nil {
		return BackupGPT{}, err
	}
	// len_ 取值为备份分区表+备份分区表表头.
	len_ := 1*GPTDefaultLBASize + ptSize
	bgpt.Bin = make([]byte, len_)
	_, err = io.ReadFull(gpt.disk, bgpt.Bin)
	if err != nil {
		return BackupGPT{}, err
	}
	err = struc.Unpack(bytes.NewReader(bgpt.Bin), &bgpt)
	return bgpt, err
}

type GPTJson struct {
	DiskLabelType                  string        `json:"disk_label_type"`
	DiskIdentifier                 string        `json:"disk_identifier"`
	SectorSize                     int           `json:"sector_size"`
	Sectors                        int64         `json:"sectors"`
	Size                           int64         `json:"size"` // 注意！！！！！！此Size仅仅代表GPT大小，并不代表实际硬盘文件大小
	FirstUnusedSectorIndex         int64         `json:"first_unused_sector_index"`
	LastUnusedSectorIndex          int64         `json:"last_unused_sector_index"`
	PartitionTableStartSectorIndex int64         `json:"partition_table_start_sector_index"`
	PartitionTableArrayLength      int           `json:"partition_table_array_size"`
	OnePartitionTableEntryBytes    int           `json:"one_partition_table_entry_bytes"`
	Parts                          []GPTPartJson `json:"parts"`
}

type GPTPartJson struct {
	// Index 这一索引仅仅代表分区在分区表中的索引位置，
	// 注意！！！在Linux系统可代表应用层设备的尾部编号(也可叫分区ID)，在windows不能代表应用层设备的分区ID.
	// 根本原因是, Windows遵循分区表中非空分区之间不允许存在空分区, 所以每增加或删除一个分区时,
	// 也就导致可能修改所有的分区表项，使得其各个非空分区表项位置连续且集中.
	Index       int    `json:"index"`
	Boot        bool   `json:"boot"`
	StartSector int64  `json:"start_sector"`
	EndSector   int64  `json:"end_sector"`
	Sectors     int64  `json:"sectors"`
	Size        int64  `json:"size"`
	Type        string `json:"type"`
	TypeDesc    string `json:"type_desc"`
}

// JSONFormat 以JSON格式获取显示输出.
//
// 示例:
// ```
//
//	{
//	       "disk_label_type": "GPT",
//	       "disk_identifier": "B2D588EC-966D-445B-BAB3-846CE330166B",
//	       "sector_size": 512,
//	       "sectors": 20971520,
//	       "size": 10737418240,
//	       "first_unused_sector_index": 34,
//	       "last_unused_sector_index": 20971486,
//	       "partition_table_start_sector_index": 2,
//	       "partition_table_array_size": 128,
//	       "one_partition_table_entry_bytes": 128,
//	       "parts": [
//	               {
//	                       "index": 1,
//	                       "start_sector": 2048,
//	                       "end_sector": 22527,
//	                       "sectors": 20480,
//	                       "size": 10737418240,
//	                       "type": "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
//	                       "type_desc": "Linux Filesystem Data"
//	               },
//	               {
//	                       "index": 3,
//	                       "start_sector": 45056,
//	                       "end_sector": 69631,
//	                       "sectors": 24576,
//	                       "size": 10737418240,
//	                       "type": "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
//	                       "type_desc": "Linux Filesystem Data"
//	               }
//	       ]
//	}
//
// ```
func (gpt *GPT) JSONFormat() (string, error) {
	gj := new(GPTJson)
	gj.SectorSize = GPTDefaultLBASize
	gj.DiskLabelType = string(DTypeGPT)
	gj.Sectors = (gpt.Header.LastUnUsableLBAIndex + 1) + 1 + 32
	gj.FirstUnusedSectorIndex = gpt.Header.FirstUnUsableLBAIndex
	gj.LastUnusedSectorIndex = gpt.Header.LastUnUsableLBAIndex
	gj.PartitionTableStartSectorIndex = gpt.Header.StartingLBAForPartEntries
	gj.OnePartitionTableEntryBytes = gpt.Header.PartEntrySize
	gj.PartitionTableArrayLength = gpt.Header.NumberOfPartEntriesArray
	gj.Size = gj.Sectors * int64(gj.SectorSize)
	gj.DiskIdentifier = GUIDToString(gpt.Header.GUID)
	gj.Parts = make([]GPTPartJson, 0)
	for _, p := range gpt.PartitionEntries {
		if p.IsEmpty() {
			continue
		}
		pt := new(GPTPartJson)
		pt.Index = p.Index
		pt.Boot = p.IsBootable()
		pt.StartSector = p.FirstLBAIndex
		pt.EndSector = p.LastLBAIndex
		pt.Sectors = p.LastLBAIndex - p.FirstLBAIndex + 1
		pt.Size = pt.Sectors * GPTDefaultLBASize
		pt.Type = p.PartTypeGUIDInMixedEndian()
		pt.TypeDesc = p.PartTypeDesc()
		gj.Parts = append(gj.Parts, *pt)
	}
	o, err := json.MarshalIndent(gj, "", "\t")
	return string(o), err
}

// DebugFormat 以Debug模式获取显示输出.
//
// 示例:
// ```
// Found valid GPT.
// Disk: 20971520 sectors, 10 GiB
// Logical sector size: 512 bytes
// Disk identifier <GUID>: B2D588EC-966D-445B-BAB3-846CE330166B
// PartitionIndex table holds up to 128 entries, the number of effective part is 2
// PartitionIndex table begins at sector 2 and the size of partition entry is 128 bytes
// First usable sector index is 34, last usable sector index is 20971486
//
// Number           Start             End                  Size    Type
//
//	1            2048           22527          10737418240B    Linux Filesystem Data
//	3           45056           69631          10737418240B    Linux Filesystem Data
//
// ```
func (gpt *GPT) DebugFormat() (string, error) {
	o, err := gpt.JSONFormat()
	if err != nil {
		return "", err
	}
	gj := new(GPTJson)
	err = json.Unmarshal([]byte(o), gj)
	if err != nil {
		return "", err
	}
	fmt_ := `
Found valid GPT.
Disk: %v sectors, %s
Logical sector size: %v bytes
Disk identifier <GUID>: %s
PartitionIndex table holds up to %v entries, the number of effective part is %v
PartitionIndex table begins at sector %v and the size of partition entry is %v bytes
First usable sector index is %v, last usable sector index is %v

%s
`
	partsDescList := make([]string, 0)
	for i, p := range gj.Parts {
		if i == 0 {
			partsDescList = append(partsDescList,
				fmt.Sprintf("%6s %15s %15s %21s    %s", "Number", "Start", "End", "Size", "Type"))
		}
		partsDescList = append(partsDescList,
			fmt.Sprintf("%6d %15d %15d %20dB    %s", p.Index, p.StartSector, p.EndSector, p.Size, p.TypeDesc))
	}
	out := fmt.Sprintf(fmt_,
		gj.Sectors,
		humanize.IBytes(uint64(gj.Size)),
		gj.SectorSize,
		gj.DiskIdentifier,
		gj.PartitionTableArrayLength,
		len(gj.Parts),
		gj.PartitionTableStartSectorIndex,
		gj.OnePartitionTableEntryBytes,
		gj.FirstUnusedSectorIndex,
		gj.LastUnusedSectorIndex,
		strings.Join(partsDescList, "\n"))
	return out, nil
}
