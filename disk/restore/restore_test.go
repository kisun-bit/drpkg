package restore

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

const (
	testGranularity = uint64(512)
	mib             = uint64(1024 * 1024)
)

// newTestTarget 在临时目录创建一个 capacity 字节的目标文件并打开。
func newTestTarget(t *testing.T, capacity uint64, opt *Option) *Target {
	t.Helper()

	path := filepath.Join(t.TempDir(), "target.img")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	if err := f.Truncate(int64(capacity)); err != nil {
		t.Fatalf("truncate target: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close target: %v", err)
	}

	if opt == nil {
		opt = &Option{}
	}
	opt.Capacity = capacity
	if opt.Granularity == 0 {
		opt.Granularity = testGranularity
	}

	target, err := OpenPath(path, opt)
	if err != nil {
		t.Fatalf("OpenPath: %v", err)
	}
	t.Cleanup(func() { _ = target.Close() })
	return target
}

func mustRestore(t *testing.T, target *Target, offset, length uint64, data []byte) uint64 {
	t.Helper()
	n, err := target.Restore(context.Background(), &Block{Offset: offset, Length: length, Data: data})
	if err != nil {
		t.Fatalf("Restore(offset=%d length=%d): %v", offset, length, err)
	}
	return n
}

// TestUserScenario 复现用户给出的场景：
// 第一个块 (0, 1MiB)，第二个块 (0, 2MiB)，
// 第二个块恢复时只应写入 [1MiB, 2MiB) 这 1MiB 新增数据。
func TestUserScenario(t *testing.T) {
	target := newTestTarget(t, 4*mib, nil)

	first := bytes.Repeat([]byte{0xAA}, int(mib))
	second := bytes.Repeat([]byte{0xBB}, int(2*mib))

	n1 := mustRestore(t, target, 0, mib, first)
	if n1 != mib {
		t.Fatalf("first restore written = %d, want %d", n1, mib)
	}

	n2 := mustRestore(t, target, 0, 2*mib, second)
	if n2 != mib {
		t.Fatalf("second restore written = %d, want %d (only the new 1MiB..2MiB part)", n2, mib)
	}

	// 校验落盘内容：[0,1MiB) 保持第一次的数据，[1MiB,2MiB) 是第二次的数据
	content, err := os.ReadFile(target.Path())
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if !bytes.Equal(content[:mib], first) {
		t.Fatalf("bytes [0, 1MiB) were overwritten by the second restore")
	}
	if !bytes.Equal(content[mib:2*mib], second[mib:]) {
		t.Fatalf("bytes [1MiB, 2MiB) were not restored by the second restore")
	}
}

func TestFullDuplicateBlock(t *testing.T) {
	target := newTestTarget(t, 2*mib, nil)

	data := bytes.Repeat([]byte{0x11}, int(mib))
	if n := mustRestore(t, target, 0, mib, data); n != mib {
		t.Fatalf("first written = %d, want %d", n, mib)
	}
	if n := mustRestore(t, target, 0, mib, data); n != 0 {
		t.Fatalf("duplicate written = %d, want 0", n)
	}
	// 完全相同的块换一块数据再发，同样不应写入（按位图去重，不比较内容）
	other := bytes.Repeat([]byte{0x22}, int(mib))
	if n := mustRestore(t, target, 0, mib, other); n != 0 {
		t.Fatalf("re-duplicate written = %d, want 0", n)
	}
}

func TestMiddleOverlap(t *testing.T) {
	target := newTestTarget(t, 4*mib, nil)

	whole := bytes.Repeat([]byte{0x33}, int(2*mib))
	mustRestore(t, target, 0, 2*mib, whole)

	// 完全落在已恢复区间内：0 字节写入
	inner := bytes.Repeat([]byte{0x44}, int(mib))
	if n := mustRestore(t, target, mib/2, mib, inner); n != 0 {
		t.Fatalf("inner overlap written = %d, want 0", n)
	}

	// 半新半旧：[1MiB,2MiB) 已恢复，[2MiB,3MiB) 是新的
	half := bytes.Repeat([]byte{0x55}, int(2*mib))
	if n := mustRestore(t, target, mib, 2*mib, half); n != mib {
		t.Fatalf("half overlap written = %d, want %d", n, mib)
	}

	content, err := os.ReadFile(target.Path())
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if !bytes.Equal(content[:2*mib], whole) {
		t.Fatalf("bytes [0, 2MiB) corrupted")
	}
	if !bytes.Equal(content[2*mib:3*mib], half[mib:]) {
		t.Fatalf("bytes [2MiB, 3MiB) not restored")
	}
}

func TestNonAlignedOffsetRejected(t *testing.T) {
	target := newTestTarget(t, mib, nil)

	data := make([]byte, 1024)
	_, err := target.Restore(context.Background(), &Block{Offset: 100, Length: 1024, Data: data})
	if err == nil {
		t.Fatalf("Restore with offset 100 (granularity 512) should fail")
	}
}

func TestExceedsCapacityRejected(t *testing.T) {
	target := newTestTarget(t, mib, nil)

	data := make([]byte, mib)
	_, err := target.Restore(context.Background(), &Block{Offset: mib / 2, Length: mib, Data: data})
	if err == nil {
		t.Fatalf("Restore exceeding capacity should fail")
	}
}

func TestCustomGranularity(t *testing.T) {
	target := newTestTarget(t, 64*1024, &Option{Granularity: 4096})

	data := bytes.Repeat([]byte{0x66}, int(8192))
	if n := mustRestore(t, target, 0, 8192, data); n != 8192 {
		t.Fatalf("written = %d, want 8192", n)
	}
	if n := mustRestore(t, target, 4096, 8192, data); n != 4096 {
		t.Fatalf("overlap written = %d, want 4096", n)
	}

	// 4096 粒度下 512 不再是对齐值
	_, err := target.Restore(context.Background(), &Block{Offset: 512, Length: 4096, Data: data})
	if err == nil {
		t.Fatalf("offset 512 should be rejected under granularity 4096")
	}
}

func TestTailPartialGranularity(t *testing.T) {
	// capacity 不是粒度的整数倍：1000 字节 / 512 → 2 个 bit
	target := newTestTarget(t, 1000, nil)

	data := bytes.Repeat([]byte{0x77}, 1000)
	if n := mustRestore(t, target, 0, 1000, data); n != 1000 {
		t.Fatalf("written = %d, want 1000", n)
	}
	// 尾部粒度已被整体置位，重复块返回 0
	if n := mustRestore(t, target, 0, 1000, data); n != 0 {
		t.Fatalf("duplicate written = %d, want 0", n)
	}
}

func TestDataShorterThanLength(t *testing.T) {
	target := newTestTarget(t, mib, nil)

	// Length 声明 8KiB，但 Data 只有 4KiB：按 4KiB 处理
	data := bytes.Repeat([]byte{0x88}, 4096)
	if n := mustRestore(t, target, 0, 8192, data); n != 4096 {
		t.Fatalf("written = %d, want 4096 (clamped to len(Data))", n)
	}
}

func TestEmptyBlock(t *testing.T) {
	target := newTestTarget(t, mib, nil)

	if n := mustRestore(t, target, 0, 0, nil); n != 0 {
		t.Fatalf("empty block written = %d, want 0", n)
	}
	if _, err := target.Restore(context.Background(), nil); err == nil {
		t.Fatalf("nil block should fail")
	}
}

func TestStats(t *testing.T) {
	target := newTestTarget(t, 2*mib, nil)

	data := bytes.Repeat([]byte{0x99}, int(mib))
	mustRestore(t, target, 0, mib, data)
	mustRestore(t, target, 0, mib, data)   // 全重复
	mustRestore(t, target, mib, mib, data) // 全新

	requested, written, skipped := target.Stats()
	if requested != 3*mib {
		t.Fatalf("requested = %d, want %d", requested, 3*mib)
	}
	if written != 2*mib {
		t.Fatalf("written = %d, want %d", written, 2*mib)
	}
	if skipped != mib {
		t.Fatalf("skipped = %d, want %d", skipped, mib)
	}
}

func TestRestoreAfterClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "target.img")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_ = f.Truncate(int64(mib))
	_ = f.Close()

	target, err := OpenPath(path, &Option{Capacity: mib})
	if err != nil {
		t.Fatalf("OpenPath: %v", err)
	}
	if err := target.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := target.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	_, err = target.Restore(context.Background(), &Block{Offset: 0, Length: 512, Data: make([]byte, 512)})
	if err == nil {
		t.Fatalf("Restore after Close should fail")
	}
}

func TestCancelledContext(t *testing.T) {
	target := newTestTarget(t, mib, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := target.Restore(ctx, &Block{Offset: 0, Length: 512, Data: make([]byte, 512)})
	if err == nil {
		t.Fatalf("Restore with cancelled context should fail")
	}
}

func TestConcurrentRestore(t *testing.T) {
	const workers = 8
	const blocksPerWorker = 32
	const blockSize = 64 * 1024

	target := newTestTarget(t, workers*blocksPerWorker*blockSize, nil)

	var wg sync.WaitGroup
	var totalWritten uint64
	var mu sync.Mutex

	// 每个 worker 负责互不重叠的区域，另外所有 worker 都重复写第 0 块，
	// 验证并发下重复块只被写入一次。
	shared := bytes.Repeat([]byte{0xEE}, blockSize)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < blocksPerWorker; i++ {
				off := uint64(w*blocksPerWorker+i) * blockSize
				data := bytes.Repeat([]byte{byte(w + 1)}, blockSize)
				n, err := target.Restore(context.Background(), &Block{Offset: off, Length: blockSize, Data: data})
				if err != nil {
					t.Errorf("worker %d block %d: %v", w, i, err)
					return
				}
				mu.Lock()
				totalWritten += n
				mu.Unlock()
			}
			// 重复写共享块
			n, err := target.Restore(context.Background(), &Block{Offset: 0, Length: blockSize, Data: shared})
			if err != nil {
				t.Errorf("worker %d shared block: %v", w, err)
				return
			}
			mu.Lock()
			totalWritten += n
			mu.Unlock()
		}(w)
	}
	wg.Wait()

	want := uint64(workers * blocksPerWorker * blockSize)
	if totalWritten != want {
		t.Fatalf("total written = %d, want %d (shared block must be written exactly once)", totalWritten, want)
	}
}

func BenchmarkRestoreSequential(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "target.img")
	const capacity = 256 * mib

	f, err := os.Create(path)
	if err != nil {
		b.Fatalf("create: %v", err)
	}
	if err := f.Truncate(int64(capacity)); err != nil {
		b.Fatalf("truncate: %v", err)
	}
	_ = f.Close()

	block := make([]byte, mib)
	for i := range block {
		block[i] = byte(i)
	}

	target, err := OpenPath(path, &Option{Capacity: capacity})
	if err != nil {
		b.Fatalf("OpenPath: %v", err)
	}
	defer target.Close()

	ctx := context.Background()
	blockCount := int(capacity / mib)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < blockCount; j++ {
			if _, err := target.Restore(ctx, &Block{Offset: uint64(j) * mib, Length: mib, Data: block}); err != nil {
				b.Fatalf("Restore: %v", err)
			}
		}
	}
	b.StopTimer()

	// 第二轮以后全部命中位图（纯跳过路径），首轮是真实写入路径
	req, written, skipped := target.Stats()
	b.ReportMetric(float64(req), "requested-B")
	b.ReportMetric(float64(written), "written-B")
	b.ReportMetric(float64(skipped), "skipped-B")
}
