package xfs

import (
	"encoding/hex"
	"fmt"
	"github.com/kisun-bit/drpkg/sys/ioctl"
	"github.com/kisun-bit/drpkg/util/basic"
	"github.com/kisun-bit/drpkg/util/logger"
	"github.com/lunixbochs/struc"
	"github.com/masahiro331/go-xfs-filesystem/xfs"
	"github.com/pkg/errors"
	"io"
	"math"
	"os"
)

// Extract 导出含有XFS文件系统的设备的位图.
//
// XFS的提取有效数据位图的逻辑参考:
// 1. https://blog.csdn.net/scaleqiao/article/details/52098546
// 2. https://zhuanlan.zhihu.com/p/353919834
// 3. https://zhuanlan.zhihu.com/p/354173738
// 4. https://github.com/torvalds/linux/tree/d2b6f8a179194de0ffc4886ffc2c4358d86047b8/fs/xfs
func Extract(device string) (clusterSize int, bitmapBinary []byte, err error) {
	logger.Debugf("XFS Extract(%s). Enter", device)

	deviceHandle, err := os.Open(device)
	if err != nil {
		return 0, nil, err
	}
	defer deviceHandle.Close()

	size, err := ioctl.QueryFileSize(device)
	if err != nil {
		return 0, nil, err
	}
	r := *io.NewSectionReader(deviceHandle, 0, int64(size))
	xfsHandle, err := xfs.NewFS(r, nil)
	if err != nil {
		return 0, nil, err
	}
	defer xfsHandle.Close()

	isV5Plus := xfsHandle.PrimaryAG.SuperBlock.Versionnum&0xF == 0x5
	if isV5Plus {
		logger.Debugf("XFS Extract(%s). Version is v5", device)
	}

	logger.Debugf("XFS Extract(%s). Version is %v",
		device, xfsHandle.PrimaryAG.SuperBlock.Versionnum)
	logger.Debugf("XFS Extract(%s). Sector size is %v",
		device, xfsHandle.PrimaryAG.SuperBlock.Sectsize)
	logger.Debugf("XFS Extract(%s). Block size is %v",
		device, xfsHandle.PrimaryAG.SuperBlock.BlockSize)
	logger.Debugf("XFS Extract(%s). DB blocks is %v",
		device, xfsHandle.PrimaryAG.SuperBlock.Dblocks)
	logger.Debugf("XFS Extract(%s). AG blocks is %v",
		device, xfsHandle.PrimaryAG.SuperBlock.Agblocks)
	logger.Debugf("XFS Extract(%s). AG number is %v",
		device, xfsHandle.PrimaryAG.SuperBlock.Agcount)
	logger.Debugf("XFS Extract(%s). AG array size is %v",
		device, len(xfsHandle.AGs))
	logger.Debugf("XFS Extract(%s). AG UUID is %v",
		device, hexUUID(xfsHandle.PrimaryAG.SuperBlock.UUID))
	logger.Debugf("XFS Extract(%s). AG Meta UUID is %v",
		device, hexUUID(xfsHandle.PrimaryAG.SuperBlock.MetaUUID))

	bitmapBytesLen := int64(math.Ceil(float64(xfsHandle.PrimaryAG.SuperBlock.Dblocks) / float64(8)))
	bitmapBinary = make([]byte, bitmapBytesLen)
	clusterSize = int(xfsHandle.PrimaryAG.SuperBlock.BlockSize)

	logger.Debugf("XFS Extract(%s). Bitmap bytes size is %v", device, bitmapBytesLen)
	logger.Debugf("XFS Extract(%s). Block size is %v", device, clusterSize)

	basic.SetBits(bitmapBinary, int(xfsHandle.PrimaryAG.SuperBlock.Dblocks))

	// XFS文件系统由一组组AG构成. 每一个AG可以理解为一个小XFS.
	// |<--------------------            XFS              ----------------------->|
	// +--------------+--------------+--------------+--------------+--------------+
	// |     AG-0     |     AG-1     |     AG-2     |     ....     |     AG-N     |
	// +--------------+--------------+--------------+--------------+--------------+
	for i := int64(xfsHandle.PrimaryAG.SuperBlock.Agcount) - 1; i >= 0; i-- {
		ag := xfsHandle.AGs[i]

		curAGDesc := fmt.Sprintf("AG%v", i)
		logger.Debugf("<<<<<<<<<<<<<<<<<<<< %s Start parsing >>>>>>>>>>>>>>>>>>>>", curAGDesc)

		logger.Debugf("AGF flfirst=%v", ag.Agf.Flfirst)
		logger.Debugf("AGF fllast=%v", ag.Agf.Fllast)
		logger.Debugf("AGF flcount=%v", ag.Agf.Flcount)
		logger.Debugf("AGF freeblks=%v", ag.Agf.Freeblks)
		logger.Debugf("AGF longest=%v", ag.Agf.Longest)
		logger.Debugf("AGF btreeblks=%v", ag.Agf.Btreeblks)
		logger.Debugf("AGF uuid=%v", hexUUID(ag.Agf.UUID))

		// agf_roots[]里保存的是AGF用来管理空间所需的几个结构的头地址，本来过去有两个(bnoroot, cntroot)，
		// 现在有四个(bnoroot, cntroot, rmaproot, refcntroot)。但是因为历史原因，
		// 导致引入refcntroot时已经不方便将其加入到这个连续的数组内了，否则容易打破当前的agf结构顺序。
		// 而且refcount tree也不是一个真正的索引used space/free space的tree，所以就单独把它拿出来放到后面了。
		// 所以这里只保存三个，依次是：
		// bnoroot=1表示以block number为key的free space管理所用的B+tree的头部在当前XFS的第1个block里（从0开始数的）。
		// cntroot=2表示以block count为key的free space管理所用的B+tree的头部在当前XFS的第2个block里。
		// rmaproot=5表示对used space的reverse map B+tree的头部在当前XFS的第5个block里
		logger.Debugf("AGF bnoroot=%v bnolevel=%v", ag.Agf.Roots[0], ag.Agf.Levels[0])
		logger.Debugf("AGF cntroot=%v cntlevel=%v", ag.Agf.Roots[1], ag.Agf.Levels[1])

		// 遍历BNO树,得到空闲块信息集合
		freeRecs, err := collectFreeRecsOnBNOBtree(r, i, xfsHandle.PrimaryAG.SuperBlock, ag.Agf.Roots[0])
		if err != nil {
			return 0, nil, err
		}
		freeBlocksCount := int64(0)
		if len(freeRecs) > 0 {
			for _, rec := range freeRecs {
				freeBlocksCount += int64(rec.BlockCount)
			}
		}
		logger.Debugf("AGF freeBlocksCount=%v freeblks=%v", freeBlocksCount, ag.Agf.Freeblks)
		if freeBlocksCount != int64(ag.Agf.Freeblks) {
			return 0, nil, errors.Errorf(
				"freeblks(%v) of agf is not equals to freeblks(%v) of AllocRecs", ag.Agf.Freeblks, freeBlocksCount)
		}

		// 根据freeRecs, 修改本AG内的位图(若未占用则改回0).
		if len(freeRecs) > 0 {
			for _, rec := range freeRecs {
				// rec.StartBlock索引相对于本AG内,且从0开始.
				ba := agfAbsBlockAddr(i, xfsHandle.PrimaryAG.SuperBlock, rec.StartBlock)
				for j := uint32(0); j < rec.BlockCount; j++ {
					idx := ba + int64(j)
					basic.SetBit(bitmapBinary, idx, false)
				}
			}
		}
	}

	return clusterSize, bitmapBinary, nil
}

func agfBNOHeaderOffset(agIndex int64, sb xfs.SuperBlock, bnoBlockStart uint32) int64 {
	return agIndex*int64(sb.Agblocks)*int64(sb.BlockSize) + int64(bnoBlockStart)*int64(sb.BlockSize)
}

func agfAbsBlockAddr(agIndex int64, sb xfs.SuperBlock, relBlockAddr uint32) int64 {
	return agIndex*int64(sb.Agblocks) + int64(relBlockAddr)
}

func hexUUID(uuid_ [16]byte) string {
	return hex.EncodeToString(uuid_[:])
}

func getBNOHeader(r io.SectionReader, agIndex int64, sb xfs.SuperBlock, bnoBlockStart uint32) (BtreeBNOHeader, error) {
	// AGF中的bnoroot指向AG的第二个block，这个block作为以block number为key的B+tree的根，用于管理未被占用的blocks.
	// 在首个block中含有一个xfs_btree_block结构：
	// struct xfs_btree_block {
	//                  bb_magic;       /* magic number for block type */
	//         __be16          bb_level;       /* 0 is a leaf */
	//         __be16          bb_numrecs;     /* current # of data records */
	//         union {
	//                 struct xfsBtreeBlockShdr s;
	//                 struct xfs_btree_block_lhdr l;
	//         } bb_u;                         /* rest */
	// };
	// 其中有一个联合体，一个是short format block header(48)，一个是long format block header64)，
	// 其中short format用于AG内部寻址的B+tree，long format用于全XFS（可跨AG寻址）的B+tree.

	// 如何确定他到底是xfs_btree_block_shdr还是xfs_btree_block_lhdr呢？？？
	// 读取其uuid属性, 并匹配块的UUID，必须与sb_uuid或sb_meta_uuid匹配，它们依赖于特性设置.
	agfBnoOff := agfBNOHeaderOffset(agIndex, sb, bnoBlockStart)

	btreeS := BtreeBlockS{}
	btreeL := BtreeBlockL{}

	var btreeHeader BtreeBNOHeader

	_ = struc.Unpack(io.NewSectionReader(&r, agfBnoOff, int64(sb.BlockSize)), &btreeS)
	_ = struc.Unpack(io.NewSectionReader(&r, agfBnoOff, int64(sb.BlockSize)), &btreeL)

	isBtreeFormatByShort := false
	blankUUID := "00000000000000000000000000000000"
	if hexUUID(btreeS.UUID) == hexUUID(sb.UUID) && hexUUID(btreeS.UUID) != blankUUID {
		isBtreeFormatByShort = true
		btreeHeader = btreeS
	} else if hexUUID(btreeL.UUID) == hexUUID(sb.UUID) && hexUUID(btreeL.UUID) != blankUUID {
		isBtreeFormatByShort = false
		btreeHeader = btreeL
	} else if hexUUID(btreeS.UUID) == hexUUID(sb.MetaUUID) && hexUUID(btreeS.UUID) != blankUUID {
		isBtreeFormatByShort = true
		btreeHeader = btreeS
	} else if hexUUID(btreeL.UUID) == hexUUID(sb.MetaUUID) && hexUUID(btreeL.UUID) != blankUUID {
		isBtreeFormatByShort = false
		btreeHeader = btreeL
	} else {
		return nil, errors.Errorf(
			"uuid of btree at bnoroot(%v) doesn't matched sb_uuid(/sb_meta_uuid)", bnoBlockStart)
	}

	// XFS V5是AB3B，V4是ABTB
	magic := btreeHeader.GetMagic()
	if !(string(magic[:]) == "AB3B" || string(magic[:]) == "ABTB") {
		return nil, errors.Errorf("bno maigc(%s) is not legal, it must be AB3B or ABTB", string(magic[:]))
	}

	if isBtreeFormatByShort {
		logger.Debugf("BNO format_type=`short format`")
	} else {
		logger.Debugf("BNO format_type=`long format`")
	}
	logger.Debugf("BNO %v bb_magic=%s", bnoBlockStart, btreeHeader.GetMagic())
	logger.Debugf("BNO %v bb_level=%v", bnoBlockStart, btreeHeader.GetLevel())
	logger.Debugf("BNO %v bb_numrecs=%v", bnoBlockStart, btreeHeader.GetNumrecs())
	logger.Debugf("BNO %v bb_leftsib=%v", bnoBlockStart, btreeHeader.GetLeftsib())
	logger.Debugf("BNO %v bb_rightsib=%v", bnoBlockStart, btreeHeader.GetRightsib())
	logger.Debugf("BNO %v bb_blkno=%v", bnoBlockStart, btreeHeader.GetBlkno())
	logger.Debugf("BNO %v bb_lsn=%#x", bnoBlockStart, btreeHeader.GetLsn())
	logger.Debugf("BNO %v bb_uuid=%v", bnoBlockStart, hexUUID(btreeHeader.GetUUID()))
	logger.Debugf("BNO %v bb_crc=%x", bnoBlockStart, btreeHeader.GetCRC())
	logger.Debugf("BNO %v bb_pad=%v", bnoBlockStart, btreeHeader.GetPad())
	logger.Debugf("BNO %v bno_header_size=%v", bnoBlockStart, btreeHeader.Size())

	return btreeHeader, nil
}

func collectBNOKeyListAndPtrList(r io.SectionReader, agIndex int64, sb xfs.SuperBlock, bnoBlockStart uint32, bnoHeader BtreeBNOHeader) ([]AllocRec, []AllocPtr, error) {
	headerOff := agfBNOHeaderOffset(agIndex, sb, bnoBlockStart)
	keyStartOff := headerOff + int64(bnoHeader.Size())
	_, err := r.Seek(keyStartOff, io.SeekStart)
	if err != nil {
		return nil, nil, errors.Errorf("failed to seek device to %v", keyStartOff)
	}

	var keys []AllocRec
	for i := uint16(0); i < bnoHeader.GetNumrecs(); i++ {
		var key AllocRec
		err = struc.Unpack(&r, &key)
		if err != nil {
			return nil, nil, errors.Errorf("failed to parse AllocRec(%v)", i)
		}
		//logger.Debugf("[%v, %v]", key.StartBlock, key.BlockCount)
		keys = append(keys, key)
	}

	// 非叶子节点, 便解析其ptr索引.
	var ptrs []AllocPtr
	if bnoHeader.GetLevel() > 0 {

		// 获取需要偏移对齐的字节数.(参考btblock_ptr_offset函数).
		// 通过计算出最大可能的bb_numrecs值，即xfs_alloc_ptr前面最大可能有多少个xfs_alloc_key，
		// 预留出需要的最大的空闲地址之后，就是xfs_alloc_ptr的开始地址,具体计算是这样：
		// 一个节点占用一个block，这里默认的block大小为4096
		// 减去前面xfs_btree_block所占的空间，由于ABTB为“short form block”，即是XFS_BTREE_SBLOCK_LEN=16，那么，4096-16=4080
		// 最大可能的bb_numrecs值为：4080/(sizeof(xfs_alloc_key)+sizeof(xfs_alloc_ptr))=4080/(8+4)=340
		// xfs_alloc_ptr的开始地址（相对本block偏移）xfs_alloc_ptr[0]=16+340*8=2736=0xAB0
		allocPtrRelStart := btreePtrRelOffset(int(sb.BlockSize), int(bnoHeader.Size()))
		allocPtrAbsStart := headerOff + int64(allocPtrRelStart)
		logger.Debugf("allocPtrRelStart=%v, allocPtrAbsStart=%v", allocPtrRelStart, allocPtrAbsStart)
		_, err = r.Seek(allocPtrAbsStart, io.SeekStart)
		if err != nil {
			return nil, nil, errors.Errorf("failed to seek device to %v for parsing alloc ptrs", allocPtrAbsStart)
		}

		for i := uint16(0); i < bnoHeader.GetNumrecs(); i++ {
			var ptr AllocPtr
			err = struc.Unpack(&r, &ptr)
			if err != nil {
				return nil, nil, errors.Errorf("failed to parse AllocPtr(%v)", i)
			}
			//logger.Debugf("%v:%v", i+1, ptr)
			ptrs = append(ptrs, ptr)
		}
	}
	return keys, ptrs, nil
}

func collectFreeRecsOnBNOBtree(r io.SectionReader, agIndex int64, sb xfs.SuperBlock, bnoBlockStart uint32) (freeRecs []AllocRec, err error) {
	// 遍历B+树时, 此函数递归时的会发生变化的参数是bnoBlockStart.
	err = collectFreeBlockListLogic(r, agIndex, sb, bnoBlockStart, &freeRecs)
	//for _, _rec := range freeRecs {
	//	logger.Debugf("freeRecs: %v-%v", _rec.StartBlock, _rec.BlockCount)
	//}
	return freeRecs, err
}

func collectFreeBlockListLogic(r io.SectionReader, agIndex int64, sb xfs.SuperBlock, bnoBlockStart uint32, freeRecs *[]AllocRec) (err error) {
	bnoHeader, err := getBNOHeader(r, agIndex, sb, bnoBlockStart)
	if err != nil {
		return err
	}
	keys, ptrs, err := collectBNOKeyListAndPtrList(r, agIndex, sb, bnoBlockStart, bnoHeader)
	if err != nil {
		return err
	}
	// 叶子节点, 存储空闲块信息.
	if len(keys) > 0 && len(ptrs) == 0 {
		*freeRecs = append(*freeRecs, keys...)
	}
	// 非叶子节点, 继续递归.
	if len(ptrs) > 0 {
		for _, ptr := range ptrs {
			err = collectFreeBlockListLogic(r, agIndex, sb, ptr.Ptr, freeRecs)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// btreePtrRelOffset 获取Ptr真实的起始偏移(不包含空闲区间), 且仅针对非叶子节点.
// 参考：
// https://github.com/sslab-gatech/hydra/blob/fd16457ba756ed602ab8fca378458838cea8e409/src/fs/xfs/xfsprogs-dev/db/btblock.c#L248的btblock_ptr_offset函数
// https://github.com/sslab-gatech/hydra/blob/fd16457ba756ed602ab8fca378458838cea8e409/src/fs/xfs/xfsprogs-dev/db/btblock.c#L177的btblock_maxrecs函数
func btreePtrRelOffset(blockSize int, bnoHeaderSize int) int {
	sizeOfAllocPtr := 4
	sizeOfAllocRec := 8
	allocKeyStartInBlock := blockSize - bnoHeaderSize
	maxNumrecs := allocKeyStartInBlock / (sizeOfAllocRec + sizeOfAllocPtr)
	return bnoHeaderSize + maxNumrecs*sizeOfAllocRec
}
