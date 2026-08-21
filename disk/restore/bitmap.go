package restore

import (
	roaring "github.com/RoaringBitmap/roaring/v2"
)

// bitmap 记录目标上"已恢复"的区域，一个 bit 对应一个粒度单位。
// 使用 RoaringBitmap v2 实现，对于稀疏和密集数据都能高效压缩：
//
// 内存占用特点：
//   - 稀疏阶段（大部分未恢复）：仅存储已恢复的区间，内存占用极小
//   - 密集阶段（大部分已恢复）：使用 Run-Length Encoding 压缩连续区间
//   - 相比固定位图（容量/4096），RoaringBitmap 在恢复过程中动态调整内存
//
// 典型场景对比（1 TiB 磁盘，512B 粒度）：
//   - 固定位图：始终占用 256 MiB
//   - RoaringBitmap：
//   - 恢复 1% 时：约 2.5 MiB（仅存储已恢复的 10 GiB）
//   - 恢复 50% 时：约 128 MiB（稀疏 + 连续区间压缩）
//   - 恢复 99% 时：约 256 MiB（接近固定位图，但仍有压缩）
//
// 性能特点：
//   - 区间操作（AddRange）：O(区间数)
//   - 查询操作（Contains/NextValue）：O(log N)，N 为容器数
//   - 位图遍历：O(已置位数)
//
// 实现细节：
//   - RoaringBitmap v2 的 Bitmap 类型只能存储 32 位值
//   - 使用分段策略：将 64 位位索引分成高 32 位和低 32 位
//   - segments map[uint32]*roaring.Bitmap 存储多个 32 位 RoaringBitmap
//   - 每个 key 代表一个 2^32 位的区间，支持任意大的位图
type bitmap struct {
	bits     uint64                     // 目标覆盖的位总数（= 向上取整(容量/粒度)）
	segments map[uint32]*roaring.Bitmap // 分段存储，key 为高 32 位
}

func newBitmap(bitCount uint64) *bitmap {
	return &bitmap{
		bits:     bitCount,
		segments: make(map[uint32]*roaring.Bitmap),
	}
}

// segmentIndex 返回位索引对应的段索引（高 32 位）
func segmentIndex(bit uint64) uint32 {
	return uint32(bit >> 32)
}

// segmentOffset 返回位索引在段内的偏移（低 32 位）
func segmentOffset(bit uint64) uint32 {
	return uint32(bit & 0xFFFFFFFF)
}

// getSegment 获取或创建指定段
func (b *bitmap) getSegment(idx uint32) *roaring.Bitmap {
	if seg, ok := b.segments[idx]; ok {
		return seg
	}
	seg := roaring.New()
	b.segments[idx] = seg
	return seg
}

// isRangeSet 判断 [start, start+length) 是否全部已置位。
// length 为 0 时视为恒真。
func (b *bitmap) isRangeSet(start, length uint64) bool {
	if length == 0 {
		return true
	}
	if start >= b.bits || start+length > b.bits {
		return false
	}

	end := start + length
	startSeg := segmentIndex(start)
	endSeg := segmentIndex(end - 1)

	// 遍历所有涉及的段
	for segIdx := startSeg; segIdx <= endSeg; segIdx++ {
		seg, ok := b.segments[segIdx]
		if !ok {
			return false // 段不存在，说明有未置位的位
		}

		// 计算当前段内需要检查的范围
		segStart := uint64(segIdx) << 32
		checkStart := start
		if checkStart < segStart {
			checkStart = segStart
		}
		checkEnd := end
		segEnd := segStart + (1 << 32)
		if checkEnd > segEnd {
			checkEnd = segEnd
		}

		// 检查段内 [checkStart-segStart, checkEnd-segStart) 是否全部置位
		segCheckStart := segmentOffset(checkStart)
		segCheckEnd := segmentOffset(checkEnd-1) + 1

		// 使用 NextAbsentValue 检查是否有未置位的位
		pos := segCheckStart
		for pos < segCheckEnd {
			nextAbsent := seg.NextAbsentValue(pos)
			if nextAbsent < 0 || uint32(nextAbsent) >= segCheckEnd {
				// 没有找到未置位的位
				break
			}
			// 找到未置位的位，说明区间未全部置位
			return false
		}
	}

	return true
}

// nextClear 返回 [start, limit) 内第一个未置位的位索引；
// 若不存在返回 limit。start 必须是粒度对齐的位索引。
func (b *bitmap) nextClear(start, limit uint64) uint64 {
	if start >= limit {
		return limit
	}

	startSeg := segmentIndex(start)
	limitSeg := segmentIndex(limit - 1)

	// 遍历所有涉及的段
	for segIdx := startSeg; segIdx <= limitSeg; segIdx++ {
		seg, ok := b.segments[segIdx]
		if !ok {
			// 段不存在，说明 start 就是未置位的位
			return start
		}

		// 计算当前段内需要检查的范围
		segStart := uint64(segIdx) << 32
		checkStart := start
		if checkStart < segStart {
			checkStart = segStart
		}
		checkLimit := limit
		segEnd := segStart + (1 << 32)
		if checkLimit > segEnd {
			checkLimit = segEnd
		}

		// 在段内查找第一个未置位的位
		segCheckStart := segmentOffset(checkStart)
		segCheckLimit := segmentOffset(checkLimit-1) + 1

		pos := segCheckStart
		for pos < segCheckLimit {
			nextAbsent := seg.NextAbsentValue(pos)
			if nextAbsent < 0 {
				// 段内没有未置位的位，跳到下一段
				break
			}
			if uint32(nextAbsent) >= segCheckLimit {
				// 未置位的位超出检查范围，跳到下一段
				break
			}
			// 找到未置位的位
			return segStart + uint64(nextAbsent)
		}

		// 当前段内没有找到，跳到下一段
		start = segEnd
	}

	return limit
}

// nextSet 返回 [start, limit) 内第一个已置位的位索引；
// 若不存在返回 limit。
func (b *bitmap) nextSet(start, limit uint64) uint64 {
	if start >= limit {
		return limit
	}

	startSeg := segmentIndex(start)
	limitSeg := segmentIndex(limit - 1)

	// 遍历所有涉及的段
	for segIdx := startSeg; segIdx <= limitSeg; segIdx++ {
		seg, ok := b.segments[segIdx]
		if !ok {
			// 段不存在，跳到下一段
			segEnd := (uint64(segIdx) + 1) << 32
			if segEnd > limit {
				break
			}
			start = segEnd
			continue
		}

		// 计算当前段内需要检查的范围
		segStart := uint64(segIdx) << 32
		checkStart := start
		if checkStart < segStart {
			checkStart = segStart
		}
		checkLimit := limit
		segEnd := segStart + (1 << 32)
		if checkLimit > segEnd {
			checkLimit = segEnd
		}

		// 在段内查找第一个已置位的位
		segCheckStart := segmentOffset(checkStart)
		segCheckLimit := segmentOffset(checkLimit-1) + 1

		nextSet := seg.NextValue(segCheckStart)
		if nextSet >= 0 && uint32(nextSet) < segCheckLimit {
			// 找到已置位的位
			return segStart + uint64(nextSet)
		}

		// 当前段内没有找到，跳到下一段
		start = segEnd
	}

	return limit
}

// setRange 把 [start, start+length) 置位。
// length 为 0 时不做任何事。
func (b *bitmap) setRange(start, length uint64) {
	if length == 0 {
		return
	}
	if start >= b.bits || start+length > b.bits {
		return
	}

	end := start + length
	startSeg := segmentIndex(start)
	endSeg := segmentIndex(end - 1)

	// 遍历所有涉及的段
	for segIdx := startSeg; segIdx <= endSeg; segIdx++ {
		seg := b.getSegment(segIdx)

		// 计算当前段内需要设置的范围
		segStart := uint64(segIdx) << 32
		setStart := start
		if setStart < segStart {
			setStart = segStart
		}
		setEnd := end
		segEnd := segStart + (1 << 32)
		if setEnd > segEnd {
			setEnd = segEnd
		}

		// 在段内设置 [setStart-segStart, setEnd-segStart)
		segSetStart := segmentOffset(setStart)
		segSetEnd := segmentOffset(setEnd-1) + 1
		seg.AddRange(uint64(segSetStart), uint64(segSetEnd))
	}
}

// countSet 统计已置位的位总数。
func (b *bitmap) countSet() uint64 {
	var count uint64
	for _, seg := range b.segments {
		count += seg.GetCardinality()
	}
	return count
}

// SizeInBytes 返回位图当前占用的内存字节数（用于调试和监控）。
func (b *bitmap) SizeInBytes() uint64 {
	var size uint64
	for _, seg := range b.segments {
		size += seg.GetSizeInBytes()
	}
	return size
}
