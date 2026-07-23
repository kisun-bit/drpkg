package bitmap

import (
	"fmt"
	"io"
	"math/bits"

	"github.com/dustin/go-humanize"
	"github.com/pkg/errors"
)

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

	// Bitmap 位图数据
	Bitmap []byte

	// Bits 位图中的位个数
	Bits int64

	// BlockSize 数据块大小
	BlockSize int
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
	// 位图按字节存储，每字节 8 位，向上取整
	byteLen := (bits + 7) / 8
	return &FsBitmap{
		Type:       fsType,
		BitmapKind: kind,
		Bitmap:     make([]byte, byteLen),
		Bits:       bits,
		BlockSize:  blockSize,
	}
}

func (b *FsBitmap) Size() int64 {
	return b.Bits * int64(b.BlockSize)
}

// Set 把指定块号对应的 bit 置为 1
func (b *FsBitmap) Set(blockNum uint64) {
	if int64(blockNum) < 0 || int64(blockNum) >= b.Bits {
		return // 越界直接忽略，避免panic；如需严格模式可以改成返回error
	}
	byteIdx := blockNum / 8
	bitOff := blockNum % 8
	b.Bitmap[byteIdx] |= 1 << bitOff
}

// SetRange 把 [start, start+length) 范围内的块都置为 1
func (b *FsBitmap) SetRange(start uint64, length uint32) {
	for i := uint32(0); i < length; i++ {
		b.Set(start + uint64(i))
	}
}

// IsSet 查询指定块号是否被置位（可选，便于测试和调试）
func (b *FsBitmap) IsSet(blockNum uint64) bool {
	if int64(blockNum) < 0 || int64(blockNum) >= b.Bits {
		return false
	}
	byteIdx := blockNum / 8
	bitOff := blockNum % 8
	return b.Bitmap[byteIdx]&(1<<bitOff) != 0
}

// SetAll 把位图所有有效 bit 全部置 1（初始化为"全部已使用"状态）
func (b *FsBitmap) SetAll() {
	for i := range b.Bitmap {
		b.Bitmap[i] = 0xFF
	}
	// 注意：如果 Bits 不是 8 的整数倍，最后一字节里超出 Bits 范围的多余 bit
	// 也会被置 1，但只要后续查询/统计都严格按 Bits 数量截止，不会被误读，无需特殊处理。
}

// Clear 把指定块号对应的 bit 清 0
func (b *FsBitmap) Clear(blockNum uint64) {
	if int64(blockNum) < 0 || int64(blockNum) >= b.Bits {
		return
	}
	byteIdx := blockNum / 8
	bitOff := blockNum % 8
	b.Bitmap[byteIdx] &^= 1 << bitOff // AND NOT，清除该位
}

// ClearRange 把 [start, start+length) 范围内的块都清 0
func (b *FsBitmap) ClearRange(start uint64, length uint32) {
	for i := uint32(0); i < length; i++ {
		b.Clear(start + uint64(i))
	}
}

// CountSet 统计位图中值为 1 的有效 bit 数量（即已使用的 block 数）。
// 只统计 [0, Bits) 范围内的位，最后一个字节里超出 Bits 的 padding bit 会被自动排除。
func (b *FsBitmap) CountSet() int64 {
	if b.Bits <= 0 {
		return 0
	}

	var count int64
	fullBytes := int(b.Bits / 8) // 完整的字节数（不含最后一个不完整字节）

	for i := 0; i < fullBytes && i < len(b.Bitmap); i++ {
		count += int64(bits.OnesCount8(b.Bitmap[i]))
	}

	// 处理最后一个不完整字节：只有低 rem 位是有效数据，高位是 padding，需要屏蔽
	if rem := b.Bits % 8; rem > 0 && fullBytes < len(b.Bitmap) {
		mask := byte(1<<uint(rem) - 1) // 低 rem 位为 1，其余位为 0
		count += int64(bits.OnesCount8(b.Bitmap[fullBytes] & mask))
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

	ratio := int64(blocksize / b.BlockSize)

	// 新位图的总位数：向上取整，确保能覆盖原来所有数据（哪怕最后一组不足 ratio 个旧 bit）
	newBits := (b.Bits + ratio - 1) / ratio
	newByteLen := (newBits + 7) / 8
	newBitmap := make([]byte, newByteLen)

	for newIdx := int64(0); newIdx < newBits; newIdx++ {
		oldStart := newIdx * ratio
		oldEnd := oldStart + ratio
		if oldEnd > b.Bits {
			oldEnd = b.Bits
		}

		used := false
		for oldIdx := oldStart; oldIdx < oldEnd; oldIdx++ {
			if b.IsSet(uint64(oldIdx)) {
				used = true
				break
			}
		}

		if used {
			byteIdx := newIdx / 8
			bitOff := uint(newIdx % 8)
			newBitmap[byteIdx] |= 1 << bitOff
		}
	}

	b.Bitmap = newBitmap
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

// nextSetBit 从 from（含）开始，找到下一个被置位（1）的 bit 索引；
// 若一直到 b.Bits 都没有，返回 b.Bits
func (b *FsBitmap) nextSetBit(from int64) int64 {
	if from < 0 {
		from = 0
	}
	i := from
	for i < b.Bits {
		byteIdx := i / 8
		byteVal := b.Bitmap[byteIdx]

		// 整字节都是 0（全空闲），直接跳过这 8 位，加速大段空闲区间的扫描
		if byteVal == 0x00 {
			i += 8 - (i % 8) // 跳到下一个字节边界
			continue
		}

		bitOff := uint(i % 8)
		if byteVal&(1<<bitOff) != 0 {
			return i
		}
		i++
	}
	return b.Bits
}

// nextClearBit 从 from（含）开始，找到下一个未被置位（0）的 bit 索引；
// 若一直到 b.Bits 都没有，返回 b.Bits（表示 [from, b.Bits) 全部是已使用块）
func (b *FsBitmap) nextClearBit(from int64) int64 {
	if from < 0 {
		from = 0
	}
	i := from
	for i < b.Bits {
		byteIdx := i / 8
		byteVal := b.Bitmap[byteIdx]

		// 整字节都是 0xFF（全部已使用），直接跳过这 8 位
		if byteVal == 0xFF {
			i += 8 - (i % 8)
			continue
		}

		bitOff := uint(i % 8)
		if byteVal&(1<<bitOff) == 0 {
			return i
		}
		i++
	}
	return b.Bits
}
