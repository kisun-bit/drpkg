package fossick

import (
	"context"
	"fmt"
	"github.com/cespare/xxhash/v2"
	"github.com/kisun-bit/drpkg/util/basic"
	"github.com/kisun-bit/drpkg/util/logger"
	"github.com/panjf2000/ants/v2"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
	"go.uber.org/zap"
	"io"
	"os"
	"sync"
	"sync/atomic"
)

const (
	MaxCores = 128
)

// EffectiveData 一组有效数据.
type EffectiveData struct {
	Offset             int64
	Length             int `struc:"sizeof=Bytes"`
	Bytes              []byte
	ClusterBitmapStart int64 // 文件系统位图的起始比特.
	ClusterBitmapEnd   int64 // 文件系统位图的结束比特.
	FirstNonZeroBit    int64 // ClusterBitmapStart 至 ClusterBitmapEnd 区间中, 首个非0比特的位置.
}

func (ed *EffectiveData) Repr() string {
	return fmt.Sprintf("EffectiveData(Offset=%v,Length=%v,LenBytes=%v,ClusterBitmapStart=%v,ClusterBitmapEnd=%v,md5=%s)",
		ed.Offset, ed.Length, len(ed.Bytes), ed.ClusterBitmapStart, ed.ClusterBitmapEnd, basic.Md5(ed.Bytes[:ed.Length]))
}

type RIdxes struct {
	BlkIdx, BitStart, BitEnd int64
}

// EffectiveDataReader 实现以文件系统为基础, 以挖掘其中有效数据的读取器.
type EffectiveDataReader struct {
	ctx                  context.Context
	mutex                sync.RWMutex
	traceID              string             // 绑定于任务的唯一标识.
	device               string             // 设备(或快照设备).
	deviceStream         readAtCloser       // 文件流
	deviceReader         *os.File           // 文件流.
	readerAt             io.ReaderAt        // 真正工作的读工作起
	deviceRPool          *ants.PoolWithFunc // 文件读池.
	rwg                  sync.WaitGroup     // 读同步计数组.
	referHashIsNil       bool               // 参考哈希是否为空.
	referHash            io.ReaderAt        // 参考哈希.
	currentHashIsNil     bool               // 本次哈希是否为空.
	currentHash          io.WriterAt        // 本次哈希.
	hashSize             int                // 单个哈希数据长度.
	blockSize            int                // 块大小（单位：字节，合法值：-1, 2M、4M、6M、8M）.
	readCores            int                // 读并发数.
	BitmapIter           *BitmapIterator    // 位图.
	logger               *zap.SugaredLogger // 日志.
	dataChan             chan EffectiveData // 产出的若干组有效数据块.
	effectBlockCount     atomic.Int64       // 有效数据块数量.
	incrEffectBlockCount atomic.Int64       // 有效增量数据块数量.
	closeOnce            sync.Once          // 仅关闭一次.
	err                  error              // 全局错误.
}

// NewEffectiveDataReader 初始化一个有效数据读取器.
func NewEffectiveDataReader(
	ctx context.Context,
	traceID string,
	logger *zap.SugaredLogger,
	device string,
	referHash io.ReaderAt,
	currentHash io.WriterAt,
	blockSize,
	readCores int) (*EffectiveDataReader, error) {
	if readCores > MaxCores || readCores <= 0 {
		readCores = MaxCores
	}
	if logger == nil {
		return nil, errors.New("lack logger")
	}
	if !funk.InInts([]int{-1, 2 << 20, 4 << 20, 6 << 20, 8 << 20}, blockSize) {
		return nil, errors.Errorf("invalid block-size %v", blockSize)
	}
	b, err := NewBitmapIterator(device, blockSize)
	if err != nil {
		return nil, err
	}
	dReader, err := os.Open(device)
	if err != nil {
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	edr := &EffectiveDataReader{
		ctx:              ctx,
		traceID:          traceID,
		logger:           logger,
		device:           device,
		deviceReader:     dReader,
		deviceStream:     dReader,
		readerAt:         dReader,
		referHashIsNil:   basic.IsNil(referHash),
		referHash:        referHash,
		currentHashIsNil: basic.IsNil(currentHash),
		currentHash:      currentHash,
		blockSize:        blockSize,
		readCores:        readCores,
		BitmapIter:       b,
		hashSize:         8, // 这里固定为8字节, 且采用xxhash64算法.
		dataChan:         make(chan EffectiveData, readCores),
	}
	edr.deviceRPool, err = ants.NewPoolWithFunc(readCores, edr.read)
	if err != nil {
		return nil, err
	}
	go edr.start()
	return edr, nil
}

// NewEffectiveDataReaderWithStreamAndBitmapIter 初始化一个有效数据读取器.
func NewEffectiveDataReaderWithStreamAndBitmapIter(
	ctx context.Context,
	traceID string,
	logger *zap.SugaredLogger,
	stream readAtCloser,
	iterator *BitmapIterator,
	referHash io.ReaderAt,
	currentHash io.WriterAt,
	blockSize,
	readCores int) (*EffectiveDataReader, error) {
	if readCores > MaxCores || readCores <= 0 {
		readCores = MaxCores
	}
	if logger == nil {
		return nil, errors.New("lack logger")
	}
	if !funk.InInts([]int{-1, 2 << 20, 4 << 20, 6 << 20, 8 << 20}, blockSize) {
		return nil, errors.Errorf("invalid block-size %v", blockSize)
	}

	var err error

	edr := &EffectiveDataReader{
		ctx:              ctx,
		traceID:          traceID,
		logger:           logger,
		deviceStream:     stream,
		readerAt:         stream,
		referHashIsNil:   basic.IsNil(referHash),
		referHash:        referHash,
		currentHashIsNil: basic.IsNil(currentHash),
		currentHash:      currentHash,
		blockSize:        blockSize,
		readCores:        readCores,
		BitmapIter:       iterator,
		hashSize:         8, // 这里固定为8字节, 且采用xxhash64算法.
		dataChan:         make(chan EffectiveData, readCores),
	}
	edr.deviceRPool, err = ants.NewPoolWithFunc(readCores, edr.read)
	if err != nil {
		return nil, err
	}
	go edr.start()
	return edr, nil
}

func (edr *EffectiveDataReader) Repr() string {
	return fmt.Sprintf("<EffectiveDataReader-(%s)>", edr.traceID)
}

func (edr *EffectiveDataReader) read(i interface{}) {
	defer edr.rwg.Done()

	idxes := i.(RIdxes)
	eb, err := edr.BitmapIter.BlockAllocated(idxes.BlkIdx, idxes.BitStart, idxes.BitEnd)
	if err != nil {
		edr.logger.Errorf("%s.read BlockAllocated ERROR=%v", edr.Repr(), err)
		edr.setError(err)
		return
	}
	if !eb.Allocated {
		return
	}
	edr.effectBlockCount.Add(1)

	buf := make([]byte, eb.BlockSize)
	// 注意：由于块是字节为单位的，但是位图是以比特为单位的，所以难免出现位图最后一个字节，并非全部是位图数据.
	// 那么就要求调用者一定要对err进行io.EOF判断.
	// 注意：如果是elastio/datto快照，那么读取其末尾有效数据块时，读取到的n可能是0，TODO 这些为0的有效数据要不要判定任务失败？
	n, err := edr.readerAt.ReadAt(buf, eb.BlockOffset)
	if err != nil && err != io.EOF {
		edr.logger.Errorf("%s.read ReadAt(offset:%v) ERROR=%v", edr.Repr(), eb.BlockOffset, err)
		edr.setError(err)
		return
	}
	if n == 0 || err == io.EOF {
		edr.logger.Debugf("%s.read n is 0, offset=%v, blocks=%v", edr.Repr(), eb.BlockOffset, eb.BlockIndex)
		return
	}
	// 只要referHash和currentHash存在一个不为nil时，就必定要计算哈希.
	if !(edr.referHashIsNil && edr.currentHashIsNil) {
		hashOff, err := CalculateHashOffset(eb.BlockOffset, eb.BlockSize, edr.hashSize)
		if err != nil {
			edr.logger.Errorf("%s.read CalculateHashOffset ERROR=%v", edr.Repr(), err)
			edr.setError(err)
			return
		}
		curHash := xxhash.Sum64(buf[:n])
		incrFlg := false
		if edr.referHashIsNil {
			incrFlg = true
		} else {
			oldHash, err := ReadHash(edr.referHash, hashOff, edr.hashSize)
			if err != nil {
				edr.logger.Errorf("%s.read ReadHash ERROR=%v", edr.Repr(), err)
				edr.setError(err)
				return
			}
			if oldHash != curHash {
				incrFlg = true
			}
		}
		if !edr.currentHashIsNil {
			err = WriteHash(edr.currentHash, hashOff, curHash, edr.hashSize)
			if err != nil {
				edr.logger.Errorf("%s.read WriteHash ERROR=%v", edr.Repr(), err)
				edr.setError(err)
				return
			}
		}
		if !incrFlg {
			return
		}
	}
	edr.incrEffectBlockCount.Add(1)
	ed := EffectiveData{
		Offset:             eb.BlockOffset,
		Length:             n,
		Bytes:              buf[:n],
		ClusterBitmapStart: eb.BitStart,
		ClusterBitmapEnd:   eb.BitEnd,
		FirstNonZeroBit:    eb.FirstNonZeroBit,
	}
	edr.dataChan <- ed
}

// DataChannel 若干组有效数据.
func (edr *EffectiveDataReader) DataChannel() chan EffectiveData {
	return edr.dataChan
}

func (edr *EffectiveDataReader) start() {
	defer edr.close()
	defer edr.check()

	edr.logger.Debugf("%s .............Readcores=%v", edr.Repr(), edr.readCores)
	edr.logger.Debugf("%s ........FilesystemSize=%v", edr.Repr(), edr.BitmapIter.b.FsDevSize)
	edr.logger.Debugf("%s .......BitmapEffective=%v", edr.Repr(), edr.BitmapIter.b.Effective())
	edr.logger.Debugf("%s ......BitmapBinarySize=%v", edr.Repr(), len(edr.BitmapIter.b.Binary))
	edr.logger.Debugf("%s .BitmapBinarySizeInBit=%v", edr.Repr(), len(edr.BitmapIter.b.Binary)*8)
	edr.logger.Debugf("%s ...............MaxBits=%v", edr.Repr(), edr.BitmapIter.maxBit)
	edr.logger.Debugf("%s .............MaxBlocks=%v", edr.Repr(), edr.BitmapIter.maxBlk)
	edr.logger.Debugf("%s .............BlockSize=%v", edr.Repr(), edr.BitmapIter.blkSize)
	edr.logger.Debugf("%s ..........BitsPerBlock=%v", edr.Repr(), edr.BitmapIter.bitsPerBlk)
	if edr.BitmapIter.b.Effective() {
		//edr.logger.Debugf("%s ......BitmapHex[0-512]=\n%v", edr.Repr(), hex.Dump(edr.BitmapIter.b.Binary[:512]))
	}

	for edr.BitmapIter.Next() {
		if edr.cancelled() || edr.errored() {
			return
		}
		idxes := RIdxes{}
		idxes.BlkIdx, idxes.BitStart, idxes.BitEnd = edr.BitmapIter.CurrentIndexes()
		edr.rwg.Add(1)
		if err := edr.deviceRPool.Invoke(idxes); err != nil {
			edr.rwg.Done()
			edr.logger.Errorf("%s.start Invoke ERROR=%v", edr.Repr(), err)
			edr.setError(err)
			return
		}
	}
}

func (edr *EffectiveDataReader) check() {
	// TODO 检查是否合法.
	return
}

func (edr *EffectiveDataReader) close() {
	edr.closeOnce.Do(func() {
		logger.Debugf("%s.close enter...", edr.Repr())
		edr.rwg.Wait()
		if edr.device != "" && edr.deviceReader != nil {
			err := edr.deviceReader.Close()
			logger.Debugf("%s.close closed `%v`: %v", edr.Repr(), edr.device, err)
		}
		close(edr.dataChan)
	})
}

func (edr *EffectiveDataReader) Filesystem() Filesystem {
	return edr.BitmapIter.b.FsType
}

func (edr *EffectiveDataReader) Release() {
	edr.logger.Debugf("%s.Release enter...", edr.Repr())
	// 清空,保证被GC回收.
	count := 0
	for range edr.dataChan {
		count++
	}
	if count != 0 {
		edr.logger.Debugf("%s.Release recycled %v item from data-chan", edr.Repr(), count)
	}
	edr.close()
}

func (edr *EffectiveDataReader) Error() error {
	edr.mutex.RLock()
	defer edr.mutex.RUnlock()
	return edr.err
}

func (edr *EffectiveDataReader) EffectBlockCount() int64 {
	return edr.effectBlockCount.Load()
}

func (edr *EffectiveDataReader) IncrEffectBlockCount() int64 {
	return edr.incrEffectBlockCount.Load()
}

func (edr *EffectiveDataReader) setError(err error) {
	edr.mutex.Lock()
	defer edr.mutex.Unlock()
	if edr.err != nil {
		return
	}
	edr.err = err
}

func (edr *EffectiveDataReader) cancelled() bool {
	return basic.Cancelled(edr.ctx)
}

func (edr *EffectiveDataReader) errored() bool {
	edr.mutex.RLock()
	defer edr.mutex.RUnlock()
	return edr.err != nil
}
