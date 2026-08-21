package restore

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkSmallRestore 小规模基准测试（10 MB）
func BenchmarkSmallRestore(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "target.img")
	const capacity = 10 * 1024 * 1024 // 10 MB

	f, err := os.Create(path)
	if err != nil {
		b.Fatalf("create: %v", err)
	}
	if err := f.Truncate(capacity); err != nil {
		b.Fatalf("truncate: %v", err)
	}
	_ = f.Close()

	block := make([]byte, 512)
	for i := range block {
		block[i] = byte(i)
	}

	target, err := OpenPath(path, &Option{Capacity: capacity})
	if err != nil {
		b.Fatalf("OpenPath: %v", err)
	}
	defer target.Close()

	ctx := context.Background()
	blockCount := int(capacity / 512)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < blockCount; j++ {
			if _, err := target.Restore(ctx, &Block{Offset: uint64(j) * 512, Length: 512, Data: block}); err != nil {
				b.Fatalf("Restore: %v", err)
			}
		}
	}
	b.StopTimer()

	req, written, skipped := target.Stats()
	b.ReportMetric(float64(req), "requested-B")
	b.ReportMetric(float64(written), "written-B")
	b.ReportMetric(float64(skipped), "skipped-B")
}

// BenchmarkMemoryUsage 内存占用对比测试
func BenchmarkMemoryUsage(b *testing.B) {
	const capacity = 1024 * 1024 * 1024 // 1 GB
	bitmap := newBitmap(capacity / 512)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bitmap.setRange(0, 1024)
		size := bitmap.SizeInBytes()
		b.ReportMetric(float64(size), "bitmap-bytes")
	}
}

// TestSparseMemoryUsage 验证稀疏场景的内存占用
func TestSparseMemoryUsage(t *testing.T) {
	const capacity = 1024 * 1024 * 1024 // 1 GB
	bitmap := newBitmap(capacity / 512)

	// 初始内存
	initialSize := bitmap.SizeInBytes()
	t.Logf("Initial bitmap size: %d bytes", initialSize)

	// 恢复 1% 的数据
	for i := 0; i < 1000; i++ {
		bitmap.setRange(uint64(i)*2048, 512)
	}
	afterSparseSize := bitmap.SizeInBytes()
	t.Logf("After 1%% sparse restore: %d bytes", afterSparseSize)

	// 恢复 50% 的数据（连续区间）
	bitmap.setRange(0, capacity/2/512)
	afterHalfSize := bitmap.SizeInBytes()
	t.Logf("After 50%% restore: %d bytes", afterHalfSize)

	// 恢复 100% 的数据
	bitmap.setRange(0, capacity/512)
	afterFullSize := bitmap.SizeInBytes()
	t.Logf("After 100%% restore: %d bytes", afterFullSize)

	// 对比固定位图
	fixedBitmapSize := capacity / 512 / 8
	t.Logf("Fixed bitmap would be: %d bytes", fixedBitmapSize)

	// 验证稀疏场景确实更省内存
	if afterSparseSize >= uint64(fixedBitmapSize) {
		t.Errorf("Sparse bitmap (%d bytes) should be smaller than fixed bitmap (%d bytes)",
			afterSparseSize, fixedBitmapSize)
	}
}
