package fat

import (
	"encoding/binary"
	"fmt"

	"github.com/kisun-bit/drpkg/disk/filesystem/bitmap"
	"github.com/kisun-bit/drpkg/extend"
)

// FAT 类型
const (
	fatUnknown = iota
	fat12
	fat16
	fat32
)

const (
	fat12Threshold = 4085
	fat16Threshold = 65525
	// FAT32 合法最大簇数，防止损坏/恶意超级块导致内存耗尽或死循环
	maxFatClusters = uint64(0x0FFFFFF6)
)

// fatBootSector 对应 C 里的 struct FatBootSector（按标准 BPB 偏移解析）
type fatBootSector struct {
	sectorSize  uint16 // offset 11, 每扇区字节数
	clusterSize uint8  // offset 13, 每簇扇区数
	reserved    uint16 // offset 14, 保留扇区数
	fats        uint8  // offset 16, FAT 表份数
	dirEntries  uint16 // offset 17, 根目录项数(FAT12/16)
	sectors     uint16 // offset 19, 总扇区数(16位, 小卷)
	fatLength   uint16 // offset 22, 每FAT扇区数(FAT12/16)
	sectorCount uint32 // offset 32, 总扇区数(32位, 大卷)

	fat32Length uint32  // offset 36, 每FAT扇区数(FAT32)
	rootCluster uint32  // offset 44, FAT32根目录起始簇(暂未使用)
	extSig16    uint8   // offset 38, FAT12/16 的 boot signature
	extSig32    uint8   // offset 66, FAT32 的 boot signature
	fatName32   [8]byte // offset 82, FAT32 fs_type 字符串 "FAT32   "
}

// BitmapParser 从 FAT12/16/32 文件系统解析已用扇区位图
type BitmapParser struct {
	dev   string
	start int64
	size  int64
	fr    *extend.FsRegionReader

	sb     fatBootSector
	fsType int
	fsName string

	// 顺序读取游标（相对 region 起始偏移），模拟 C 里 read()/lseek() 的当前文件位置
	cursor int64

	// FAT12 半字节缓冲：hasNibble=false 对应 C 里 nibble==0xFF（空缓冲）
	nibble    uint16
	hasNibble bool
}

func NewBitmapParser(dev string, start int64, size int64) (bitmap.FsBitmapParser, error) {
	fr, e := extend.NewFsRegionReader(dev, start, size)
	if e != nil {
		return nil, e
	}
	return &BitmapParser{dev: dev, start: start, size: size, fr: fr}, nil
}

func (p *BitmapParser) String() string {
	return fmt.Sprintf("<FATBitmapParser(dev=%s,start=%d,size=%d)>",
		p.dev, p.start, p.size)
}

// ---- 底层顺序读取辅助（对应 C 的 read()/lseek()） ----

func (p *BitmapParser) seek(off int64) {
	p.cursor = off
}

func (p *BitmapParser) readSeq(n int) ([]byte, error) {
	buf := make([]byte, n)
	m, err := p.fr.ReadAt(buf, p.cursor)
	if err != nil {
		return nil, err
	}
	if m != n {
		return nil, fmt.Errorf("short read at offset %d: want %d got %d", p.cursor, n, m)
	}
	p.cursor += int64(n)
	return buf, nil
}

// read12 读取一个 12-bit FAT 表项，内部维护半字节缓冲状态
// 对应 C 的 read12()：FAT12 每 3 字节打包 2 个 12-bit 表项
func (p *BitmapParser) read12() (uint16, error) {
	if !p.hasNibble {
		buf, err := p.readSeq(2)
		if err != nil {
			return 0, err
		}
		buffer := binary.LittleEndian.Uint16(buf)
		p.nibble = buffer >> 12
		p.hasNibble = true
		return buffer & 0xFFF, nil
	}
	buf, err := p.readSeq(1)
	if err != nil {
		return 0, err
	}
	out := (uint16(buf[0]) << 4) | p.nibble
	p.hasNibble = false
	return out, nil
}

// ---- 引导扇区解析 ----

func (p *BitmapParser) parseBootSector() error {
	buf, err := p.readSeq(90) // 需要读到 offset 82~89 (fat32 fs_type)
	if err != nil {
		return fmt.Errorf("read boot sector: %w", err)
	}
	sb := &p.sb
	sb.sectorSize = binary.LittleEndian.Uint16(buf[11:13])
	sb.clusterSize = buf[13]
	sb.reserved = binary.LittleEndian.Uint16(buf[14:16])
	sb.fats = buf[16]
	sb.dirEntries = binary.LittleEndian.Uint16(buf[17:19])
	sb.sectors = binary.LittleEndian.Uint16(buf[19:21])
	sb.fatLength = binary.LittleEndian.Uint16(buf[22:24])
	sb.sectorCount = binary.LittleEndian.Uint32(buf[32:36])

	sb.extSig16 = buf[38] // FAT12/16 boot signature

	sb.fat32Length = binary.LittleEndian.Uint32(buf[36:40])
	sb.rootCluster = binary.LittleEndian.Uint32(buf[44:48])
	sb.extSig32 = buf[66] // FAT32 boot signature
	copy(sb.fatName32[:], buf[82:90])

	if sb.sectorSize == 0 {
		return fmt.Errorf("invalid sector size: 0")
	}
	if sb.clusterSize == 0 {
		return fmt.Errorf("invalid cluster size: 0")
	}
	return nil
}

// ---- 各种数量计算，对应 C 的 get_total_sector / get_sec_per_fat / get_root_sec / get_cluster_count ----

func (p *BitmapParser) getTotalSector() (uint64, error) {
	if p.sb.sectors != 0 {
		return uint64(p.sb.sectors), nil
	}
	if p.sb.sectorCount != 0 {
		return uint64(p.sb.sectorCount), nil
	}
	return 0, fmt.Errorf("total_sector error: sectors and sector_count are both zero")
}

func (p *BitmapParser) getSecPerFat() (uint64, error) {
	if p.sb.fatLength != 0 {
		return uint64(p.sb.fatLength), nil
	}
	if p.sb.fat32Length != 0 {
		return uint64(p.sb.fat32Length), nil
	}
	return 0, fmt.Errorf("sec_per_fat is zero")
}

func (p *BitmapParser) getRootSec() uint64 {
	return (uint64(p.sb.dirEntries)*32 + uint64(p.sb.sectorSize) - 1) / uint64(p.sb.sectorSize)
}

func roundToMultiple(n, m uint64) uint64 {
	if n == 0 || m == 0 {
		return 0
	}
	return n + m - 1 - (n-1)%m
}

func (p *BitmapParser) getClusterCount() (uint64, error) {
	totalSector, err := p.getTotalSector()
	if err != nil {
		return 0, err
	}
	rootSec := p.getRootSec()
	secPerFat, err := p.getSecPerFat()
	if err != nil {
		return 0, err
	}
	reserved := uint64(p.sb.reserved) + uint64(p.sb.fats)*secPerFat + rootSec
	if reserved > totalSector {
		return 0, nil
	}
	dataSec := totalSector - reserved
	return dataSec / uint64(p.sb.clusterSize), nil
}

// getFatType 判断 FAT12/16/32，对应 C 的 get_fat_type()
func (p *BitmapParser) getFatType() error {
	sb := &p.sb
	if sb.extSig16 == 0x29 || (sb.fatLength != 0 && sb.fat32Length == 0) {
		totalSector, err := p.getTotalSector()
		if err != nil {
			return err
		}
		logicalSectorSize := uint64(sb.sectorSize)
		secPerFat, err := p.getSecPerFat()
		if err != nil {
			return err
		}
		rootStart := (uint64(sb.reserved) + uint64(sb.fats)*secPerFat) * logicalSectorSize
		dataStart := rootStart + roundToMultiple(uint64(sb.dirEntries)<<5, logicalSectorSize)
		dataSize := int64(totalSector*logicalSectorSize) - int64(dataStart)
		if dataSize <= 0 {
			return fmt.Errorf("data_size count error")
		}
		clusters := uint64(dataSize) / (uint64(sb.clusterSize) * logicalSectorSize)
		if clusters == 0 {
			return fmt.Errorf("clusters count error")
		}
		if clusters >= fat12Threshold {
			p.fsType = fat16
			p.fsName = "FAT16"
			if clusters >= fat16Threshold {
				// 对应 C 的 log_mesg 警告：簇数超出 FAT16 上限，仅记录不中断
			}
		} else {
			p.fsType = fat12
			p.fsName = "FAT12"
		}
	} else if sb.fatName32[4] == '2' || (sb.fatLength == 0 && sb.fat32Length != 0) {
		p.fsType = fat32
		p.fsName = "FAT32"
	} else {
		return fmt.Errorf("unknown fat type")
	}
	return nil
}

// ---- 卷状态检查，对应 C 的 check_fat_status() ----
// 返回值：0 正常，1 未正常卸载，2 I/O 错误
func (p *BitmapParser) checkFatStatus() (int, error) {
	switch p.fsType {
	case fat16:
		if _, err := p.readSeq(2); err != nil { // FAT[0] media byte
			return 2, err
		}
		buf, err := p.readSeq(2) // FAT[1] dirty flag
		if err != nil {
			return 2, err
		}
		entry := binary.LittleEndian.Uint16(buf)
		if entry&0x8000 == 0 {
			return 1, nil
		}
		if entry&0x4000 == 0 {
			return 2, nil
		}
		return 0, nil

	case fat32:
		if _, err := p.readSeq(4); err != nil {
			return 2, err
		}
		buf, err := p.readSeq(4)
		if err != nil {
			return 2, err
		}
		entry := binary.LittleEndian.Uint32(buf)
		if entry&0x08000000 == 0 {
			return 1, nil
		}
		if entry&0x04000000 == 0 {
			return 2, nil
		}
		return 0, nil

	case fat12:
		if _, err := p.read12(); err != nil { // FAT[0]
			return 2, err
		}
		if _, err := p.read12(); err != nil { // FAT[1]，FAT12 无脏位标记，仅跳过
			return 2, err
		}
		return 0, nil

	default:
		return 2, fmt.Errorf("wrong fs type")
	}
}

// ---- 保留区标记，对应 C 的 mark_reserved_sectors() ----
func (p *BitmapParser) markReservedSectors(fb *bitmap.FsBitmap, block uint64) (uint64, error) {
	secPerFat, err := p.getSecPerFat()
	if err != nil {
		return block, err
	}
	rootSec := p.getRootSec()

	// A) 保留扇区
	for i := uint64(0); i < uint64(p.sb.reserved); i++ {
		fb.Set(block)
		block++
	}
	// B) FAT 表占用的扇区
	for j := uint8(0); j < p.sb.fats; j++ {
		for i := uint64(0); i < secPerFat; i++ {
			fb.Set(block)
			block++
		}
	}
	// C) 根目录占用的扇区（FAT32 没有独立根目录区）
	if rootSec > 0 {
		for i := uint64(0); i < rootSec; i++ {
			fb.Set(block)
			block++
		}
	}
	return block, nil
}

// ---- 逐簇状态检查，对应 check_fat12/16/32_entry() ----

func (p *BitmapParser) markCluster(fb *bitmap.FsBitmap, block uint64, used bool) uint64 {
	clusterSize := uint64(p.sb.clusterSize)
	for i := uint64(0); i < clusterSize; i++ {
		if used {
			fb.Set(block)
		} else {
			fb.Clear(block)
		}
		block++
	}
	return block
}

func (p *BitmapParser) checkFat32Entry(fb *bitmap.FsBitmap, block uint64) (uint64, error) {
	buf, err := p.readSeq(4)
	if err != nil {
		return block, err
	}
	entry := binary.LittleEndian.Uint32(buf)
	switch entry {
	case 0x0FFFFFF7: // 坏簇
		return p.markCluster(fb, block, false), nil
	case 0x00000000: // 空闲
		return p.markCluster(fb, block, false), nil
	default: // 已用
		return p.markCluster(fb, block, true), nil
	}
}

func (p *BitmapParser) checkFat16Entry(fb *bitmap.FsBitmap, block uint64) (uint64, error) {
	buf, err := p.readSeq(2)
	if err != nil {
		return block, err
	}
	entry := binary.LittleEndian.Uint16(buf)
	switch entry {
	case 0xFFF7:
		return p.markCluster(fb, block, false), nil
	case 0x0000:
		return p.markCluster(fb, block, false), nil
	default:
		return p.markCluster(fb, block, true), nil
	}
}

func (p *BitmapParser) checkFat12Entry(fb *bitmap.FsBitmap, block uint64) (uint64, error) {
	entry, err := p.read12()
	if err != nil {
		return block, err
	}
	switch entry {
	case 0xFF7:
		return p.markCluster(fb, block, false), nil
	case 0x000:
		return p.markCluster(fb, block, false), nil
	default:
		return p.markCluster(fb, block, true), nil
	}
}

// ---- 主流程，对应 read_super_blocks() + read_bitmap() ----

func (p *BitmapParser) Dump() (bitmapOut *bitmap.FsBitmap, err error) {
	defer func() {
		if p.fr != nil {
			_ = p.fr.Close()
		}
	}()

	p.seek(0)
	if err := p.parseBootSector(); err != nil {
		return nil, err
	}
	if err := p.getFatType(); err != nil {
		return nil, err
	}

	totalSector, err := p.getTotalSector()
	if err != nil {
		return nil, err
	}

	clusterCount, err := p.getClusterCount()
	if err != nil {
		return nil, err
	}
	if clusterCount > maxFatClusters {
		return nil, fmt.Errorf(
			"ERROR: maliciously large cluster_count detected: %d, max allowed: %d",
			clusterCount, maxFatClusters)
	}

	fb := bitmap.NewFsBitmap(p.fsName, bitmap.BitmapFromFS, int64(totalSector), int(p.sb.sectorSize))

	// 初始状态全部置为“已用”，对应 C 的 pc_init_bitmap(bitmap, 0xFF, total_sector)
	fb.SetAll()

	// A) B) C): 保留扇区 / FAT 表 / 根目录标记已用
	block, err := p.markReservedSectors(fb, 0)
	if err != nil {
		return nil, err
	}

	// 跳到第一份 FAT 表起始位置（保留扇区之后）
	fatReservedBytes := int64(p.sb.sectorSize) * int64(p.sb.reserved)
	p.seek(fatReservedBytes)
	p.hasNibble = false

	// 用第一份 FAT 的前两项检查卷状态（脏位标记）
	fatStat, err := p.checkFatStatus()
	if err != nil {
		return nil, fmt.Errorf("check fat status: %w", err)
	}
	switch fatStat {
	case 1:
		return nil, fmt.Errorf("filesystem isn't in a valid state (not cleanly unmounted)")
	case 2:
		return nil, fmt.Errorf("I/O error while checking fat status")
	}

	// D) 逐簇扫描：从数据区第一个簇（cluster 2）开始
	for i := uint64(0); i < clusterCount; i++ {
		if block >= totalSector {
			return nil, fmt.Errorf("block too large: block=%d total_sector=%d", block, totalSector)
		}
		switch p.fsType {
		case fat16:
			block, err = p.checkFat16Entry(fb, block)
		case fat32:
			block, err = p.checkFat32Entry(fb, block)
		case fat12:
			block, err = p.checkFat12Entry(fb, block)
		default:
			err = fmt.Errorf("unknown fs type")
		}
		if err != nil {
			return nil, fmt.Errorf("read fat entry %d: %w", i, err)
		}
	}

	// 簇数计算后仍剩余的尾部扇区（对齐/填充区）统一标记为已用
	// 对应 C 的 get_used_block() 里 `while(block < total_sector) pc_set_bit(...)`
	for ; block < totalSector; block++ {
		fb.Set(block)
	}

	return fb, nil
}
