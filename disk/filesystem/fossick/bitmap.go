package fossick

import (
	"github.com/kisun-bit/drpkg/disk/filesystem/fossick/fs/ext"
	"github.com/kisun-bit/drpkg/disk/filesystem/fossick/fs/ntfs"
	"github.com/kisun-bit/drpkg/disk/filesystem/fossick/fs/xfs"
	"github.com/kisun-bit/drpkg/sys/ioctl"
	"github.com/kisun-bit/drpkg/util"
	"github.com/kisun-bit/drpkg/util/logger"
	"github.com/pkg/errors"
	"math"
	"runtime"
)

type Bitmap struct {
	FsPath      string
	FsType      Filesystem
	FsDevSize   int64 // 此fsSize可能并非实际的文件系统的大小.
	ClusterSize int
	Binary      []byte
}

// ForceExtractBitmap 强制导出位图.
// 需要注意此方法会在受支持的文件系统中，不管其是否支持有效数据提取, 均会导出位图.
func ForceExtractBitmap(fsPath string) (*Bitmap, error) {
	var extractFunc func(string) (int, []byte, error)
	fsType, err := GetFilesystemType(fsPath)
	if err != nil {
		return nil, err
	}
	// 注意：此fsSize可能并非实际的文件系统的大小
	// 举例说明：
	// 例如/dev/sda本身100GB，但是仅装有100M的EXT文件系统.
	fsDevSize, err := ioctl.QueryFileSize(fsPath)
	if err != nil {
		return nil, err
	}
	logger.Debugf("ForceExtractBitmap size of `%s` is `%v`", fsPath, fsDevSize)

	switch fsType {
	case EXT:
		extractFunc = ext.Extract
	case XFS:
		extractFunc = xfs.Extract
	case NTFS:
		extractFunc = ntfs.Extract
	default:
		// 不支持有效数据提取的文件系统也进行导出.
		extractFunc = func(string) (int, []byte, error) {
			return 0, nil, nil
		}
	}

	b := new(Bitmap)
	b.FsDevSize = int64(fsDevSize)
	b.FsPath = fsPath
	b.FsType = fsType
	b.ClusterSize, b.Binary, err = extractFunc(fsPath)
	if err != nil {
		// 日志警告并强制抹除其位图数据.
		b.Binary = make([]byte, 0)
		logger.Warnf("ForceExtractBitmap extractFunc fsDevPath=%v ERROR=%v", fsPath, err)
	}
	logger.Debugf("ForceExtractBitmap exported successfully bitmap for `%s`, bytes of bitmap is %v", fsPath, len(b.Binary))
	return b, nil
}

// Effective 位图是否有效.
func (b *Bitmap) Effective() bool {
	return len(b.Binary) != 0
}

type BitmapIterator struct {
	b                                        *Bitmap
	blkSize                                  int
	bitsPerBlk                               int
	maxBit, maxBlk, blkIdx, bitStart, bitEnd int64
	fsSignature                              string
}

type EffectBlockAddr struct {
	BlockOffset, BlockIndex, BitStart, BitEnd, FirstNonZeroBit int64
	BlockSize                                                  int
	Allocated, IsLastBlock                                     bool
}

func NewBitmapIteratorByBitmap(b *Bitmap, blockSize int) (bi *BitmapIterator, err error) {
	bi = new(BitmapIterator)
	bi.b = b
	bi.blkSize = blockSize
	bi.fix()
	if int64(bi.blkSize) > bi.b.FsDevSize {
		return nil, errors.Errorf("block-size(%v) can not be less than fs-size(%v)", bi.blkSize, bi.b.FsDevSize)
	}
	bi.bitsPerBlk, err = BitCountInNewBlockSize(bi.b.ClusterSize, bi.blkSize)
	if err != nil {
		return nil, err
	}
	bi.maxBit = int64(math.Ceil(float64(bi.b.FsDevSize) / float64(bi.b.ClusterSize)))
	if bi.b.Effective() {
		bi.maxBit = int64(len(bi.b.Binary)) * 8
	}
	bi.maxBlk = int64(math.Ceil(float64(bi.maxBit) / float64(bi.bitsPerBlk)))
	bi.fsSignature, err = CalculateHashFileSignature(bi.b.ClusterSize, bi.blkSize, 8, bi.maxBit, bi.maxBlk)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to calculate hash-signature")
	}
	bi.Reset()
	return bi, nil
}

func NewBitmapIterator(fsPath string, blockSize int) (*BitmapIterator, error) {
	b, err := ForceExtractBitmap(fsPath)
	if err != nil {
		return nil, err
	}
	return NewBitmapIteratorByBitmap(b, blockSize)
}

func (bi *BitmapIterator) fix() {
	switch runtime.GOOS {
	case "windows":
		// do nothing
	default:
		// Linux下NTFS存在部分位图问题，会造成卷实际已使用，但位图比特仍然为0的情况，
		// 保险起见, 只能增加其块大小的方式来尽量避免此类问题. TODO: 道理上来说最好直接走非文件系统解析的处理逻辑.
		if bi.b.FsType == NTFS {
			bi.blkSize = 2 << 20
		}
	}

	// 以文件系统簇大小为其块大小.
	if bi.blkSize == -1 {
		if bi.b.ClusterSize != 0 {
			// 有效.
			bi.blkSize = bi.b.ClusterSize
		} else {
			// 无效.
			bi.b.ClusterSize = 2 << 10
			bi.blkSize = bi.b.ClusterSize
		}
	} else {
		// 指定块大小且非有效位图.
		if bi.b.ClusterSize == 0 {
			bi.b.ClusterSize = bi.blkSize
		}
	}
}

func (bi *BitmapIterator) GetClusterSize() int {
	return bi.b.ClusterSize
}

func (bi *BitmapIterator) GetBlockSize() int {
	return bi.blkSize
}

func (bi *BitmapIterator) GetFsSize() int64 {
	return bi.b.FsDevSize
}

func (bi *BitmapIterator) GetFsHashSignature() string {
	return bi.fsSignature
}

// BitCountInNewBlockSize 以新的块大小去计算得到其跨了多少个比特位.
// 这里需要分情况看待：
// 情况一: 位图有效.
//
//	正常以b.ClusterSize进行计算即可.
//
// 情况二: 位图无效(即提取位图错误或未实现此文件系统的位图).
//
//	返回0.
//
// blkSize 表示新的块大小. 单位字节, 有效值(-1, 2M, 4M, 6M, 8M), 其中-1表示已簇大小为块大小, 请在调用前保证其值合法性.
func BitCountInNewBlockSize(clusterSize, blockSize int) (int, error) {
	if blockSize == -1 {
		return 1, nil
	}
	if blockSize%clusterSize != 0 {
		return 0, errors.Errorf("invalid block-size `%v` for cluster-size `%v`", blockSize, clusterSize)
	}
	bits := blockSize / clusterSize
	if bits == 0 {
		return 0, errors.Errorf("invalid block-size `%v` for cluster-size `%v`", blockSize, clusterSize)
	}
	return bits, nil
}

func (bi *BitmapIterator) Reset() {
	bi.blkIdx = -1
	bi.bitStart = -int64(bi.bitsPerBlk)
	bi.bitEnd = 0
}

func (bi *BitmapIterator) Next() bool {
	if bi.blkIdx >= bi.maxBlk-1 {
		return false
	}
	bi.blkIdx++
	bi.bitStart += int64(bi.bitsPerBlk)
	if bi.blkIdx+1 == bi.maxBlk {
		bi.bitEnd += bi.maxBit - bi.bitStart
	} else {
		bi.bitEnd += int64(bi.bitsPerBlk)
	}
	return true
}

func (bi *BitmapIterator) CurrentIndexes() (blkIdx, bitStart, bitEnd int64) {
	return bi.blkIdx, bi.bitStart, bi.bitEnd
}

// BlockAllocated 指定块号的数据区域是否已分配.
// 为何要将blkIdx, bitStart, bitEnd单独做成参数传入？其目的是为了让此方法能够并发.
func (bi *BitmapIterator) BlockAllocated(blkIdx, bitStart, bitEnd int64) (addr EffectBlockAddr, err error) {
	defer func() {
		if e := recover(); e != nil {
			err = errors.Errorf("panic when block index is %v, %v", bi.blkIdx, e)
		}
	}()
	if blkIdx > bi.maxBlk-1 {
		return addr, errors.New("overflow")
	}
	// 非有效位图的处理(总是已分配).
	if !bi.b.Effective() {
		return EffectBlockAddr{
			blkIdx * int64(bi.blkSize),
			blkIdx,
			bitStart,
			bitEnd,
			bitStart,
			bi.blkSize,
			true,
			blkIdx+1 == bi.maxBlk,
		}, nil
	}
	// 对比 bitStart 至 bitEnd 之间的所有比特位，是否存在非0位.
	firstNonZeroBit, allocated := util.ExistedNonZeroBit(bi.b.Binary, bitStart, bitEnd)
	//logger.Debugf("[%v] bitStart=%v bitEnd=%v firstNonZeroBit=%v",
	//	allocated, bitStart, bitEnd, firstNonZeroBit)
	return EffectBlockAddr{
		blkIdx * int64(bi.blkSize),
		blkIdx,
		bitStart,
		bitEnd,
		firstNonZeroBit,
		bi.blkSize,
		allocated,
		blkIdx+1 == bi.maxBlk,
	}, nil
}
