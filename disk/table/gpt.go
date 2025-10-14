package table

import (
	"bytes"
	"io"
	"os"

	"github.com/kisun-bit/drpkg/extend"
	"github.com/lunixbochs/struc"
	"github.com/pkg/errors"
)

// GPT GPT磁盘信息结构
// 具体见：https://en.wikipedia.org/wiki/GUID_Partition_Table.
type GPT struct {
	Path string `struc:"skip"` // GPT设备路径

	disk io.ReadSeeker `struc:"skip"` // GPT设备文件读句柄

	SectorSize       int            `struc:"skip"`      // 一个物理扇区的字节数
	Offset           int64          `struc:"skip"`      // GPT数据绝对起始偏移
	Bin              []byte         `struc:"skip"`      // GPT数据的LBA0，LBA1, LBA2-34的二进制数据, 若您有备份磁盘需求，请备份此字段
	BinProtectiveMBR []byte         `struc:"[512]byte"` // GPT数据-LBA0的保护性MBR数据
	Header           GPTHeader      // GPT数据-LBA1的GPT头数据
	PartitionEntries []GPTPartition // GPT分区表项
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

type GPTPartition struct {
	Index         int      `struc:"skip"`              // 分区位置索引.
	PartTypeGUID  []byte   `struc:"[16]byte"`          // 0x00, 16, mixed endian, 分区类型GUID.
	UniqGUID      []byte   `struc:"[16]byte"`          // 0x10, 16, mixed endian, 唯一编码GUID.
	FirstLBAIndex int64    `struc:"int64,little"`      // 0x20, 8, little endian, 起始LBA(包含).
	LastLBAIndex  int64    `struc:"int64,little"`      // 0x28, 8, little endian, 结束LBA(包含).
	AttrFlags     []byte   `struc:"[8]byte"`           // 0x30, 8, 属性, 例如位 60 表示只读.
	PartitionName []uint16 `struc:"[36]uint16,little"` // 0x38, 72, 分区名称, 36 个 UTF-16LE 代码单元.
}

type BackupGPT struct {
	Offset           int64          `struc:"skip"` // 次要GPT数据起始偏移.
	Bin              []byte         `struc:"skip"` // 次要GPT数据的LBA-1, LBA-2 - -33的二进制数据.
	PartitionEntries []GPTPartition // 次要GPT分区表项.
	Header           GPTHeader      // 次要GPT数据的GPT头数据
}

func NewGPT(disk string, start int64) (gpt *GPT, err error) {
	ss, err := extend.BytesPerSector(disk)
	if err != nil {
		return nil, err
	}
	fp, err := os.Open(disk)
	if err != nil {
		return nil, errors.New("failed to open disk")
	}
	gpt, err = newGPT(fp, start, ss)
	if err != nil {
		_ = fp.Close()
		return nil, errors.Wrapf(err, "failed to parse GPT")
	}
	gpt.Path = disk
	gpt.SectorSize = ss
	return gpt, nil
}

func newGPT(disk io.ReadSeeker, start int64, lbaSize int) (gpt *GPT, err error) {
	gpt = new(GPT)

	gpt.disk = disk
	gpt.Offset = start
	gpt.Bin = make([]byte, (1+1+32)*lbaSize) // 保护性MBR(LBA0) + GPT头部信息(LBA1) + 分区表项(LBA2-LBA33)

	if lbaSize <= 0 {
		return nil, errors.New("invalid sector size")
	}
	if _, err = disk.Seek(start, io.SeekStart); err != nil {
		return gpt, err
	}
	_, err = io.ReadFull(disk, gpt.Bin)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read GPT")
	}

	//
	// 保护性MBR(LBA0)
	//

	gpt.BinProtectiveMBR = gpt.Bin[:512]

	//
	// GPT头部信息(LBA1)
	//

	r := bytes.NewReader(gpt.Bin[lbaSize*1:])
	h := GPTHeader{}
	err = struc.Unpack(r, &h)
	if err != nil {
		return nil, err
	}
	gpt.Header = h

	//
	// 分区表项(LBA2-LBA33)
	//

	pesBinLen := h.PartEntrySize * h.NumberOfPartEntriesArray
	if pesBinLen%lbaSize != 0 {
		pesBinLen += lbaSize - pesBinLen%lbaSize
	}
	pesBin := make([]byte, pesBinLen)
	pesOffset := h.StartingLBAForPartEntries * int64(lbaSize)
	if _, err = disk.Seek(pesOffset, io.SeekStart); err != nil {
		return nil, err
	}
	_, err = io.ReadFull(disk, pesBin)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read partition entries")
	}

	pesReader := bytes.NewReader(pesBin)
	for i := 0; i < h.NumberOfPartEntriesArray; i++ {
		if _, err = pesReader.Seek(int64(h.PartEntrySize)*int64(i), io.SeekStart); err != nil {
			return nil, errors.Wrapf(err, "failed to seek partition entry %v", i)
		}
		var pe GPTPartition
		err = struc.Unpack(pesReader, &pe)
		if err != nil {
			return nil, errors.Wrapf(err, "unpack partition entrie %v", i)
		}
		gpt.PartitionEntries = append(gpt.PartitionEntries, pe)
	}

	if string(gpt.Header.Signature) != "EFI PART" {
		return nil, errors.New("invalid gpt signature")
	}

	for i := 0; i < h.NumberOfPartEntriesArray; i++ {
		gpt.PartitionEntries[i].Index = i + 1
	}
	return gpt, nil
}

func (gpt *GPT) Close() error {
	if extend.IsNilType(gpt.disk) {
		return nil
	}
	fd := gpt.disk.(*os.File)
	return fd.Close()
}

func (gpt *GPT) Identifier() string {
	return GUIDToString(gpt.Header.GUID)
}

func (gpt *GPT) Size() (int64, error) {
	size, err := extend.FileSize(gpt.Path)
	return int64(size), err
}

func (gpt *GPT) ContainsBootFlag() bool {
	for _, p := range gpt.PartitionEntries {
		switch p.Type() {
		case GPT_BIOS_BOOT, GPT_FREEBSD_BOOT, GPT_APPLE_BOOT, GPT_SOLARIS_BOOT, GPT_MIDNIGHTBSD_BOOT, GPT_EFI:
			return true
		}
	}
	return false
}

func (gpt *GPT) BackupGPT() (bgpt *BackupGPT, err error) {
	bgpt = new(BackupGPT)

	pesBinLen := int64(32) * int64(gpt.SectorSize)
	bgpt.Offset = gpt.Header.BackupLBA*int64(gpt.SectorSize) - 1*int64(gpt.SectorSize) - pesBinLen

	_, err = gpt.disk.Seek(bgpt.Offset, io.SeekStart)
	if err != nil {
		return nil, err
	}

	// len_ 取值为备份分区表+备份分区表表头.
	len_ := 1*int64(gpt.SectorSize) + pesBinLen
	bgpt.Bin = make([]byte, len_)
	_, err = io.ReadFull(gpt.disk, bgpt.Bin)
	if err != nil {
		return nil, err
	}
	bgptBinReader := bytes.NewReader(bgpt.Bin)

	//
	// 备份分区表的表头
	//

	if _, err = bgptBinReader.Seek(int64(1*gpt.SectorSize), io.SeekEnd); err != nil {
		return nil, err
	}
	if err = struc.Unpack(bgptBinReader, &bgpt.Header); err != nil {
		return nil, err
	}

	//
	// 备份分区表的分区表项
	//

	if _, err = bgptBinReader.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	for i := 0; i < bgpt.Header.NumberOfPartEntriesArray; i++ {
		if _, err = bgptBinReader.Seek(int64(bgpt.Header.PartEntrySize)*int64(i), io.SeekStart); err != nil {
			return nil, errors.Wrapf(err, "failed to seek partition entry %v", i)
		}
		var pe GPTPartition
		err = struc.Unpack(bgptBinReader, &pe)
		if err != nil {
			return nil, errors.Wrapf(err, "unpack partition entrie %v", i)
		}
		bgpt.PartitionEntries = append(bgpt.PartitionEntries, pe)
	}

	return bgpt, err
}

func (part *GPTPartition) Type() PartType {
	return GUIDToString(part.PartTypeGUID)
}

func (part *GPTPartition) Description() string {
	if desc, ok := TypeDescMapping[part.Type()]; ok {
		return desc
	}
	return "Unknown"
}

func GUIDToString(byteGuid []byte) string {
	byteToChars := func(b byte) (res []byte) {
		res = make([]byte, 0, 2)
		for i := 1; i >= 0; i-- {
			switch b >> uint(4*i) & 0x0F {
			case 0:
				res = append(res, '0')
			case 1:
				res = append(res, '1')
			case 2:
				res = append(res, '2')
			case 3:
				res = append(res, '3')
			case 4:
				res = append(res, '4')
			case 5:
				res = append(res, '5')
			case 6:
				res = append(res, '6')
			case 7:
				res = append(res, '7')
			case 8:
				res = append(res, '8')
			case 9:
				res = append(res, '9')
			case 10:
				res = append(res, 'A')
			case 11:
				res = append(res, 'B')
			case 12:
				res = append(res, 'C')
			case 13:
				res = append(res, 'D')
			case 14:
				res = append(res, 'E')
			case 15:
				res = append(res, 'F')
			}
		}
		return
	}
	if len(byteGuid) != 16 {
		return ""
	}
	s := make([]byte, 0, 36)
	byteOrder := [...]int{3, 2, 1, 0, -1, 5, 4, -1, 7, 6, -1, 8, 9, -1, 10, 11, 12, 13, 14, 15}
	for _, i := range byteOrder {
		if i == -1 {
			s = append(s, '-')
		} else {
			s = append(s, byteToChars(byteGuid[i])...)
		}
	}
	return string(s)
}
