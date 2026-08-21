package bitmap

import (
	"bytes"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

func TestSetIsSetClear(t *testing.T) {
	b := NewFsBitmap("test", BitmapFromFS, 100, 4096)

	if b.IsSet(0) {
		t.Fatalf("new bitmap should have no bit set")
	}

	b.Set(0)
	b.Set(99)
	b.Set(50)
	if !b.IsSet(0) || !b.IsSet(50) || !b.IsSet(99) {
		t.Fatalf("Set/IsSet mismatch")
	}
	if b.CountSet() != 3 {
		t.Fatalf("CountSet = %d, want 3", b.CountSet())
	}

	// 越界的 Set/IsSet/Clear 都应被安全忽略
	b.Set(100)
	b.Set(^uint64(0))
	if b.IsSet(100) || b.IsSet(^uint64(0)) {
		t.Fatalf("out-of-range bit must not be set")
	}
	b.Clear(100)
	b.Clear(50)
	if b.IsSet(50) {
		t.Fatalf("Clear failed")
	}
	if b.CountSet() != 2 {
		t.Fatalf("CountSet = %d, want 2", b.CountSet())
	}
}

func TestSetRangeClearRange(t *testing.T) {
	b := NewFsBitmap("test", BitmapFromFS, 1000, 512)

	b.SetRange(10, 20) // [10, 30)
	if b.CountSet() != 20 {
		t.Fatalf("CountSet = %d, want 20", b.CountSet())
	}
	if !b.IsSet(10) || !b.IsSet(29) || b.IsSet(9) || b.IsSet(30) {
		t.Fatalf("SetRange boundary wrong")
	}

	// 超出 Bits 的部分被截断
	b.SetRange(990, 100) // 实际只置 [990, 1000)
	if b.CountSet() != 20+10 {
		t.Fatalf("CountSet = %d, want 30", b.CountSet())
	}
	if !b.IsSet(999) || b.IsSet(1000) {
		t.Fatalf("SetRange clamp wrong")
	}

	// 起点越界 / 长度为 0：无效果
	b.SetRange(1000, 10)
	b.SetRange(50, 0)
	if b.CountSet() != 30 {
		t.Fatalf("CountSet = %d, want 30", b.CountSet())
	}

	// ClearRange 部分清除 + 截断
	b.ClearRange(15, 10) // 清 [15, 25)
	if b.CountSet() != 20 {
		t.Fatalf("CountSet = %d, want 20", b.CountSet())
	}
	if b.IsSet(15) || b.IsSet(24) || !b.IsSet(14) || !b.IsSet(25) {
		t.Fatalf("ClearRange boundary wrong")
	}
	b.ClearRange(995, 100) // 实际清 [995, 1000)
	if b.CountSet() != 15 {
		t.Fatalf("CountSet = %d, want 15", b.CountSet())
	}
}

func TestSetAllCountSet(t *testing.T) {
	const bits = int64(1003) // 故意不是 8 的整数倍
	b := NewFsBitmap("test", BitmapRaw, bits, 512)

	b.SetAll()
	if b.CountSet() != bits {
		t.Fatalf("CountSet = %d, want %d", b.CountSet(), bits)
	}
	if !b.IsSet(0) || !b.IsSet(uint64(bits-1)) {
		t.Fatalf("SetAll must set first and last bit")
	}

	b.Clear(500)
	if b.CountSet() != bits-1 {
		t.Fatalf("CountSet = %d, want %d", b.CountSet(), bits-1)
	}

	if b.Size() != bits*512 {
		t.Fatalf("Size = %d, want %d", b.Size(), bits*512)
	}
	if b.UsedSize() != (bits-1)*512 {
		t.Fatalf("UsedSize = %d, want %d", b.UsedSize(), (bits-1)*512)
	}
	if b.UsedSizeHuman() == "" {
		t.Fatalf("UsedSizeHuman returned empty string")
	}
}

func TestNewFsBitmapFromBytes(t *testing.T) {
	// raw[0] = 0xA5: bit 0,2,5,7; raw[1] = 0x93: bit 8,9,12,15
	// Bits = 13，因此 bit 13,14,15（padding）必须被忽略
	raw := []byte{0xA5, 0x93}
	b := NewFsBitmapFromBytes("ntfs", BitmapFromFS, raw, 13, 4096)

	want := map[uint64]bool{0: true, 2: true, 5: true, 7: true, 8: true, 9: true, 12: true}
	for i := uint64(0); i < 13; i++ {
		if b.IsSet(i) != want[i] {
			t.Fatalf("bit %d = %v, want %v", i, b.IsSet(i), want[i])
		}
	}
	if b.CountSet() != 7 {
		t.Fatalf("CountSet = %d, want 7 (padding bits must be ignored)", b.CountSet())
	}

	// 与手工逐位解析等价
	for i := uint64(0); i < 13; i++ {
		expect := raw[i/8]>>(i%8)&1 == 1
		if b.IsSet(i) != expect {
			t.Fatalf("bit %d disagrees with raw bytes", i)
		}
	}

	// 空输入
	empty := NewFsBitmapFromBytes("ntfs", BitmapFromFS, nil, 100, 4096)
	if empty.CountSet() != 0 {
		t.Fatalf("empty raw must produce empty bitmap")
	}
}

func TestChangeBlockSize(t *testing.T) {
	rng := rand.New(rand.NewSource(42))

	const oldBits = int64(10000)
	b := NewFsBitmap("test", BitmapFromFS, oldBits, 512)
	for k := 0; k < 200; k++ {
		start := rng.Int63n(oldBits)
		length := uint32(1 + rng.Int63n(50))
		b.SetRange(uint64(start), length)
	}
	usedBefore := b.UsedSize()

	// 转换前按定义计算期望结果：新 bit j 置位 ⟺ 旧 bit [j*8, (j+1)*8) 中任意一个置位
	const ratio = int64(8)
	newBits := (oldBits + ratio - 1) / ratio
	expected := make([]bool, newBits)
	for j := int64(0); j < newBits; j++ {
		for i := j * ratio; i < (j+1)*ratio && i < oldBits; i++ {
			if b.IsSet(uint64(i)) {
				expected[j] = true
				break
			}
		}
	}

	if err := b.ChangeBlockSize(4096); err != nil {
		t.Fatalf("ChangeBlockSize: %v", err)
	}
	if b.Bits != newBits || b.BlockSize != 4096 {
		t.Fatalf("after change: Bits=%d BlockSize=%d, want %d/4096", b.Bits, b.BlockSize, newBits)
	}
	for j := int64(0); j < newBits; j++ {
		if b.IsSet(uint64(j)) != expected[j] {
			t.Fatalf("new bit %d = %v, want %v", j, b.IsSet(uint64(j)), expected[j])
		}
	}
	// 块粒度变粗只会多覆盖、不会少覆盖，已使用字节数不会变小
	if b.UsedSize() < usedBefore {
		t.Fatalf("UsedSize shrank after ChangeBlockSize: %d < %d", b.UsedSize(), usedBefore)
	}

	// 相同块大小：无操作
	if err := b.ChangeBlockSize(4096); err != nil {
		t.Fatalf("ChangeBlockSize(same): %v", err)
	}
	// 非法参数
	if err := b.ChangeBlockSize(2048); err == nil {
		t.Fatalf("smaller blocksize must fail")
	}
	if err := b.ChangeBlockSize(5000); err == nil {
		t.Fatalf("non-multiple blocksize must fail")
	}
	if err := b.ChangeBlockSize(0); err == nil {
		t.Fatalf("zero blocksize must fail")
	}
}

func TestNextSetClearBit(t *testing.T) {
	b := NewFsBitmap("test", BitmapFromFS, 1000, 512)

	// 空位图
	if got := b.nextSetBit(0); got != b.Bits {
		t.Fatalf("nextSetBit on empty = %d, want %d", got, b.Bits)
	}
	if got := b.nextClearBit(0); got != 0 {
		t.Fatalf("nextClearBit on empty = %d, want 0", got)
	}

	b.SetRange(10, 10)  // [10, 20)
	b.SetRange(100, 10) // [100, 110)

	if got := b.nextSetBit(0); got != 10 {
		t.Fatalf("nextSetBit(0) = %d, want 10", got)
	}
	if got := b.nextSetBit(10); got != 10 {
		t.Fatalf("nextSetBit(10) = %d, want 10", got)
	}
	if got := b.nextSetBit(11); got != 11 {
		t.Fatalf("nextSetBit(11) = %d, want 11", got)
	}
	if got := b.nextSetBit(20); got != 100 {
		t.Fatalf("nextSetBit(20) = %d, want 100", got)
	}
	if got := b.nextSetBit(110); got != b.Bits {
		t.Fatalf("nextSetBit(110) = %d, want %d", got, b.Bits)
	}

	if got := b.nextClearBit(0); got != 0 {
		t.Fatalf("nextClearBit(0) = %d, want 0", got)
	}
	if got := b.nextClearBit(10); got != 20 {
		t.Fatalf("nextClearBit(10) = %d, want 20", got)
	}
	if got := b.nextClearBit(20); got != 20 {
		t.Fatalf("nextClearBit(20) = %d, want 20", got)
	}

	// from < 0 等价于 from = 0
	if got := b.nextSetBit(-5); got != 10 {
		t.Fatalf("nextSetBit(-5) = %d, want 10", got)
	}
	if got := b.nextClearBit(-5); got != 0 {
		t.Fatalf("nextClearBit(-5) = %d, want 0", got)
	}

	// 全部置位
	all := NewFsBitmap("test", BitmapFromFS, 100, 512)
	all.SetAll()
	if got := all.nextSetBit(0); got != 0 {
		t.Fatalf("nextSetBit(0) on full = %d, want 0", got)
	}
	if got := all.nextClearBit(0); got != all.Bits {
		t.Fatalf("nextClearBit(0) on full = %d, want %d", got, all.Bits)
	}
}

// TestSegmentBoundary 验证位索引跨越 2^32 段边界时的正确性。
// 段边界是分段实现最容易出错的地方（32 位算术溢出/绕回），必须专门覆盖。
func TestSegmentBoundary(t *testing.T) {
	const boundary = uint64(1) << 32 // 段边界
	b := NewFsBitmap("test", BitmapRaw, int64(boundary)+2048, 4096)

	b.Set(boundary - 1)
	b.Set(boundary)
	if !b.IsSet(boundary-1) || !b.IsSet(boundary) {
		t.Fatalf("Set across segment boundary failed")
	}
	if b.CountSet() != 2 {
		t.Fatalf("CountSet = %d, want 2", b.CountSet())
	}

	// 跨边界的 SetRange：[boundary-5, boundary+5)
	b.SetRange(boundary-5, 10)
	if b.CountSet() != 10 {
		t.Fatalf("CountSet = %d, want 10", b.CountSet())
	}
	for i := boundary - 5; i < boundary+5; i++ {
		if !b.IsSet(i) {
			t.Fatalf("bit %d should be set", i)
		}
	}
	if b.IsSet(boundary-6) || b.IsSet(boundary+5) {
		t.Fatalf("SetRange exceeded its range")
	}

	if got := b.nextSetBit(int64(boundary - 5)); got != int64(boundary-5) {
		t.Fatalf("nextSetBit = %d, want %d", got, boundary-5)
	}
	if got := b.nextClearBit(int64(boundary - 5)); got != int64(boundary+5) {
		t.Fatalf("nextClearBit = %d, want %d", got, boundary+5)
	}
	if got := b.nextSetBit(int64(boundary + 5)); got != b.Bits {
		t.Fatalf("nextSetBit = %d, want %d", got, b.Bits)
	}

	// 跨边界的 ClearRange：[boundary-2, boundary+2)
	b.ClearRange(boundary-2, 4)
	if b.CountSet() != 6 {
		t.Fatalf("CountSet = %d, want 6", b.CountSet())
	}
	if got := b.nextClearBit(int64(boundary - 5)); got != int64(boundary-2) {
		t.Fatalf("nextClearBit = %d, want %d", got, boundary-2)
	}
	if got := b.nextSetBit(int64(boundary - 2)); got != int64(boundary+2) {
		t.Fatalf("nextSetBit = %d, want %d", got, boundary+2)
	}

	// 恰好落在 Bits 上的块必须被忽略
	b.Set(uint64(b.Bits))
	if b.IsSet(uint64(b.Bits)) {
		t.Fatalf("bit at Bits must be ignored")
	}
}

func TestMirrorFs(t *testing.T) {
	const blockSize = 512
	const numBlocks = 2048 // 1 MiB

	dir := t.TempDir()
	originPath := filepath.Join(dir, "origin.img")
	targetPath := filepath.Join(dir, "target.img")

	// origin：第 i 个块填充为 byte(i)，便于逐块校验
	originData := make([]byte, numBlocks*blockSize)
	for i := 0; i < numBlocks; i++ {
		for j := 0; j < blockSize; j++ {
			originData[i*blockSize+j] = byte(i)
		}
	}
	if err := os.WriteFile(originPath, originData, 0644); err != nil {
		t.Fatalf("write origin: %v", err)
	}
	if err := os.WriteFile(targetPath, make([]byte, numBlocks*blockSize), 0644); err != nil {
		t.Fatalf("write target: %v", err)
	}

	b := NewFsBitmap("test", BitmapFromFS, numBlocks, blockSize)
	// 三段已使用区间，其中 [500, 1600) 长度 1100 > maxChunkBlocks(1024)，
	// 用于覆盖 MirrorFs 内部的分块读写循环
	b.SetRange(3, 4)
	b.SetRange(100, 1)
	b.SetRange(500, 1100)

	originFile, err := os.Open(originPath)
	if err != nil {
		t.Fatalf("open origin: %v", err)
	}
	defer originFile.Close()

	targetFile, err := os.OpenFile(targetPath, os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("open target: %v", err)
	}
	defer targetFile.Close()

	copied, err := b.MirrorFs(originFile, targetFile)
	if err != nil {
		t.Fatalf("MirrorFs: %v", err)
	}
	wantCopied := int64(4+1+1100) * blockSize
	if copied != wantCopied {
		t.Fatalf("copied = %d, want %d", copied, wantCopied)
	}

	// 逐块校验：置位块与 origin 一致，空闲块保持全 0
	result, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	for i := 0; i < numBlocks; i++ {
		block := result[i*blockSize : (i+1)*blockSize]
		if b.IsSet(uint64(i)) {
			if !bytes.Equal(block, originData[i*blockSize:(i+1)*blockSize]) {
				t.Fatalf("block %d not mirrored", i)
			}
		} else if !bytes.Equal(block, make([]byte, blockSize)) {
			t.Fatalf("free block %d was written", i)
		}
	}

	// origin 比位图声明的范围小：读到 EOF 后提前结束，不报错
	big := NewFsBitmap("test", BitmapFromFS, numBlocks*2, blockSize)
	big.SetAll()
	copied2, err := big.MirrorFs(originFile, targetFile)
	if err != nil {
		t.Fatalf("MirrorFs with short origin: %v", err)
	}
	if copied2 != numBlocks*blockSize {
		t.Fatalf("copied = %d, want %d (short origin must stop at EOF)", copied2, numBlocks*blockSize)
	}
}

// TestMemoryEfficiency 验证 RoaringBitmap 相对传统 []byte 位图的内存优势，
// 这是本次改造的核心目标。
func TestMemoryEfficiency(t *testing.T) {
	const bits = int64(1) << 30 // 固定位图需要 128 MiB

	// 密集场景：全部置位（Run-Length 压缩）
	dense := NewFsBitmap("test", BitmapRaw, bits, 4096)
	dense.SetAll()
	if dense.CountSet() != bits {
		t.Fatalf("CountSet = %d, want %d", dense.CountSet(), bits)
	}
	fixedSize := uint64(bits / 8)
	if dense.MemorySize() >= fixedSize/16 {
		t.Fatalf("dense bitmap uses %d bytes, fixed bitmap %d bytes: compression ratio too low",
			dense.MemorySize(), fixedSize)
	}
	t.Logf("dense: %d bits, roaring=%d bytes, fixed=%d bytes", bits, dense.MemorySize(), fixedSize)

	// 稀疏场景：少量分散的置位块
	sparse := NewFsBitmap("test", BitmapRaw, bits, 4096)
	for i := int64(0); i < 100000; i++ {
		sparse.Set(uint64(i * 1000))
	}
	if sparse.CountSet() != 100000 {
		t.Fatalf("CountSet = %d, want 100000", sparse.CountSet())
	}
	if sparse.MemorySize() >= fixedSize {
		t.Fatalf("sparse bitmap uses %d bytes >= fixed bitmap %d bytes", sparse.MemorySize(), fixedSize)
	}
	t.Logf("sparse: 100000 bits set, roaring=%d bytes, fixed=%d bytes", sparse.MemorySize(), fixedSize)
}
