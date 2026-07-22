package xfs

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync/atomic"
	"unsafe"

	"github.com/davecgh/go-spew/spew"
	"github.com/dustin/go-humanize"
	"github.com/kisun-bit/drpkg/define"
	"github.com/kisun-bit/drpkg/disk/filesystem/bitmap"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/pkg/errors"
)

type XfsBitmapParser struct {
	dev   string
	start int64
	size  int64
	fr    *extend.FsRegionReader

	sb       SuperBlock
	fsBitmap *bitmap.FsBitmap
	freeBits int64
}

func NewBitmapParser(dev string, start int64, size int64) (bitmap.FsBitmapParser, error) {
	fr, e := extend.NewFsRegionReader(dev, start, size)
	if e != nil {
		return nil, e
	}
	return &XfsBitmapParser{dev: dev, start: start, size: size, fr: fr}, nil
}

func (p *XfsBitmapParser) String() string {
	return fmt.Sprintf("<XfsBitmapParser(dev=%s,start=%d,size=%d)>",
		p.dev, p.start, p.size)
}

func (p *XfsBitmapParser) Dump() (*bitmap.FsBitmap, error) {

	//
	// 读取主超级块
	//

	if err := binary.Read(p.fr, binary.BigEndian, &p.sb); err != nil {
		return nil, errors.Wrapf(err, "read superblock")
	}
	if p.sb.Magicnum != XFS_SB_MAGIC {
		return nil, errors.Errorf("wrong magic number of superblock")
	}
	logger.Debugf("%s.Dump() superblock: \n%s", p, spew.Sdump(p.sb))

	blockSize := int64(p.sb.BlockSize)
	agBlocks := int64(p.sb.Agblocks)
	agCount := p.sb.Agcount

	// 初始化位图（总块数 = sb_dblocks）
	p.fsBitmap = bitmap.NewFsBitmap(define.FsTypeXFS, bitmap.BitmapFromFS, int64(p.sb.Dblocks), int(blockSize))
	p.fsBitmap.SetAll()

	//
	// 遍历每个AG
	//

	for agno := uint32(0); agno < agCount; agno++ {
		agOffset := int64(agno) * agBlocks * blockSize

		logger.Debugf("%s.Dump() ########## AG%02d ##########", p, agno)

		var agf AGF
		if _, err := p.fr.Seek(agOffset+1*int64(p.sb.Sectsize), io.SeekStart); err != nil {
			return nil, errors.Wrapf(err, "seek")
		}
		if err := binary.Read(p.fr, binary.BigEndian, &agf); err != nil {
			return nil, errors.Wrapf(err, "read")
		}
		if agf.Magicnum != XFS_AGF_MAGIC {
			return nil, errors.Errorf("wrong magic number of agf")
		}
		logger.Debugf("%s.Dump() AG%02d: AGF:\n%s", p, agno, spew.Sdump(agf))

		var agi AGI
		if _, err := p.fr.Seek(agOffset+2*int64(p.sb.Sectsize), io.SeekStart); err != nil {
			return nil, errors.Wrapf(err, "seek")
		}
		if err := binary.Read(p.fr, binary.BigEndian, &agi); err != nil {
			return nil, errors.Wrapf(err, "read")
		}
		if agi.Magicnum != XFS_AGI_MAGIC {
			return nil, errors.Errorf("wrong magic number of agi")
		}
		logger.Debugf("%s.Dump() AG%02d: AGI:\n%s", p, agno, spew.Sdump(agi))

		var agfl AGFL
		if _, err := p.fr.Seek(agOffset+3*int64(p.sb.Sectsize), io.SeekStart); err != nil {
			return nil, errors.Wrapf(err, "seek")
		}
		// 注：v4（非CRC）文件系统的 AGFL 没有这个头部，整个扇区都是 bno 数组，
		// 这里假设是 CRC(v5) 场景；若要兼容 v4，需要判断 hasCrc() 后跳过本次读取。
		if err := binary.Read(p.fr, binary.BigEndian, &agfl); err != nil {
			return nil, errors.Wrapf(err, "read")
		}
		if agfl.Magicnum != XFS_AGFL_MAGIC {
			return nil, errors.Errorf("wrong magic number of agfl")
		}
		logger.Debugf("%s.Dump() AG%02d: AGFL:\n%s", p, agno, spew.Sdump(agfl))

		bnoArr := make([]uint32, p.sb.agflBnoCount())
		if err := binary.Read(p.fr, binary.BigEndian, &bnoArr); err != nil {
			return nil, errors.Wrapf(err, "read")
		}
		logger.Debugf("%s.Dump() AG%02d: BNO Array:\n%s", p, agno, spew.Sdump(bnoArr))

		// 1. 扫描 AGFL 空闲链表
		if err := p.scanFreeList(agno, agf, bnoArr); err != nil {
			return nil, errors.Wrapf(err, "AG%02d scan freelist", agno)
		}

		// 2. 扫描 bnobt（按块号索引的空闲空间树），登记 B+树管理的空闲块
		if err := p.scanSbtreeBno(agf, agOffset, agf.Roots[0], int(agf.Levels[0])); err != nil {
			return nil, errors.Wrapf(err, "AG%02d scan bnobt", agno)
		}
	}

	//logger.Debugf("%s.Dump() bitmap:\n%s", p, hex.Dump(p.fsBitmap.Bitmap))

	usedBlks := int64(p.sb.Dblocks) - p.freeBits
	effectBytes := usedBlks * int64(p.fsBitmap.BlockSize)
	logger.Debugf("%s.Dump() usedblocks=%d, blocksize=%d, effectivebytes=%dB(%s/%s)",
		p,
		usedBlks,
		p.fsBitmap.BlockSize,
		effectBytes,
		humanize.IBytes(uint64(effectBytes)),
		humanize.IBytes(uint64(p.size)))

	return p.fsBitmap, nil
}

// scanFreeList 扫描 AGFL 空闲链表，把其中记录的空闲块登记到位图中
// 参考：xfsprogs repair/scan.c 中的 scan_freelist()
func (p *XfsBitmapParser) scanFreeList(agno uint32, agf AGF, bnoArr []uint32) error {
	// agf_flcount == 0 表示空闲链表为空，无需扫描
	if agf.Flcount == 0 {
		return nil
	}

	size := len(bnoArr)

	// 校验 flfirst/fllast 是否越界，越界说明 AGF 数据有问题，跳过扫描
	if int(agf.Flfirst) >= size || int(agf.Fllast) >= size {
		logger.Warnf("%s.scanFreeList() AG%02d agf freelist index bad (flfirst=%d fllast=%d size=%d), skip",
			p, agno, agf.Flfirst, agf.Fllast, size)
		return nil
	}

	i := agf.Flfirst
	for {
		bno := bnoArr[i]

		// 登记该块为空闲块（1个block）
		p.markFree(agno, bno, 1)

		if i == agf.Fllast {
			break
		}

		i++
		if int(i) == size {
			i = 0
		}
	}

	return nil
}

// scanSbtreeBno 对应内核 scan_sbtree()（这里专门针对 bnobt，固定分发到 scanFuncBno）
// agOffset: 该 AG 起始的字节偏移；agbno: 该 btree block 在 AG 内的相对块号
func (p *XfsBitmapParser) scanSbtreeBno(agf AGF, agOffset int64, agbno uint32, nlevels int) error {
	blockSize := int64(p.sb.BlockSize)
	offset := agOffset + int64(agbno)*blockSize

	data, err := p.readBlockAt(offset, blockSize)
	if err != nil {
		// 与内核一致：读失败只记日志、不当作致命错误，直接跳过
		logger.Debugf("%s.scanSbtreeBno() AG%02d can't read btree block %d: %v",
			p, agf.Seqno, agbno, err)
		return nil
	}

	return p.scanFuncBno(agf, data, nlevels-1)
}

// scanFuncBno 对应内核 scanfunc_bno()
func (p *XfsBitmapParser) scanFuncBno(agf AGF, data []byte, level int) error {
	order := binary.BigEndian

	var hdr BtreeShortBlock
	hdr.Magicnum = order.Uint32(data[0:4])
	hdr.Level = order.Uint16(data[4:6])
	hdr.Numrecs = order.Uint16(data[6:8])
	hdr.Leftsib = order.Uint32(data[8:12])
	hdr.Rightsib = order.Uint32(data[12:16])

	var headerLen int

	logger.Debugf("%s.scanFuncBno() magicnum=%v", p, hdr.Magicnum)

	switch hdr.Magicnum {
	case XFS_ABTB_CRC_MAGIC:
		// 用 block 自身的 magic 判断，而不是 sb.hasCrc()，避免两者不一致时读串位置
		headerLen = binary.Size(hdr) // 56
		if len(data) < headerLen {
			return errors.Errorf("block too small: %d", len(data))
		}
		hdr.Blkno = order.Uint64(data[16:24])
		hdr.Lsn = order.Uint64(data[24:32])
		copy(hdr.UUID[:], data[32:48])
		hdr.Owner = order.Uint32(data[48:52])
		hdr.CRC = binary.LittleEndian.Uint32(data[52:56])
	case XFS_ABTB_MAGIC:
		headerLen = 16
		if len(data) < headerLen {
			return errors.Errorf("block too small: %d", len(data))
		}
	default:
		logger.Debugf("%s.scanFuncBno() bb_magic error: %x", p, hdr.Magicnum)
		return nil
	}

	numrecs := int(hdr.Numrecs)
	blockSize := int64(p.sb.BlockSize)

	if level == 0 {
		recBase := headerLen
		for i := 0; i < numrecs; i++ {
			off := recBase + i*8
			if off+8 > len(data) {
				return errors.Errorf("record %d out of range", i)
			}
			startBlock := order.Uint32(data[off : off+4])
			blockCount := order.Uint32(data[off+4 : off+8])

			// 加一层健全性检查：单个 extent 不可能超过整个 AG 的大小，
			// 一旦出现就说明解析有问题，及时报错而不是悄悄写入错误的位图数据
			if uint64(startBlock)+uint64(blockCount) > uint64(agf.Length) {
				return errors.Errorf(
					"AG%02d bnobt leaf record out of range: start=%d count=%d aglen=%d (headerLen=%d numrecs=%d)",
					agf.Seqno, startBlock, blockCount, agf.Length, headerLen, numrecs)
			}

			p.markFree(agf.Seqno, startBlock, blockCount)
		}
		return nil
	}

	// 非叶子节点：指针数组紧跟在"最大记录数个 record 槽位"之后
	maxrecs := allocBtreeMaxRecs(blockSize, headerLen, false)
	ptrBase := headerLen + maxrecs*8

	agOffset := int64(agf.Seqno) * int64(p.sb.Agblocks) * blockSize

	for i := 0; i < numrecs; i++ {
		off := ptrBase + i*4
		if off+4 > len(data) {
			return errors.Errorf("ptr %d out of range", i)
		}
		childAgbno := order.Uint32(data[off : off+4])

		if err := p.scanSbtreeBno(agf, agOffset, childAgbno, level); err != nil {
			return err
		}
	}

	return nil
}

// readBlockAt 从设备读取一个完整的 fs block
func (p *XfsBitmapParser) readBlockAt(offset int64, blockSize int64) ([]byte, error) {
	buf := make([]byte, blockSize)
	if _, err := p.fr.Seek(offset, io.SeekStart); err != nil {
		return nil, errors.Wrapf(err, "seek to %d", offset)
	}
	if _, err := io.ReadFull(p.fr, buf); err != nil {
		return nil, errors.Wrapf(err, "read block at %d", offset)
	}
	return buf, nil
}

// markFree 把 (AG号, AG内相对块号, 长度) 换算成设备绝对块号，
// 在位图中清除对应的 bit（表示这段块是空闲的）
func (p *XfsBitmapParser) markFree(agno uint32, agbno uint32, length uint32) {
	atomic.AddInt64(&p.freeBits, int64(length))
	startBlock := uint64(agno)*uint64(p.sb.Agblocks) + uint64(agbno)
	logger.Debugf("%s.setBitmap() agno=%d agbno=%d len=%d startblk=%d", p, agno, agbno, length, startBlock)
	p.fsBitmap.ClearRange(startBlock, length)
}

func (sb *SuperBlock) agflBnoCount() int {
	size := int(sb.Sectsize)
	if sb.hasCrc() {
		size -= int(unsafe.Sizeof(AGFL{}))
	}
	return size / int(unsafe.Sizeof(uint32(0)))
}

func (sb *SuperBlock) version() XfsSbVersion {
	return XfsSbVersion(sb.Versionnum & uint16(XFS_SB_VERSION_NUMBITS))
}

func (sb *SuperBlock) hasCrc() bool {
	return sb.version() == XFS_SB_VERSION_5
}

func (sb *SuperBlock) hasFeature(bit XfsSbVersionBit) bool {
	return sb.Versionnum&uint16(bit) != 0
}

// allocBtreeMaxRecs 对应内核 xfs_allocbt_maxrecs()：
// 计算给定 header 长度下，bnobt/cntbt 一个满 block 最多能容纳多少条 record（叶子）
// 或多少组 key+ptr（非叶子）
func allocBtreeMaxRecs(blockSize int64, headerLen int, leaf bool) int {
	blocklen := int(blockSize) - headerLen
	const recSize = 8 // xfs_alloc_rec_t / xfs_alloc_key_t: startblock(4)+blockcount(4)
	const ptrSize = 4 // xfs_alloc_ptr_t
	if leaf {
		return blocklen / recSize
	}
	return blocklen / (recSize + ptrSize)
}
