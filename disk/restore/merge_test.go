package restore

import (
	"bytes"
	"context"
	"math/rand"
	"os"
	"sync"
	"testing"
)

const kib = uint64(1024)

// mustAppend 向合并器追加一个块，失败即终止测试。
func mustAppend(t *testing.T, m *Merger, offset, length uint64, data []byte) {
	t.Helper()
	if err := m.Append(context.Background(), &Block{Offset: offset, Length: length, Data: data}); err != nil {
		t.Fatalf("Append(offset=%d length=%d): %v", offset, length, err)
	}
}

func mustFlush(t *testing.T, m *Merger) {
	t.Helper()
	if err := m.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}
}

// TestMergerUserScenario 复现用户给出的场景：
// 连续 4 个 4KB 块（block 100..103）不应产生 4 次写入，
// 而是合并为一次 16KB 的大块提交。
func TestMergerUserScenario(t *testing.T) {
	target := newTestTarget(t, 1*mib, nil)
	m, err := target.NewMerger(nil)
	if err != nil {
		t.Fatalf("NewMerger: %v", err)
	}

	base := uint64(100) * 4 * kib
	for i := uint64(0); i < 4; i++ {
		data := bytes.Repeat([]byte{byte(0xA0 + i)}, int(4*kib))
		mustAppend(t, m, base+i*4*kib, 4*kib, data)
	}

	// 攒批阶段：尚未提交
	if s := m.Stats(); s.Flushes != 0 || s.BytesOut != 0 {
		t.Fatalf("before flush: flushes=%d bytesOut=%d, want 0/0", s.Flushes, s.BytesOut)
	}

	mustFlush(t, m)

	s := m.Stats()
	if s.BlocksIn != 4 {
		t.Fatalf("blocksIn = %d, want 4", s.BlocksIn)
	}
	if s.Flushes != 1 || s.DirectWrites != 0 {
		t.Fatalf("flushes=%d direct=%d, want 1/0 (4 个相邻 4KB 块应合并为一次提交)", s.Flushes, s.DirectWrites)
	}
	if s.BytesOut != 16*kib || s.Written != 16*kib || s.Skipped != 0 {
		t.Fatalf("bytesOut=%d written=%d skipped=%d, want %d/%d/0",
			s.BytesOut, s.Written, s.Skipped, 16*kib, 16*kib)
	}

	content, err := os.ReadFile(target.Path())
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	for i := uint64(0); i < 4; i++ {
		want := bytes.Repeat([]byte{byte(0xA0 + i)}, int(4*kib))
		off := base + i*4*kib
		if !bytes.Equal(content[off:off+4*kib], want) {
			t.Fatalf("block %d content mismatch", 100+i)
		}
	}
}

// TestMergerMixedAdjacentSizes 验证不同粒度的相邻小块（512B/4KB/8KB 交替）
// 会被攒进同一段缓冲区并一次性提交。
func TestMergerMixedAdjacentSizes(t *testing.T) {
	target := newTestTarget(t, 1*mib, nil)
	m, err := target.NewMerger(nil)
	if err != nil {
		t.Fatalf("NewMerger: %v", err)
	}

	sizes := []uint64{512, 4 * kib, 8 * kib, 4 * kib, 512}
	var off uint64
	var total uint64
	var expect []byte
	for i, sz := range sizes {
		data := bytes.Repeat([]byte{byte(i + 1)}, int(sz))
		mustAppend(t, m, off, sz, data)
		expect = append(expect, data...)
		off += sz
		total += sz
	}
	mustFlush(t, m)

	s := m.Stats()
	if s.Flushes != 1 {
		t.Fatalf("flushes = %d, want 1", s.Flushes)
	}
	if s.BytesOut != total {
		t.Fatalf("bytesOut = %d, want %d", s.BytesOut, total)
	}

	content, err := os.ReadFile(target.Path())
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if !bytes.Equal(content[:total], expect) {
		t.Fatalf("merged content mismatch")
	}
}

// TestMergerBufferFullFlush 验证缓冲区攒满上限时立即刷出。
func TestMergerBufferFullFlush(t *testing.T) {
	target := newTestTarget(t, 1*mib, nil)
	m, err := target.NewMerger(&MergeOption{MaxBufferBytes: 8 * kib})
	if err != nil {
		t.Fatalf("NewMerger: %v", err)
	}

	// 两个 4KB 块恰好攒满 8KB 上限 → 自动刷出
	mustAppend(t, m, 0, 4*kib, bytes.Repeat([]byte{0x01}, int(4*kib)))
	mustAppend(t, m, 4*kib, 4*kib, bytes.Repeat([]byte{0x02}, int(4*kib)))

	if s := m.Stats(); s.Flushes != 1 || s.BytesOut != 8*kib {
		t.Fatalf("after full buffer: flushes=%d bytesOut=%d, want 1/%d", s.Flushes, s.BytesOut, 8*kib)
	}

	// 第三块进入新缓冲区，需要显式 Flush
	mustAppend(t, m, 8*kib, 4*kib, bytes.Repeat([]byte{0x03}, int(4*kib)))
	if s := m.Stats(); s.Flushes != 1 {
		t.Fatalf("flushes = %d, want 1 (third block pending)", s.Flushes)
	}
	mustFlush(t, m)
	if s := m.Stats(); s.Flushes != 2 || s.Written != 12*kib {
		t.Fatalf("after flush: flushes=%d written=%d, want 2/%d", s.Flushes, s.Written, 12*kib)
	}
}

// TestMergerGapSplitsMerge 验证间隙（空洞）会切断合并。
func TestMergerGapSplitsMerge(t *testing.T) {
	target := newTestTarget(t, 1*mib, nil)
	m, err := target.NewMerger(nil)
	if err != nil {
		t.Fatalf("NewMerger: %v", err)
	}

	mustAppend(t, m, 0, 4*kib, bytes.Repeat([]byte{0x11}, int(4*kib)))
	// 跳过 [4KB, 8KB)，第二块从 8KB 开始
	mustAppend(t, m, 8*kib, 4*kib, bytes.Repeat([]byte{0x22}, int(4*kib)))
	mustFlush(t, m)

	s := m.Stats()
	if s.Flushes != 2 {
		t.Fatalf("flushes = %d, want 2 (gap must split the merge)", s.Flushes)
	}

	content, err := os.ReadFile(target.Path())
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if !bytes.Equal(content[:4*kib], bytes.Repeat([]byte{0x11}, int(4*kib))) {
		t.Fatalf("first segment wrong")
	}
	if !bytes.Equal(content[4*kib:8*kib], make([]byte, 4*kib)) {
		t.Fatalf("gap region must remain untouched")
	}
	if !bytes.Equal(content[8*kib:12*kib], bytes.Repeat([]byte{0x22}, int(4*kib))) {
		t.Fatalf("second segment wrong")
	}
}

// TestMergerBackwardOverlap 验证回退重叠块不覆盖先写的数据
// （与逐块 replay 的位图"先写者胜"语义一致）。
func TestMergerBackwardOverlap(t *testing.T) {
	target := newTestTarget(t, 1*mib, nil)
	m, err := target.NewMerger(nil)
	if err != nil {
		t.Fatalf("NewMerger: %v", err)
	}

	first := bytes.Repeat([]byte{0xAA}, int(8*kib))
	mustAppend(t, m, 0, 8*kib, first)
	mustFlush(t, m)

	// 回退到 0，用不同数据重放 16KB：[0,8KB) 已恢复应被位图跳过
	second := bytes.Repeat([]byte{0xBB}, int(16*kib))
	mustAppend(t, m, 0, 16*kib, second)
	mustFlush(t, m)

	s := m.Stats()
	if s.Written != 16*kib || s.Skipped != 8*kib {
		t.Fatalf("written=%d skipped=%d, want %d/%d", s.Written, s.Skipped, 16*kib, 8*kib)
	}

	content, err := os.ReadFile(target.Path())
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if !bytes.Equal(content[:8*kib], first) {
		t.Fatalf("first data must win")
	}
	if !bytes.Equal(content[8*kib:16*kib], second[8*kib:]) {
		t.Fatalf("new tail not restored")
	}
}

// TestMergerOverlapPendingFlushesFirst 验证新块与"待刷出缓冲区"重叠时，
// 会先刷出缓冲区再由位图裁剪，等价于逐块 replay。
func TestMergerOverlapPendingFlushesFirst(t *testing.T) {
	target := newTestTarget(t, 1*mib, nil)
	m, err := target.NewMerger(nil)
	if err != nil {
		t.Fatalf("NewMerger: %v", err)
	}

	// 缓冲区 pending [0, 4KB)，新块回退到 2KB 与之部分重叠
	mustAppend(t, m, 0, 4*kib, bytes.Repeat([]byte{0x01}, int(4*kib)))
	mustAppend(t, m, 2*kib, 4*kib, bytes.Repeat([]byte{0x02}, int(4*kib)))
	mustFlush(t, m)

	// pending [0,4KB) 先刷出 → [0,4KB) 已置位；
	// 回退块 [2KB,6KB) 中 [2KB,4KB) 被跳过，[4KB,6KB) 写入
	s := m.Stats()
	if s.Written != 6*kib || s.Skipped != 2*kib {
		t.Fatalf("written=%d skipped=%d, want %d/%d", s.Written, s.Skipped, 6*kib, 2*kib)
	}

	content, err := os.ReadFile(target.Path())
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if !bytes.Equal(content[:4*kib], bytes.Repeat([]byte{0x01}, int(4*kib))) {
		t.Fatalf("pending buffer data must win")
	}
	if !bytes.Equal(content[4*kib:6*kib], bytes.Repeat([]byte{0x02}, int(2*kib))) {
		t.Fatalf("tail of backward block not restored")
	}
}

// TestMergerDirectWrite 验证单个大块（≥ 缓冲上限）绕过缓冲区直写。
func TestMergerDirectWrite(t *testing.T) {
	target := newTestTarget(t, 1*mib, nil)
	m, err := target.NewMerger(&MergeOption{MaxBufferBytes: 8 * kib})
	if err != nil {
		t.Fatalf("NewMerger: %v", err)
	}

	big := bytes.Repeat([]byte{0x5A}, int(16*kib))
	mustAppend(t, m, 0, 16*kib, big)

	s := m.Stats()
	if s.DirectWrites != 1 || s.Flushes != 0 {
		t.Fatalf("direct=%d flushes=%d, want 1/0", s.DirectWrites, s.Flushes)
	}
	if s.Written != 16*kib {
		t.Fatalf("written = %d, want %d", s.Written, 16*kib)
	}

	content, err := os.ReadFile(target.Path())
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if !bytes.Equal(content[:16*kib], big) {
		t.Fatalf("direct write content mismatch")
	}
}

// TestMergerDirectWritePreservesOrder 验证大块直写前会先刷出 pending，
// 提交顺序不被打乱（否则重叠数据的先后语义会出错）。
func TestMergerDirectWritePreservesOrder(t *testing.T) {
	target := newTestTarget(t, 1*mib, nil)
	m, err := target.NewMerger(&MergeOption{MaxBufferBytes: 8 * kib})
	if err != nil {
		t.Fatalf("NewMerger: %v", err)
	}

	// pending [0, 4KB)
	mustAppend(t, m, 0, 4*kib, bytes.Repeat([]byte{0x01}, int(4*kib)))
	// 回退大块 [0, 16KB)：必须先刷出 pending，再直写大块；
	// 若顺序颠倒，[0,4KB) 会被大块覆盖
	mustAppend(t, m, 0, 16*kib, bytes.Repeat([]byte{0x02}, int(16*kib)))
	mustFlush(t, m)

	content, err := os.ReadFile(target.Path())
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if !bytes.Equal(content[:4*kib], bytes.Repeat([]byte{0x01}, int(4*kib))) {
		t.Fatalf("pending data must be submitted before the direct big block")
	}
	if !bytes.Equal(content[4*kib:16*kib], bytes.Repeat([]byte{0x02}, int(12*kib))) {
		t.Fatalf("big block tail not restored")
	}
}

// TestMergerEmptyAndValidation 覆盖空块、参数校验等边界。
func TestMergerEmptyAndValidation(t *testing.T) {
	target := newTestTarget(t, 1*mib, nil)
	m, err := target.NewMerger(nil)
	if err != nil {
		t.Fatalf("NewMerger: %v", err)
	}

	// 空块：静默忽略，不计入统计
	if err := m.Append(context.Background(), &Block{Offset: 0, Length: 0}); err != nil {
		t.Fatalf("empty block: %v", err)
	}
	if err := m.Append(context.Background(), nil); err == nil {
		t.Fatalf("nil block should fail")
	}
	if s := m.Stats(); s.BlocksIn != 0 {
		t.Fatalf("blocksIn = %d, want 0", s.BlocksIn)
	}

	// 非对齐偏移
	if err := m.Append(context.Background(), &Block{Offset: 100, Length: 512, Data: make([]byte, 512)}); err == nil {
		t.Fatalf("unaligned offset should fail")
	}
	// 超出容量
	if err := m.Append(context.Background(), &Block{Offset: mib - 512, Length: 4 * kib, Data: make([]byte, 4*kib)}); err == nil {
		t.Fatalf("exceeding capacity should fail")
	}
	// nil target
	if _, err := NewMerger(nil, nil); err == nil {
		t.Fatalf("nil target should fail")
	}
	// 出错不应留下脏的待刷出数据
	mustFlush(t, m)
	if s := m.Stats(); s.BytesOut != 0 {
		t.Fatalf("bytesOut = %d, want 0 after rejected appends", s.BytesOut)
	}
}

// TestMergerFlushWithoutPending 验证空缓冲区上的 Flush 不产生提交。
func TestMergerFlushWithoutPending(t *testing.T) {
	target := newTestTarget(t, 1*mib, nil)
	m, err := target.NewMerger(nil)
	if err != nil {
		t.Fatalf("NewMerger: %v", err)
	}

	for i := 0; i < 3; i++ {
		mustFlush(t, m)
	}
	if s := m.Stats(); s.Flushes != 0 || s.BytesOut != 0 {
		t.Fatalf("flushes=%d bytesOut=%d, want 0/0", s.Flushes, s.BytesOut)
	}
}

// TestMergerDuplicateStream 验证整段重复的日志流第二次全部被位图跳过。
func TestMergerDuplicateStream(t *testing.T) {
	target := newTestTarget(t, 256*kib, nil)
	m, err := target.NewMerger(nil)
	if err != nil {
		t.Fatalf("NewMerger: %v", err)
	}

	blocks := make([]*Block, 32)
	for i := range blocks {
		blocks[i] = &Block{
			Offset: uint64(i) * 4 * kib,
			Length: 4 * kib,
			Data:   bytes.Repeat([]byte{byte(i)}, int(4*kib)),
		}
	}

	for _, blk := range blocks {
		mustAppend(t, m, blk.Offset, blk.Length, blk.Data)
	}
	mustFlush(t, m)
	for _, blk := range blocks {
		mustAppend(t, m, blk.Offset, blk.Length, blk.Data)
	}
	mustFlush(t, m)

	s := m.Stats()
	if s.Written != 128*kib {
		t.Fatalf("written = %d, want %d", s.Written, 128*kib)
	}
	if s.Skipped != 128*kib {
		t.Fatalf("skipped = %d, want %d (duplicate stream must be fully deduped)", s.Skipped, 128*kib)
	}
}

// TestMergerCancelledContext 验证取消的 ctx 会被拒绝。
func TestMergerCancelledContext(t *testing.T) {
	target := newTestTarget(t, 1*mib, nil)
	m, err := target.NewMerger(nil)
	if err != nil {
		t.Fatalf("NewMerger: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.Append(ctx, &Block{Offset: 0, Length: 512, Data: make([]byte, 512)}); err == nil {
		t.Fatalf("Append with cancelled context should fail")
	}
	if err := m.Flush(ctx); err == nil {
		t.Fatalf("Flush with cancelled context should fail")
	}
}

// genBlocks 生成一段随机块序列：随机大小、随机前进/回退/间隙，
// 每个块的数据模式互不相同，避免同值数据掩盖错误。
func genBlocks(rng *rand.Rand, count int, capacity uint64, granularity uint64) []*Block {
	blocks := make([]*Block, 0, count)
	pos := uint64(0)
	sizes := []uint64{512, 1024, 2 * kib, 4 * kib, 8 * kib}
	units := int(capacity / granularity)

	for i := 0; i < count; i++ {
		size := sizes[rng.Intn(len(sizes))]

		// 位置策略：30% 紧跟上一块结尾、30% 小幅回退、40% 随机跳跃
		switch rng.Intn(10) {
		case 0, 1, 2:
			// 保持 pos（紧跟）
		case 3, 4, 5:
			// 回退：随机回到之前某处
			if pos > 0 {
				back := rng.Intn(int(minUint64(pos/granularity, 64)) + 1)
				pos -= uint64(back) * granularity
			}
		default:
			pos = uint64(rng.Intn(units)) * granularity
		}

		if size > capacity-pos {
			size = capacity - pos
			size -= size % granularity
			if size == 0 {
				pos = 0
				size = sizes[0]
			}
		}

		data := make([]byte, size)
		for j := range data {
			data[j] = byte(i*7 + j*13)
		}
		blocks = append(blocks, &Block{Offset: pos, Length: size, Data: data})
		pos += size
	}
	return blocks
}

func minUint64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

// TestMergerEquivalenceRandom 是核心正确性测试：
// 对同一段随机块序列（含回退/重叠/间隙），分别用
//  (1) 逐块直接调用 Target.Restore（基准语义）
//  (2) 经 Merger 合并后提交
// 两种方式恢复，最终目标内容必须逐字节相同。
// 用小缓冲区放大刷出次数，覆盖更多合并边界。
func TestMergerEquivalenceRandom(t *testing.T) {
	const capacity = 4 * mib
	const granularity = uint64(512)

	for seed := int64(1); seed <= 5; seed++ {
		rng := rand.New(rand.NewSource(seed))
		blocks := genBlocks(rng, 3000, capacity, granularity)

		// 基准路径：逐块 replay
		refTarget := newTestTarget(t, capacity, nil)
		for _, blk := range blocks {
			if _, err := refTarget.Restore(context.Background(), blk); err != nil {
				t.Fatalf("seed %d: direct Restore: %v", seed, err)
			}
		}
		refContent, err := os.ReadFile(refTarget.Path())
		if err != nil {
			t.Fatalf("seed %d: read ref: %v", seed, err)
		}

		// 合并路径：小缓冲（16KB）强制频繁刷出
		mergedTarget := newTestTarget(t, capacity, nil)
		m, err := mergedTarget.NewMerger(&MergeOption{MaxBufferBytes: 16 * kib})
		if err != nil {
			t.Fatalf("seed %d: NewMerger: %v", seed, err)
		}
		for _, blk := range blocks {
			mustAppend(t, m, blk.Offset, blk.Length, blk.Data)
		}
		mustFlush(t, m)
		mergedContent, err := os.ReadFile(mergedTarget.Path())
		if err != nil {
			t.Fatalf("seed %d: read merged: %v", seed, err)
		}

		if !bytes.Equal(refContent, mergedContent) {
			t.Fatalf("seed %d: merged result differs from direct replay", seed)
		}

		// 统计不变量检查
		s := m.Stats()
		if s.BlocksIn != uint64(len(blocks)) {
			t.Fatalf("seed %d: blocksIn=%d, want %d", seed, s.BlocksIn, len(blocks))
		}
		if s.BytesOut != s.Written+s.Skipped {
			t.Fatalf("seed %d: bytesOut(%d) != written(%d)+skipped(%d)",
				seed, s.BytesOut, s.Written, s.Skipped)
		}
		if s.BytesOut != s.BytesIn {
			t.Fatalf("seed %d: bytesOut=%d, bytesIn=%d (合并层不应增删字节)", seed, s.BytesOut, s.BytesIn)
		}
		if s.IOSubmitted() == 0 {
			t.Fatalf("seed %d: no IO submitted", seed)
		}
		if s.IOSubmitted() >= uint64(len(blocks)) {
			t.Fatalf("seed %d: ios=%d >= blocks=%d (合并没有生效)", seed, s.IOSubmitted(), len(blocks))
		}
		t.Logf("seed %d: blocks=%d ios=%d avgIO=%dB", seed, len(blocks), s.IOSubmitted(), s.AvgIOSize())
	}
}

// TestMergerConcurrent 验证并发 Append 的安全性：
// 多个 goroutine 写入互不重叠的区域，最终每个区域内容正确。
func TestMergerConcurrent(t *testing.T) {
	const workers = 8
	const blocksPerWorker = 64
	const blockSize = 4 * kib

	// 块之间留 4KB 间隙，实际跨度为每块 2*blockSize，容量按跨度计算
	target := newTestTarget(t, workers*blocksPerWorker*2*blockSize, nil)
	m, err := target.NewMerger(&MergeOption{MaxBufferBytes: 64 * kib})
	if err != nil {
		t.Fatalf("NewMerger: %v", err)
	}

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			// 每个 worker 的区块之间留 4KB 间隙，确保与其他区块不相邻，
			// 各区域的写入内容不受合并顺序影响。
			for i := 0; i < blocksPerWorker; i++ {
				off := uint64(w*blocksPerWorker+i) * 2 * blockSize
				data := bytes.Repeat([]byte{byte(w + 1)}, int(blockSize))
				if err := m.Append(context.Background(), &Block{Offset: off, Length: blockSize, Data: data}); err != nil {
					t.Errorf("worker %d block %d: %v", w, i, err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	mustFlush(t, m)

	content, err := os.ReadFile(target.Path())
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	for w := 0; w < workers; w++ {
		for i := 0; i < blocksPerWorker; i++ {
			off := uint64(w*blocksPerWorker+i) * 2 * blockSize
			want := bytes.Repeat([]byte{byte(w + 1)}, int(blockSize))
			if !bytes.Equal(content[off:off+blockSize], want) {
				t.Fatalf("worker %d block %d content mismatch", w, i)
			}
			// 间隙保持为零
			if !bytes.Equal(content[off+blockSize:off+2*blockSize], make([]byte, blockSize)) {
				t.Fatalf("worker %d block %d gap was written", w, i)
			}
		}
	}
}

// makeSmallIOBlocks 生成模拟用户场景的小 IO 日志序列：
// 512B/4KB/8KB/4KB/512B 循环、严格相邻、总计恰好 totalBytes。
func makeSmallIOBlocks(totalBytes uint64) []*Block {
	pattern := []uint64{512, 4 * kib, 8 * kib, 4 * kib, 512}
	var blocks []*Block
	var off uint64
	for i := 0; off < totalBytes; i++ {
		size := pattern[i%len(pattern)]
		if size > totalBytes-off {
			size = totalBytes - off
		}
		data := make([]byte, size)
		for j := range data {
			data[j] = byte(i + j)
		}
		blocks = append(blocks, &Block{Offset: off, Length: size, Data: data})
		off += size
	}
	return blocks
}

// BenchmarkMergerVsDirect 模拟用户场景（大量 512B/4KB/8KB 小 IO 顺序恢复），
// 对比逐块直接写入与经 Merger 合并写入的吞吐。
func BenchmarkMergerVsDirect(b *testing.B) {
	const capacity = 16 * mib
	blocks := makeSmallIOBlocks(capacity)

	b.Run("Direct", func(b *testing.B) {
		b.SetBytes(int64(capacity))
		for i := 0; i < b.N; i++ {
			target := newBenchTarget(b, capacity)
			for _, blk := range blocks {
				if _, err := target.Restore(context.Background(), blk); err != nil {
					b.Fatalf("Restore: %v", err)
				}
			}
			_ = target.Close()
		}
	})

	b.Run("Merged", func(b *testing.B) {
		b.SetBytes(int64(capacity))
		for i := 0; i < b.N; i++ {
			target := newBenchTarget(b, capacity)
			m, err := target.NewMerger(nil)
			if err != nil {
				b.Fatalf("NewMerger: %v", err)
			}
			for _, blk := range blocks {
				if err := m.Append(context.Background(), blk); err != nil {
					b.Fatalf("Append: %v", err)
				}
			}
			if err := m.Flush(context.Background()); err != nil {
				b.Fatalf("Flush: %v", err)
			}
			if i == 0 {
				s := m.Stats()
				b.ReportMetric(float64(len(blocks)), "blocks")
				b.ReportMetric(float64(s.IOSubmitted()), "io-submitted")
				b.ReportMetric(float64(s.AvgIOSize()), "avg-io-size-B")
			}
			_ = target.Close()
		}
	})
}

// newBenchTarget 为基准测试创建并打开一个目标文件。
func newBenchTarget(b *testing.B, capacity uint64) *Target {
	b.Helper()
	path := b.TempDir() + "/target.img"
	f, err := os.Create(path)
	if err != nil {
		b.Fatalf("create: %v", err)
	}
	if err := f.Truncate(int64(capacity)); err != nil {
		b.Fatalf("truncate: %v", err)
	}
	_ = f.Close()

	target, err := OpenPath(path, &Option{Capacity: capacity, NoSyncOnClose: true})
	if err != nil {
		b.Fatalf("OpenPath: %v", err)
	}
	return target
}
