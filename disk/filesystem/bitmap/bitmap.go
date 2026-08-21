// Package bitmap 提供文件系统"已使用块"位图。
//
// 位图内部使用 RoaringBitmap（github.com/RoaringBitmap/roaring/v2）存储置位的块号，
// 内存占用随"已使用块的分布"而不是"磁盘总容量"增长：
//
//   - 稀疏位图（只有少量已使用块）：只存储已置位的区间，内存占用极小；
//   - 密集位图（大部分块已使用）：连续区间以 Run-Length 方式压缩存储；
//   - 对比传统 []byte 位图（恒定占用 Bits/8 字节），例如 16 TiB 磁盘按 4 KiB
//     粒度需要 2^32 个 bit，传统方式固定占用 512 MiB，而 RoaringBitmap 通常
//     只需要几 MiB ~ 几十 MiB。
//
// roaring.Bitmap 只能存储 32 位值，因此 FsBitmap 按位索引的高 32 位分段，
// 每段一个 roaring.Bitmap（覆盖 2^32 个 bit），可以表示任意大小的位图。
package bitmap

import (
	"fmt"
	"io"
	"math/bits"
	"sort"

	roaring "github.com/RoaringBitmap/roaring/v2"
	"github.com/dustin/go-humanize"
	"github.com/pkg/errors"
)

// segmentShift 是位索引分段使用的位数：低 32 位为段内偏移，高 32 位为段号。
const segmentShift = 32

// BitmapKind 表示位图的数据来源类型
type BitmapKind int

const (
	// BitmapRaw 表示未经过文件系统解析的原始位图
	BitmapRaw BitmapKind = iota

	// BitmapFromFS 表示经过文件系统解析得到的位图
	BitmapFromFS
)

type FsBitmap struct {
	// Type 文件系统类型
	Type string

	// BitmapKind: 位图来源类型
	BitmapKind BitmapKind

	// Bits 位图中的位个数
	Bits int64

	// BlockSize 数据块大小
	BlockSize int

	// segments 存储已置位的块号（已使用块），key 为位索引的高 32 位。
	// 全 0 的段不占用内存（不存在对应的 key）。
	segments map[uint32]*roaring.Bitmap
}

// FsBitmapParser 表示文件系统位图解析接口
type FsBitmapParser interface {
	fmt.Stringer

	// Dump 导出位图数据
	Dump() (bitmap *FsBitmap, err error)
}

// NewFsBitmap 创建一个新的文件系统位图
// bits: 位图总位数（通常等于文件系统的总块数，如 sb_dblocks）
// blockSize: 每个 bit 对应的数据块大小（字节）
func NewFsBitmap(fsType string, kind BitmapKind, bits int64, blockSize int) *FsBitmap {
	return &FsBitmap{
		Type:       fsType,
		BitmapKind: kind,
		Bits:       bits,
		BlockSize:  blockSize,
		segments:   make(map[uint32]*roaring.Bitmap),
	}
}

// NewFsBitmapFromBytes 从原始位图字节创建位图。
// 原始字节按字节存储、每字节 8 个 bit，字节内低位在前（bit i 对应 raw[i/8] 的第 i%8 位），
// 与 NTFS $Bitmap 等 on-disk 格式的布局一致。
// 超出 numBits 的尾部 padding bit 会被忽略。
func NewFsBitmapFromBytes(fsType string, kind BitmapKind, raw []byte, numBits int64, blockSize int) *FsBitmap {
	b := NewFsBitmap(fsType, kind, numBits, blockSize)
	if numBits <= 0 || len(raw) == 0 {
		return b
	}

	// 按升序遍历所有置位的 bit，并把连续的 bit 合并成区间后批量置位，
	// 让 RoaringBitmap 尽可能以 Run-Length 方式压缩。
	runStart := int64(-1) // 当前连续置位区间的起点，-1 表示没有
	lastSet := int64(-1)  // 上一个置位的 bit 索引
	for byteIdx, by := range raw {
		v := by
		base := int64(byteIdx) * 8
		for v != 0 {
			j := bits.TrailingZeros8(v)
			v &^= 1 << uint(j)

			bit := base + int64(j)
			if bit >= numBits {
				continue // 忽略超出 numBits 的 padding bit
			}

			if runStart >= 0 && bit == lastSet+1 {
				lastSet = bit // 并入当前区间
				continue
			}
			if runStart >= 0 {
				b.addRange(uint64(runStart), uint64(lastSet)+1)
			}
			runStart, lastSet = bit, bit
		}
	}
	if runStart >= 0 {
		b.addRange(uint64(runStart), uint64(lastSet)+1)
	}
	return b
}

func (b *FsBitmap) Size() int64 {
	return b.Bits * int64(b.BlockSize)
}

// Set 把指定块号对应的 bit 置为 1
func (b *FsBitmap) Set(blockNum uint64) {
	if b.Bits <= 0 || blockNum >= uint64(b.Bits) {
		return // 越界直接忽略，避免panic；如需严格模式可以改成返回error
	}
	b.segment(segmentIndex(blockNum), true).Add(uint32(blockNum))
}

// SetRange 把 [start, start+length) 范围内的块都置为 1，超出 Bits 的部分被截断
func (b *FsBitmap) SetRange(start uint64, length uint32) {
	if length == 0 || b.Bits <= 0 || start >= uint64(b.Bits) {
		return
	}
	end := start + uint64(length)
	if end > uint64(b.Bits) {
		end = uint64(b.Bits)
	}
	b.addRange(start, end)
}

// IsSet 查询指定块号是否被置位（可选，便于测试和调试）
func (b *FsBitmap) IsSet(blockNum uint64) bool {
	if b.Bits <= 0 || blockNum >= uint64(b.Bits) {
		return false
	}
	seg, ok := b.segments[segmentIndex(blockNum)]
	return ok && seg.Contains(uint32(blockNum))
}

// SetAll 把位图所有有效 bit 全部置 1（初始化为"全部已使用"状态）
func (b *FsBitmap) SetAll() {
	if b.Bits <= 0 {
		return
	}
	b.addRange(0, uint64(b.Bits))
}

// Clear 把指定块号对应的 bit 清 0
func (b *FsBitmap) Clear(blockNum uint64) {
	if b.Bits <= 0 || blockNum >= uint64(b.Bits) {
		return
	}
	idx := segmentIndex(blockNum)
	seg, ok := b.segments[idx]
	if !ok {
		return
	}
	seg.Remove(uint32(blockNum))
	if seg.IsEmpty() {
		// 段被清空后直接删除，保持稀疏位图的内存占用最小
		delete(b.segments, idx)
	}
}

// ClearRange 把 [start, start+length) 范围内的块都清 0，超出 Bits 的部分被截断
func (b *FsBitmap) ClearRange(start uint64, length uint32) {
	if length == 0 || b.Bits <= 0 || start >= uint64(b.Bits) {
		return
	}
	end := start + uint64(length)
	if end > uint64(b.Bits) {
		end = uint64(b.Bits)
	}
	b.removeRange(start, end)
}

// CountSet 统计位图中值为 1 的 bit 数量（即已使用的 block 数）。
// 所有置位操作都被限制在 [0, Bits) 内，因此不存在需要排除的 padding bit。
func (b *FsBitmap) CountSet() int64 {
	var count int64
	for _, seg := range b.segments {
		count += int64(seg.GetCardinality())
	}
	return count
}

// UsedSize 返回位图中值为 1 的 bit（即已使用的 block）所代表的数据总大小，单位字节。
func (b *FsBitmap) UsedSize() int64 {
	return b.CountSet() * int64(b.BlockSize)
}

func (b *FsBitmap) UsedSizeHuman() string {
	return humanize.IBytes(uint64(b.UsedSize()))
}

// MemorySize 返回位图当前占用的内存字节数（RoaringBitmap 压缩后的大小）。
// 传统 []byte 位图恒定为 Bits/8，该值可用于观察压缩效果。
func (b *FsBitmap) MemorySize() uint64 {
	var size uint64
	for _, seg := range b.segments {
		size += seg.GetSizeInBytes()
	}
	return size
}

// ChangeBlockSize 重新以新的块大小生成位图
// 注意：新的块大小必须是旧的块大小的整数倍
// 合并规则：新 bit 覆盖的多个旧 bit 中，只要有任意一个为 1（已使用），新 bit 就置 1，
// 避免因为块粒度变粗而丢失已使用数据（宁可多复制，不能少复制）
func (b *FsBitmap) ChangeBlockSize(blocksize int) error {
	if blocksize <= 0 {
		return errors.Errorf("invalid blocksize: %d", blocksize)
	}
	if b.BlockSize <= 0 {
		return errors.Errorf("invalid current blocksize: %d", b.BlockSize)
	}
	if blocksize == b.BlockSize {
		return nil // 无需转换
	}
	if blocksize < b.BlockSize {
		return errors.Errorf("new blocksize(%d) must not be smaller than current blocksize(%d)", blocksize, b.BlockSize)
	}
	if blocksize%b.BlockSize != 0 {
		return errors.Errorf("new blocksize(%d) must be a multiple of current blocksize(%d)", blocksize, b.BlockSize)
	}
	if b.Bits <= 0 {
		b.BlockSize = blocksize
		return nil
	}

	ratio := uint64(blocksize / b.BlockSize)
	newBits := (b.Bits + int64(ratio) - 1) / int64(ratio)

	// 只有置位的旧 bit 会影响结果：新 bit = 旧 bit / ratio。
	// 按段号升序遍历可以保证新 bit 按升序插入，有利于 RoaringBitmap 的区间压缩。
	keys := make([]uint32, 0, len(b.segments))
	for idx := range b.segments {
		keys = append(keys, idx)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	segments := make(map[uint32]*roaring.Bitmap, len(b.segments))
	for _, idx := range keys {
		base := uint64(idx) << segmentShift
		it := b.segments[idx].Iterator()
		for it.HasNext() {
			newBit := (base | uint64(it.Next())) / ratio
			newIdx := segmentIndex(newBit)
			seg, ok := segments[newIdx]
			if !ok {
				seg = roaring.New()
				segments[newIdx] = seg
			}
			seg.Add(uint32(newBit))
		}
	}

	b.segments = segments
	b.Bits = newBits
	b.BlockSize = blocksize
	return nil
}

// MirrorFs 根据位图，把 origin 中被标记为"已使用"的块（bit=1）复制到 target 对应偏移处，
// 跳过标记为"空闲"的块（bit=0），从而只搬运实际有效数据，节省 IO。
// 返回值为实际复制的字节数。
func (b *FsBitmap) MirrorFs(origin io.ReaderAt, target io.WriterAt) (int64, error) {
	if origin == nil || target == nil {
		return 0, errors.New("origin/target must not be nil")
	}

	const maxChunkBlocks = 1024 // 单次 IO 最多合并搬运的块数，避免大段连续区间一次性分配过大内存

	chunkBufSize := maxChunkBlocks * int64(b.BlockSize)
	buf := make([]byte, chunkBufSize)

	var totalCopied int64
	pos := int64(0)

	for pos < b.Bits {
		// 跳过连续的空闲块（bit=0），不做任何 IO
		pos = b.nextSetBit(pos)
		if pos >= b.Bits {
			break
		}

		// 找到从 pos 开始，连续被置位（已使用）的块数
		runStart := pos
		runEnd := b.nextClearBit(pos) // [runStart, runEnd) 都是已使用块
		runLen := runEnd - runStart

		offset := runStart * int64(b.BlockSize)
		remaining := runLen

		// 按 maxChunkBlocks 分批读写，避免超大连续区间一次性占用过多内存
		for remaining > 0 {
			chunkBlocks := remaining
			if chunkBlocks > maxChunkBlocks {
				chunkBlocks = maxChunkBlocks
			}
			chunkSize := chunkBlocks * int64(b.BlockSize)

			n, err := origin.ReadAt(buf[:chunkSize], offset)
			if err != nil && err != io.EOF {
				return totalCopied, errors.Wrapf(err, "read origin at offset %d", offset)
			}
			actual := int64(n)
			if actual <= 0 {
				break // 读到 EOF 且没有数据了，提前结束（比如设备实际大小小于位图声明的范围）
			}

			if _, err := target.WriteAt(buf[:actual], offset); err != nil {
				return totalCopied, errors.Wrapf(err, "write target at offset %d", offset)
			}

			totalCopied += actual
			offset += actual
			remaining -= chunkBlocks
		}

		pos = runEnd
	}

	return totalCopied, nil
}

// segmentIndex 返回位索引对应的段索引（高 32 位）
func segmentIndex(bit uint64) uint32 {
	return uint32(bit >> segmentShift)
}

// segment 返回指定段的 RoaringBitmap；create 为 true 时不存在则创建。
func (b *FsBitmap) segment(idx uint32, create bool) *roaring.Bitmap {
	seg, ok := b.segments[idx]
	if !ok && create {
		seg = roaring.New()
		b.segments[idx] = seg
	}
	return seg
}

// addRange 把 [start, end) 置位。调用方需保证 0 <= start < end <= Bits。
func (b *FsBitmap) addRange(start, end uint64) {
	for start < end {
		idx := segmentIndex(start)
		segStart := uint64(idx) << segmentShift
		segEnd := segStart + uint64(1)<<segmentShift
		hi := end
		if hi > segEnd {
			hi = segEnd
		}
		b.segment(idx, true).AddRange(start-segStart, hi-segStart)
		start = hi
	}
}

// removeRange 把 [start, end) 清零。调用方需保证 0 <= start < end <= Bits。
func (b *FsBitmap) removeRange(start, end uint64) {
	for start < end {
		idx := segmentIndex(start)
		segStart := uint64(idx) << segmentShift
		segEnd := segStart + uint64(1)<<segmentShift
		hi := end
		if hi > segEnd {
			hi = segEnd
		}
		if seg, ok := b.segments[idx]; ok {
			seg.RemoveRange(start-segStart, hi-segStart)
			if seg.IsEmpty() {
				delete(b.segments, idx)
			}
		}
		start = hi
	}
}

// nextSetBit 从 from（含）开始，找到下一个被置位（1）的 bit 索引；
// 若一直到 b.Bits 都没有，返回 b.Bits
func (b *FsBitmap) nextSetBit(from int64) int64 {
	if from < 0 {
		from = 0
	}
	if b.Bits <= 0 || from >= b.Bits {
		return b.Bits
	}

	total := uint64(b.Bits)
	lastIdx := segmentIndex(total - 1)
	lo := uint32(from) // 仅在第一个段内有效（from 位于该段内），之后的段从 0 开始查
	for idx := segmentIndex(uint64(from)); idx <= lastIdx; idx++ {
		if seg, ok := b.segments[idx]; ok {
			if v := seg.NextValue(lo); v >= 0 {
				// 置位操作都被限制在 [0, Bits) 内，结果无需再截断
				return int64((uint64(idx) << segmentShift) | uint64(uint32(v)))
			}
		}
		lo = 0
	}
	return b.Bits
}

// nextClearBit 从 from（含）开始，找到下一个未被置位（0）的 bit 索引；
// 若一直到 b.Bits 都没有，返回 b.Bits（表示 [from, b.Bits) 全部是已使用块）
func (b *FsBitmap) nextClearBit(from int64) int64 {
	if from < 0 {
		from = 0
	}
	if b.Bits <= 0 || from >= b.Bits {
		return b.Bits
	}

	total := uint64(b.Bits)
	lastIdx := segmentIndex(total - 1)
	lo := uint32(from) // 仅在第一个段内有效，之后的段从 0 开始查
	for idx := segmentIndex(uint64(from)); idx <= lastIdx; idx++ {
		segStart := uint64(idx) << segmentShift

		seg, ok := b.segments[idx]
		if !ok {
			// 段不存在，段内所有 bit 都是 0
			res := segStart
			if uint64(from) > res {
				res = uint64(from)
			}
			return int64(res)
		}

		v := seg.NextAbsentValue(lo)
		if v >= int64(lo) && v < int64(1)<<segmentShift {
			// 正常路径：v 是段内第一个空闲位
			res := (uint64(idx) << segmentShift) | uint64(uint32(v))
			if res >= total {
				return b.Bits
			}
			return int64(res)
		}
		if v >= 0 && v < int64(lo) {
			// roaring v2.10.0 bug：arrayContainer.nextAbsentValue 在最后一个
			// 元素为 0xFFFF 时 uint16 溢出，再被 combineLoHi32 的高位移位
			// 叠加后返回了 < lo 的错误值。回退到迭代器逐值扫描找第一个空洞。
			expected := uint64(lo)
			it := seg.Iterator()
			it.AdvanceIfNeeded(lo)
			for it.HasNext() && uint64(it.PeekNext()) == expected {
				expected++
				it.Next()
			}
			if expected < uint64(1)<<segmentShift {
				res := (uint64(idx) << segmentShift) | expected
				if res >= total {
					return b.Bits
				}
				return int64(res)
			}
			// expected 溢出到下一个段，继续找下一段
		}
		// v < 0 或 v >= 2^32：本段 [lo, 2^32) 全部置位，继续找下一段
		lo = 0
	}
	return b.Bits
}
