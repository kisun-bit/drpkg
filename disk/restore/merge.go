package restore

// Merger：小 IO 流式合并器。
//
// # 背景
//
// 原机采集的 IO 日志往往包含大量小 IO（512B / 4KB / 8KB 交替出现）。
// 恢复时若严格按原始粒度逐块 replay：
//
//	pwrite(fd, 512B, ...)
//	pwrite(fd, 4KB, ...)
//	pwrite(fd, 512B, ...)
//
// 即使底层 NVMe 很快，也会被三个因素拖住：
//  1. 系统调用次数与小 IO 数量成正比，CPU 开销大；
//  2. 小 IO 受设备 IOPS 上限约束，用不满顺序带宽；
//  3. 单线程顺序写的队列深度只有 1，填不满设备流水线。
//
// # 算法：流式相邻合并（streaming contiguous coalescing）
//
// Merger 在日志块流与 Target 之间做一层缓冲，把相邻的小块攒成大块再提交：
//
//  1. 缓冲区不变式：缓冲区始终表示一段【连续、无重叠、尚未写入】的数据
//     区间 [pendOff, pendOff+n)。
//  2. 到达一个新块 blk：
//     a. blk.Offset == pendOff+n（严格相邻）：追加进缓冲区；
//        追加后缓冲区达到上限则立即刷出。
//     b. blk.Offset != pendOff+n（回退 / 重叠 / 间隙）：先刷出缓冲区，
//        再按 (c) 处理 blk。重叠数据由 Target 的去重位图裁决
//        （先写者胜），因此最终结果与逐块 replay 完全一致。
//     c. 单块长度 >= 缓冲区上限：绕过缓冲区直接提交（零拷贝）。
//  3. 刷出 = 把整段缓冲区作为一个大块调用 Target.Restore，
//     位图去重对其同样生效（整段已恢复过时一次比较即跳过）。
//
// 效果：N 个小块的写系统调用从 N 次降为约 总字节数/缓冲区大小 次，
// 单次提交变为大 IO，顺序带宽和设备队列利用率显著提升。
//
// # 等价性保证
//
// 对任意块序列（含回退、重叠、间隙），经 Merger 恢复后的目标内容与
// 逐块调用 Target.Restore 逐字节相同：
//   - 缓冲区从不保存同一偏移的两份数据（重叠必触发先刷出）；
//   - 提交顺序与逻辑顺序一致，重叠区域由位图按"先写者胜"裁剪；
//   - 间隙区域保持原样，不会被填充。
//
// # 用法
//
//	target, _ := restore.OpenPath("/dev/sdb", nil)
//	defer target.Close()
//
//	m, _ := target.NewMerger(nil) // 默认 4MiB 缓冲区
//	for _, blk := range blocksFromLog {
//	    if err := m.Append(ctx, blk); err != nil { return err }
//	}
//	if err := m.Flush(ctx); err != nil { return err } // 刷出尾部残留
//
// Merger 的所有方法都是并发安全的。使用完毕后必须先 Flush 再 Close Target。
// 错误返回后缓冲区中的完整块仍会原样提交（不做拆分重试），
// 由 Target 的位图保证重试安全。

import (
	"context"
	"sync"

	"github.com/pkg/errors"
)

// DefaultMergeBuffer 是合并缓冲区的默认上限：4 MiB。
// 该值在"减少系统调用次数"与"内存占用"之间折中，
// 对 NVMe 顺序写已能产生接近满带宽的大 IO。
const DefaultMergeBuffer uint64 = 4 << 20

// MergeOption 用于定制 Merger 的行为。零值字段使用默认值。
type MergeOption struct {
	// MaxBufferBytes 是合并缓冲区的字节上限，攒满即刷出。
	// 为 0 时使用 DefaultMergeBuffer（4 MiB）。
	// 建议不小于目标粒度的 64 倍；单个块长度达到该上限时绕过缓冲区直写。
	MaxBufferBytes uint64
}

// MergeStats 是 Merger 的统计信息。
type MergeStats struct {
	// BlocksIn 是收到的有效块总数（空块不计入）。
	BlocksIn uint64

	// BytesIn 是收到的有效字节总数（= 各块 min(Length, len(Data)) 之和）。
	BytesIn uint64

	// Flushes 是缓冲区刷出次数（缓冲满 / 流不连续 / 显式 Flush 均计入；
	// 空缓冲区上的刷出不计入）。
	Flushes uint64

	// DirectWrites 是绕过缓冲区直接提交的大块个数。
	DirectWrites uint64

	// BytesOut 是提交给 Target 的字节总数（刷出 + 直写）。
	// 恒有 BytesOut = Written + Skipped。
	BytesOut uint64

	// Written 是 Target 实际写入的字节数。
	Written uint64

	// Skipped 是因 Target 位图去重而跳过的字节数。
	Skipped uint64
}

// IOSubmitted 返回提交给 Target 的大 IO 总次数（刷出 + 直写）。
func (s MergeStats) IOSubmitted() uint64 { return s.Flushes + s.DirectWrites }

// AvgIOSize 返回平均每次提交的 IO 大小（字节）；无提交时返回 0。
// 该值直接反映合并效果：越接近 MaxBufferBytes，说明小 IO 合并越充分。
func (s MergeStats) AvgIOSize() uint64 {
	ios := s.IOSubmitted()
	if ios == 0 {
		return 0
	}
	return s.BytesOut / ios
}

// Merger 把小块恢复流合并为大块写入的中间层。
// 它绑定一个 Target，自身只做内存中的攒批，不持有文件句柄。
type Merger struct {
	target *Target
	maxBuf uint64

	mu      sync.Mutex
	buf     []byte // 固定大小的合并缓冲区，首次需要时分配
	n       int    // 缓冲区中待刷出的字节数
	pendOff uint64 // 待刷出数据在目标上的起始偏移
	hasPend bool

	// 以下统计字段由 mu 保护。
	blocksIn     uint64
	bytesIn      uint64
	flushes      uint64
	directWrites uint64
	bytesOut     uint64
	written      uint64
	skipped      uint64
}

// NewMerger 创建一个绑定 target 的 Merger。
// opt 为 nil 或字段为零时使用默认值。
// 单个 Target 同一时刻只应绑定一个 Merger（多个 Merger 并发绑定
// 虽然安全，但会各自攒批，削弱合并效果）。
func NewMerger(target *Target, opt *MergeOption) (*Merger, error) {
	if target == nil {
		return nil, errors.New("restore: nil merge target")
	}
	maxBuf := DefaultMergeBuffer
	if opt != nil && opt.MaxBufferBytes > 0 {
		maxBuf = opt.MaxBufferBytes
	}
	return &Merger{target: target, maxBuf: maxBuf}, nil
}

// NewMerger 在该目标上创建一个合并器，等价于 restore.NewMerger(t, opt)。
func (t *Target) NewMerger(opt *MergeOption) (*Merger, error) {
	return NewMerger(t, opt)
}

// Append 把一个数据块送入合并器。
//
// 与 Target.Restore 不同，Append 不保证数据立即落盘：相邻小块会先在
// 内存中攒批，直到缓冲满、流不连续或显式 Flush 才提交。返回值只有错误；
// 写入量等反馈通过 Stats 获取。
//
// 参数校验与 Target.Restore 完全一致：nil 块返回错误，空块静默忽略，
// 偏移必须粒度对齐，区间不得超出目标容量。
// 出错时调用方应中止恢复或重试整段日志（重试由位图保证安全）。
func (m *Merger) Append(ctx context.Context, blk *Block) error {
	if blk == nil {
		return errors.New("restore: nil block")
	}
	if blk.Length == 0 || len(blk.Data) == 0 {
		return nil // 空块不产生任何效果，与 Target.Restore 保持一致
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return errors.Wrap(err, "restore: merge cancelled")
	}

	g := m.target.Granularity()
	if blk.Offset%g != 0 {
		return errors.Errorf("restore: merge: offset %d not aligned to granularity %d", blk.Offset, g)
	}

	// 有效长度 = min(Length, len(Data))，与 Target.Restore 保持一致。
	length := blk.Length
	if dataLen := uint64(len(blk.Data)); dataLen < length {
		length = dataLen
	}

	// 容量预检（用减法避免 Offset+length 溢出）。
	capacity := m.target.Capacity()
	if blk.Offset > capacity || length > capacity-blk.Offset {
		return errors.Errorf("restore: merge: block [%d, %d) exceeds capacity %d",
			blk.Offset, blk.Offset+length, capacity)
	}

	m.blocksIn++
	m.bytesIn += length

	// 1) 与缓冲区不连续（回退 / 重叠 / 间隙）：先刷出缓冲区。
	//    与已落盘数据的重叠随后由 Target 位图按"先写者胜"裁剪。
	if m.hasPend && blk.Offset != m.pendOff+uint64(m.n) {
		if err := m.flushLocked(ctx); err != nil {
			return err
		}
	}

	// 2) 大块绕过缓冲区直写（零拷贝）。先刷出残留以保持提交顺序。
	if length >= m.maxBuf {
		if m.hasPend {
			if err := m.flushLocked(ctx); err != nil {
				return err
			}
		}
		return m.directWrite(ctx, blk.Offset, length, blk.Data[:length])
	}

	// 3) 缓冲区装不下：先刷出。
	if m.hasPend && uint64(m.n)+length > m.maxBuf {
		if err := m.flushLocked(ctx); err != nil {
			return err
		}
	}

	// 4) 追加进缓冲区。
	if !m.hasPend {
		if m.buf == nil {
			m.buf = make([]byte, m.maxBuf)
		}
		m.hasPend = true
		m.pendOff = blk.Offset
		m.n = 0
	}
	copy(m.buf[m.n:], blk.Data[:length])
	m.n += int(length)

	// 5) 缓冲区恰好攒满：立即刷出，控制内存水位。
	if uint64(m.n) >= m.maxBuf {
		return m.flushLocked(ctx)
	}
	return nil
}

// Flush 把缓冲区中残留的数据立即提交给 Target。
// 恢复结束（或需要强制落盘时机）时必须调用一次。
// 空缓冲区上的 Flush 不做任何事，返回 nil。
func (m *Merger) Flush(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return errors.Wrap(err, "restore: merge cancelled")
	}
	return m.flushLocked(ctx)
}

// Stats 返回合并器的统计快照。并发安全。
func (m *Merger) Stats() MergeStats {
	m.mu.Lock()
	defer m.mu.Unlock()
	return MergeStats{
		BlocksIn:     m.blocksIn,
		BytesIn:      m.bytesIn,
		Flushes:      m.flushes,
		DirectWrites: m.directWrites,
		BytesOut:     m.bytesOut,
		Written:      m.written,
		Skipped:      m.skipped,
	}
}

// flushLocked 把缓冲区作为一个大块提交给 Target。
// 调用前必须持有 m.mu。
func (m *Merger) flushLocked(ctx context.Context) error {
	if !m.hasPend || m.n == 0 {
		m.hasPend = false
		m.n = 0
		return nil
	}

	off := m.pendOff
	data := m.buf[:m.n]
	// 先清空缓冲区状态再提交：Target.Restore 是同步的，
	// 返回后 data（即 m.buf 前缀）即可安全复用。
	m.hasPend = false
	m.n = 0

	m.flushes++
	m.bytesOut += uint64(len(data))

	w, err := m.target.Restore(ctx, &Block{Offset: off, Length: uint64(len(data)), Data: data})
	m.written += w
	if err != nil {
		return errors.Wrapf(err, "restore: merge flush at %d", off)
	}
	m.skipped += uint64(len(data)) - w
	return nil
}

// directWrite 绕过缓冲区直接提交单个大块。调用前必须持有 m.mu。
func (m *Merger) directWrite(ctx context.Context, off, length uint64, data []byte) error {
	m.directWrites++
	m.bytesOut += length

	w, err := m.target.Restore(ctx, &Block{Offset: off, Length: length, Data: data})
	m.written += w
	if err != nil {
		return errors.Wrapf(err, "restore: merge direct write at %d", off)
	}
	m.skipped += length - w
	return nil
}
