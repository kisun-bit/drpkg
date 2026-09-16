package hkd

import (
	"bytes"
	"io"
	"testing"

	"github.com/kisun-bit/drpkg/storage/backend"
)

func testOptions() Options {
	return Options{
		DisasterSystemID: "sys00001",
		UserID:           "00000001",
		StorageMediaID:   "123e4567-e89b-12d3-a456-426614174000",
		PolicyID:         "123e4567-e89b-12d3-a456-426614174000",
		TaskID:           "123e4567-e89b-12d3-a456-426614174000",
		HostID:           "host0001",
		DiskID:           "disk0001",
		DiskSize:         4 << 20,
		LBASize:          512,
		PBASize:          4096,
		ClusterSize:      64 << 10,
	}
}

func newLocalFsAccessor(t *testing.T) backend.Accessor {
	t.Helper()
	acc, err := backend.New(backend.LocalFsConfig{SubPath: t.TempDir()})
	if err != nil {
		t.Fatalf("backend.New: %v", err)
	}
	if err := acc.Online(); err != nil {
		t.Fatalf("Online: %v", err)
	}
	t.Cleanup(func() { _ = acc.Offline() })
	return acc
}

func fillPattern(b []byte, seed byte) {
	for i := range b {
		b[i] = seed + byte(i%251)
	}
}

func TestIndexRoundTrip(t *testing.T) {
	idx := NewIndex(4<<20, 64<<10, 512, 4096)

	if idx.ClusterCount() != 64 {
		t.Fatalf("ClusterCount = %d, want 64", idx.ClusterCount())
	}
	if idx.ClusterSize() != 64<<10 {
		t.Fatalf("ClusterSize = %d, want %d", idx.ClusterSize(), 64<<10)
	}
	if idx.IsWritten(0) {
		t.Fatalf("fresh index should not mark cluster written")
	}

	idx.SetEntry(3, indexEntry{VolID: 1, Offset: 128, Size: 100})
	idx.SetEntry(63, indexEntry{VolID: 2, Offset: 4096, Size: 2048})

	buf := new(bytes.Buffer)
	if _, err := idx.writeTo(buf); err != nil {
		t.Fatalf("writeTo: %v", err)
	}

	got, err := readIndex(buf)
	if err != nil {
		t.Fatalf("readIndex: %v", err)
	}
	if got.ClusterCount() != idx.ClusterCount() {
		t.Fatalf("ClusterCount mismatch")
	}
	if got.EntryAt(3) != (indexEntry{VolID: 1, Offset: 128, Size: 100}) {
		t.Fatalf("entry 3 = %+v", got.EntryAt(3))
	}
	if got.EntryAt(63) != (indexEntry{VolID: 2, Offset: 4096, Size: 2048}) {
		t.Fatalf("entry 63 = %+v", got.EntryAt(63))
	}
	if got.IsWritten(3) != true || got.IsWritten(0) != false {
		t.Fatalf("IsWritten mismatch")
	}
}

func TestHkdHeaderRoundTrip(t *testing.T) {
	opt := testOptions()
	hdr := opt.toHeader()

	buf := new(bytes.Buffer)
	if err := writeHkdHeader(buf, hdr); err != nil {
		t.Fatalf("writeHkdHeader: %v", err)
	}
	got, err := readHkdHeader(buf)
	if err != nil {
		t.Fatalf("readHkdHeader: %v", err)
	}
	if got != hdr {
		t.Fatalf("header mismatch:\n got=%+v\nwant=%+v", got, hdr)
	}
}

func TestWriteAtReadAtFull(t *testing.T) {
	acc := newLocalFsAccessor(t)
	opt := testOptions()

	h, err := Create(acc, opt)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	want := make([]byte, opt.DiskSize)
	fillPattern(want, 7)
	if err := h.WriteAt(0, want); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	h2, err := Open(acc, opt)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer h2.Close()

	got := make([]byte, opt.DiskSize)
	if _, err := h2.ReadAt(0, got); err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("full read mismatch")
	}
}

func TestWriteAtPartial(t *testing.T) {
	acc := newLocalFsAccessor(t)
	opt := testOptions()
	cs := int(opt.ClusterSize)

	h, err := Create(acc, opt)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	part := []byte("0123456789")
	if err := h.WriteAt(100, part); err != nil {
		t.Fatalf("WriteAt partial: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	h2, err := Open(acc, opt)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer h2.Close()

	buf := make([]byte, cs)
	if _, err := h2.ReadAt(0, buf); err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	if !bytes.Equal(buf[100:110], part) {
		t.Fatalf("partial write region mismatch")
	}
	if !allZero(buf[0:100]) || !allZero(buf[110:]) {
		t.Fatalf("region outside partial write should be zero")
	}
}

func TestWriteAtOverwrite(t *testing.T) {
	acc := newLocalFsAccessor(t)
	opt := testOptions()

	h, err := Create(acc, opt)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	first := make([]byte, opt.ClusterSize)
	fillPattern(first, 1)
	second := make([]byte, opt.ClusterSize)
	fillPattern(second, 2)

	if err := h.WriteAt(0, first); err != nil {
		t.Fatalf("first WriteAt: %v", err)
	}
	if err := h.WriteAt(0, second); err != nil {
		t.Fatalf("overwrite WriteAt: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	h2, err := Open(acc, opt)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer h2.Close()

	buf := make([]byte, opt.ClusterSize)
	if _, err := h2.ReadAt(0, buf); err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	if !bytes.Equal(buf, second) {
		t.Fatalf("expected latest overwrite data")
	}
}

func TestCompact(t *testing.T) {
	acc := newLocalFsAccessor(t)
	opt := testOptions()
	opt.Compress = true // 压缩使覆盖写大小变化，产生垃圾

	h, err := Create(acc, opt)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	small := bytes.Repeat([]byte("ab"), int(opt.ClusterSize)/2)
	big := make([]byte, opt.ClusterSize)
	fillPattern(big, 9)

	if err := h.WriteAt(0, small); err != nil {
		t.Fatalf("WriteAt small: %v", err)
	}
	if err := h.WriteAt(0, big); err != nil {
		t.Fatalf("WriteAt big: %v", err)
	}
	if err := h.Compact(); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	h2, err := Open(acc, opt)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer h2.Close()

	buf := make([]byte, opt.ClusterSize)
	if _, err := h2.ReadAt(0, buf); err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	if !bytes.Equal(buf, big) {
		t.Fatalf("expected big data after compact")
	}
}

func TestBacking(t *testing.T) {
	acc := newLocalFsAccessor(t)

	baseOpt := testOptions()
	base, err := Create(acc, baseOpt)
	if err != nil {
		t.Fatalf("Create base: %v", err)
	}
	baseData := make([]byte, baseOpt.ClusterSize)
	fillPattern(baseData, 3)
	if err := base.WriteAt(0, baseData); err != nil {
		t.Fatalf("base WriteAt: %v", err)
	}
	if err := base.Close(); err != nil {
		t.Fatalf("base Close: %v", err)
	}

	overlayOpt := testOptions()
	overlayOpt.DiskID = "disk0002"
	overlay, err := Create(acc, overlayOpt, base)
	if err != nil {
		t.Fatalf("Create overlay: %v", err)
	}
	if err := overlay.Close(); err != nil {
		t.Fatalf("overlay Close: %v", err)
	}

	opened, err := Open(acc, overlayOpt, base)
	if err != nil {
		t.Fatalf("Open overlay: %v", err)
	}
	defer opened.Close()

	buf := make([]byte, overlayOpt.ClusterSize)
	if _, err := opened.ReadAt(0, buf); err != nil {
		t.Fatalf("ReadAt via backing: %v", err)
	}
	if !bytes.Equal(buf, baseData) {
		t.Fatalf("overlay should fall back to backing data")
	}
}

func allZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

func TestOverlayWriteRead(t *testing.T) {
	acc := newLocalFsAccessor(t)

	baseOpt := testOptions()
	base, err := Create(acc, baseOpt)
	if err != nil {
		t.Fatalf("Create base: %v", err)
	}
	baseData := make([]byte, baseOpt.ClusterSize*2)
	fillPattern(baseData, 1)
	if err := base.WriteAt(0, baseData); err != nil {
		t.Fatalf("base WriteAt: %v", err)
	}
	if err := base.Close(); err != nil {
		t.Fatalf("base Close: %v", err)
	}

	overlayOpt := testOptions()
	overlayOpt.DiskID = "disk0002"
	overlay, err := Create(acc, overlayOpt, base)
	if err != nil {
		t.Fatalf("Create overlay: %v", err)
	}
	overridden := make([]byte, overlayOpt.ClusterSize)
	fillPattern(overridden, 2)
	if err := overlay.WriteAt(0, overridden); err != nil {
		t.Fatalf("overlay WriteAt: %v", err)
	}
	if err := overlay.Close(); err != nil {
		t.Fatalf("overlay Close: %v", err)
	}

	opened, err := Open(acc, overlayOpt, base)
	if err != nil {
		t.Fatalf("Open overlay: %v", err)
	}
	defer opened.Close()

	// cluster 0 应取自 overlay（覆盖 backing）。
	buf0 := make([]byte, overlayOpt.ClusterSize)
	if _, err := opened.ReadAt(0, buf0); err != nil {
		t.Fatalf("ReadAt cluster 0: %v", err)
	}
	if !bytes.Equal(buf0, overridden) {
		t.Fatalf("cluster 0 should be overlay data")
	}
	// cluster 1 未在 overlay 写入，应回落 backing。
	buf1 := make([]byte, overlayOpt.ClusterSize)
	if _, err := opened.ReadAt(overlayOpt.ClusterSize, buf1); err != nil {
		t.Fatalf("ReadAt cluster 1: %v", err)
	}
	if !bytes.Equal(buf1, baseData[overlayOpt.ClusterSize:]) {
		t.Fatalf("cluster 1 should fall back to backing")
	}
}

func TestCommit(t *testing.T) {
	acc := newLocalFsAccessor(t)

	baseOpt := testOptions()
	base, err := Create(acc, baseOpt)
	if err != nil {
		t.Fatalf("Create base: %v", err)
	}
	baseData := make([]byte, baseOpt.ClusterSize)
	fillPattern(baseData, 1)
	if err := base.WriteAt(0, baseData); err != nil {
		t.Fatalf("base WriteAt: %v", err)
	}
	// 不 Close base，保持可写以便 commit。

	overlayOpt := testOptions()
	overlayOpt.DiskID = "disk0002"
	overlay, err := Create(acc, overlayOpt, base)
	if err != nil {
		t.Fatalf("Create overlay: %v", err)
	}
	overridden := make([]byte, overlayOpt.ClusterSize)
	fillPattern(overridden, 2)
	if err := overlay.WriteAt(0, overridden); err != nil {
		t.Fatalf("overlay WriteAt: %v", err)
	}

	if err := overlay.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// backing（base）现在应读到 overlay 提交的数据。
	buf := make([]byte, baseOpt.ClusterSize)
	if _, err := base.ReadAt(0, buf); err != nil {
		t.Fatalf("base ReadAt after commit: %v", err)
	}
	if !bytes.Equal(buf, overridden) {
		t.Fatalf("base should have committed overlay data")
	}

	// 提交后 overlay 清空，读应回落 base。
	if overlay.ClusterCount() != baseOpt.DiskSize/baseOpt.ClusterSize {
		t.Fatalf("overlay ClusterCount = %d", overlay.ClusterCount())
	}
	if overlay.index.IsWritten(0) {
		t.Fatalf("overlay cluster 0 should be empty after commit")
	}

	if err := base.Close(); err != nil {
		t.Fatalf("base Close: %v", err)
	}
}

func TestRelease(t *testing.T) {
	acc := newLocalFsAccessor(t)
	opt := testOptions()

	h, err := Create(acc, opt)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	data := make([]byte, opt.ClusterSize)
	fillPattern(data, 5)
	if err := h.WriteAt(0, data); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if err := h.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	// 文件已删除，Open 应失败。
	if _, err := Open(acc, opt); err == nil {
		t.Fatalf("Open after Release should fail")
	}
}

func TestRebase(t *testing.T) {
	acc := newLocalFsAccessor(t)

	base1Opt := testOptions()
	base1, err := Create(acc, base1Opt)
	if err != nil {
		t.Fatalf("Create base1: %v", err)
	}
	data1 := make([]byte, base1Opt.ClusterSize)
	fillPattern(data1, 1)
	if err := base1.WriteAt(0, data1); err != nil {
		t.Fatalf("base1 WriteAt: %v", err)
	}
	if err := base1.Close(); err != nil {
		t.Fatalf("base1 Close: %v", err)
	}

	base2Opt := testOptions()
	base2Opt.DiskID = "disk0002"
	base2, err := Create(acc, base2Opt)
	if err != nil {
		t.Fatalf("Create base2: %v", err)
	}
	data2 := make([]byte, base2Opt.ClusterSize)
	fillPattern(data2, 2)
	if err := base2.WriteAt(0, data2); err != nil {
		t.Fatalf("base2 WriteAt: %v", err)
	}
	if err := base2.Close(); err != nil {
		t.Fatalf("base2 Close: %v", err)
	}

	overlayOpt := testOptions()
	overlayOpt.DiskID = "disk0003"
	overlay, err := Create(acc, overlayOpt, base1)
	if err != nil {
		t.Fatalf("Create overlay with base1: %v", err)
	}
	defer overlay.Close()

	buf := make([]byte, overlayOpt.ClusterSize)
	if _, err := overlay.ReadAt(0, buf); err != nil {
		t.Fatalf("ReadAt before rebase: %v", err)
	}
	if !bytes.Equal(buf, data1) {
		t.Fatalf("before rebase should read base1 data")
	}

	// rebase 到 base2。
	overlay.Rebase(base2)
	if _, err := overlay.ReadAt(0, buf); err != nil {
		t.Fatalf("ReadAt after rebase: %v", err)
	}
	if !bytes.Equal(buf, data2) {
		t.Fatalf("after rebase should read base2 data")
	}
}

func TestCompactDisabled(t *testing.T) {
	acc := newLocalFsAccessor(t)
	opt := testOptions()
	opt.Compress = true
	// Compact 默认 false：覆盖写应追加（产生垃圾），Compact() 为 no-op。

	h, err := Create(acc, opt)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	small := bytes.Repeat([]byte("ab"), int(opt.ClusterSize)/2)
	big := make([]byte, opt.ClusterSize)
	fillPattern(big, 9)

	if err := h.WriteAt(0, small); err != nil {
		t.Fatalf("WriteAt small: %v", err)
	}
	off1 := h.index.EntryAt(0)
	if err := h.WriteAt(0, big); err != nil {
		t.Fatalf("WriteAt big: %v", err)
	}
	off2 := h.index.EntryAt(0)

	// 不开 Compact 时，覆盖写应追加到新位置（Offset 变化）。
	if off1.Offset == off2.Offset {
		t.Fatalf("append-only mode should write to a new offset, got same %d", off1.Offset)
	}
	// Compact() 应为 no-op。
	if err := h.Compact(); err != nil {
		t.Fatalf("Compact no-op: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	h2, err := Open(acc, opt)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer h2.Close()
	buf := make([]byte, opt.ClusterSize)
	if _, err := h2.ReadAt(0, buf); err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	if !bytes.Equal(buf, big) {
		t.Fatalf("expected big data")
	}
}

func make32ByteKey() []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = byte(i)
	}
	return k
}

// TestWriteAtReadAtEncrypt 验证加密 + 压缩 + 校验全开时的完整读写一致性。
func TestWriteAtReadAtEncrypt(t *testing.T) {
	if err := SetEncryptionKey(make32ByteKey()); err != nil {
		t.Fatalf("SetEncryptionKey: %v", err)
	}
	t.Cleanup(func() { encryptionKey = nil })

	acc := newLocalFsAccessor(t)
	opt := testOptions()
	opt.Encrypt = true
	opt.Compress = true
	opt.Check = true

	h, err := Create(acc, opt)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// 可压缩数据，验证压缩 + 加密 + CRC 组合路径。
	data := make([]byte, opt.DiskSize)
	for i := range data {
		data[i] = byte(i % 251)
	}
	if err := h.WriteAt(0, data); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	h2, err := Open(acc, opt)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer h2.Close()

	got := make([]byte, opt.DiskSize)
	if _, err := h2.ReadAt(0, got); err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("encrypt+compress+check round-trip mismatch")
	}
}

// TestCheckDetectCorruption 验证 CRC32C 校验能检测到落盘数据被篡改。
func TestCheckDetectCorruption(t *testing.T) {
	acc := newLocalFsAccessor(t)
	opt := testOptions()
	opt.Check = true

	h, err := Create(acc, opt)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	data := make([]byte, opt.ClusterSize)
	fillPattern(data, 6)
	if err := h.WriteAt(0, data); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// 篡改 cluster 0 落盘 payload 的首字节。
	entry := h.index.EntryAt(0)
	if entry.Size == 0 {
		t.Fatalf("cluster 0 should be written")
	}
	volPath := h.joinBase(h.volName(entry.VolID))
	f, err := acc.CreateFile(volPath)
	if err != nil {
		t.Fatalf("open vol for tamper: %v", err)
	}
	if _, err := f.Seek(int64(entry.Offset)+ClusterHeaderSize, io.SeekStart); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	if _, err := f.Write([]byte{0xFF}); err != nil {
		t.Fatalf("tamper Write: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// 重新打开读取，CRC 校验应失败。
	h2, err := Open(acc, opt)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer h2.Close()
	buf := make([]byte, opt.ClusterSize)
	if _, err := h2.ReadAt(0, buf); err == nil {
		t.Fatalf("expected CRC mismatch after tampecting payload")
	}
}

// TestWriteAtCrossCluster 验证一次写入跨越两个 Cluster 边界的数据一致性。
func TestWriteAtCrossCluster(t *testing.T) {
	acc := newLocalFsAccessor(t)
	opt := testOptions()
	cs := int(opt.ClusterSize)

	h, err := Create(acc, opt)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// 写 [cs-100, cs+100)，跨越 cluster 0 与 cluster 1。
	data := make([]byte, 200)
	fillPattern(data, 4)
	if err := h.WriteAt(uint64(cs-100), data); err != nil {
		t.Fatalf("WriteAt cross-cluster: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	h2, err := Open(acc, opt)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer h2.Close()

	got := make([]byte, 200)
	if _, err := h2.ReadAt(uint64(cs-100), got); err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("cross-cluster data mismatch")
	}

	// 边界之外的数据应保持零。
	buf := make([]byte, cs)
	if _, err := h2.ReadAt(0, buf); err != nil {
		t.Fatalf("ReadAt cluster 0: %v", err)
	}
	if !allZero(buf[:cs-100]) {
		t.Fatalf("region before write should be zero")
	}
	if !bytes.Equal(buf[cs-100:], data[:100]) {
		t.Fatalf("cluster 0 tail mismatch")
	}
}

// TestLastClusterPartial 验证磁盘大小不是 ClusterSize 整数倍时最后不满 Cluster 的一致性。
func TestLastClusterPartial(t *testing.T) {
	acc := newLocalFsAccessor(t)
	opt := testOptions()
	opt.DiskSize = opt.ClusterSize*3 + 1000 // 3 个满 Cluster + 1000 字节

	h, err := Create(acc, opt)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	data := make([]byte, opt.DiskSize)
	fillPattern(data, 8)
	if err := h.WriteAt(0, data); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	h2, err := Open(acc, opt)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer h2.Close()

	got := make([]byte, opt.DiskSize)
	if _, err := h2.ReadAt(0, got); err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("partial last cluster data mismatch")
	}
}

// TestReadAtBounds 验证越过磁盘末尾的读取语义。
func TestReadAtBounds(t *testing.T) {
	acc := newLocalFsAccessor(t)
	opt := testOptions()

	h, err := Create(acc, opt)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	data := make([]byte, opt.DiskSize)
	fillPattern(data, 3)
	if err := h.WriteAt(0, data); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	h2, err := Open(acc, opt)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer h2.Close()

	// offset == DiskSize → EOF。
	buf := make([]byte, 10)
	if n, err := h2.ReadAt(opt.DiskSize, buf); n != 0 || err != io.EOF {
		t.Fatalf("ReadAt(DiskSize) = (%d, %v), want (0, io.EOF)", n, err)
	}
	// offset > DiskSize → EOF。
	if n, err := h2.ReadAt(opt.DiskSize+100, buf); n != 0 || err != io.EOF {
		t.Fatalf("ReadAt(past end) = (%d, %v), want (0, io.EOF)", n, err)
	}
	// offset + len > DiskSize → 读部分 + EOF。
	buf2 := make([]byte, 200)
	n, err := h2.ReadAt(opt.DiskSize-100, buf2)
	if n != 100 || err != io.EOF {
		t.Fatalf("ReadAt(tail) = (%d, %v), want (100, io.EOF)", n, err)
	}
	if !bytes.Equal(buf2[:100], data[opt.DiskSize-100:]) {
		t.Fatalf("tail data mismatch")
	}
}

// TestBackingReadOnly 验证 overlay 的 backing 层成为只读，不能再写入。
func TestBackingReadOnly(t *testing.T) {
	acc := newLocalFsAccessor(t)

	baseOpt := testOptions()
	base, err := Create(acc, baseOpt)
	if err != nil {
		t.Fatalf("Create base: %v", err)
	}
	defer base.Close()

	overlayOpt := testOptions()
	overlayOpt.DiskID = "disk0002"
	overlay, err := Create(acc, overlayOpt, base)
	if err != nil {
		t.Fatalf("Create overlay: %v", err)
	}
	defer overlay.Close()

	data := make([]byte, baseOpt.ClusterSize)
	if err := base.WriteAt(0, data); err == nil {
		t.Fatalf("backing should be read-only after being used as backing")
	}
}

// TestWriteAfterCompact 验证 Compact 之后仍可继续写入且状态一致。
func TestWriteAfterCompact(t *testing.T) {
	acc := newLocalFsAccessor(t)
	opt := testOptions()
	opt.Compress = true
	opt.Compact = true

	h, err := Create(acc, opt)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	first := make([]byte, opt.ClusterSize)
	fillPattern(first, 1)
	second := make([]byte, opt.ClusterSize)
	fillPattern(second, 2)

	if err := h.WriteAt(0, first); err != nil {
		t.Fatalf("first WriteAt: %v", err)
	}
	if err := h.WriteAt(0, second); err != nil {
		t.Fatalf("overwrite WriteAt: %v", err)
	}
	if err := h.Compact(); err != nil {
		t.Fatalf("Compact: %v", err)
	}

	// Compact 后继续写入另一个 Cluster。
	third := make([]byte, opt.ClusterSize)
	fillPattern(third, 3)
	if err := h.WriteAt(opt.ClusterSize, third); err != nil {
		t.Fatalf("WriteAt after Compact: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	h2, err := Open(acc, opt)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer h2.Close()

	buf := make([]byte, opt.ClusterSize)
	if _, err := h2.ReadAt(0, buf); err != nil {
		t.Fatalf("ReadAt cluster 0: %v", err)
	}
	if !bytes.Equal(buf, second) {
		t.Fatalf("cluster 0 should be second-after-compact")
	}
	if _, err := h2.ReadAt(opt.ClusterSize, buf); err != nil {
		t.Fatalf("ReadAt cluster 1: %v", err)
	}
	if !bytes.Equal(buf, third) {
		t.Fatalf("cluster 1 should be third")
	}
}

// TestWriteAtBounds 验证写越界的边界处理。
func TestWriteAtBounds(t *testing.T) {
	acc := newLocalFsAccessor(t)
	opt := testOptions()

	h, err := Create(acc, opt)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer h.Close()

	// offset >= DiskSize 应报错。
	if err := h.WriteAt(opt.DiskSize, []byte("x")); err == nil {
		t.Fatalf("WriteAt at DiskSize should fail")
	}
	// offset 有效但 data 越过磁盘末尾应报错。
	data := make([]byte, 200)
	if err := h.WriteAt(opt.DiskSize-100, data); err == nil {
		t.Fatalf("WriteAt past end should fail")
	}
	// 合法边界（正好到磁盘末尾）应成功。
	ok := make([]byte, 100)
	if err := h.WriteAt(opt.DiskSize-100, ok); err != nil {
		t.Fatalf("WriteAt at exact boundary: %v", err)
	}
}

// TestMultiLayerBacking 验证三层 backing 链（base -> mid -> top）的读回落。
func TestMultiLayerBacking(t *testing.T) {
	acc := newLocalFsAccessor(t)
	cs := testOptions().ClusterSize

	baseOpt := testOptions()
	base, err := Create(acc, baseOpt)
	if err != nil {
		t.Fatalf("Create base: %v", err)
	}
	baseData := make([]byte, cs*3)
	fillPattern(baseData, 1)
	if err := base.WriteAt(0, baseData); err != nil {
		t.Fatalf("base WriteAt: %v", err)
	}
	if err := base.Close(); err != nil {
		t.Fatalf("base Close: %v", err)
	}

	midOpt := testOptions()
	midOpt.DiskID = "disk0002"
	mid, err := Create(acc, midOpt, base)
	if err != nil {
		t.Fatalf("Create mid: %v", err)
	}
	midData := make([]byte, cs)
	fillPattern(midData, 2)
	if err := mid.WriteAt(cs, midData); err != nil {
		t.Fatalf("mid WriteAt: %v", err)
	}
	if err := mid.Close(); err != nil {
		t.Fatalf("mid Close: %v", err)
	}

	topOpt := testOptions()
	topOpt.DiskID = "disk0003"
	top, err := Create(acc, topOpt, mid)
	if err != nil {
		t.Fatalf("Create top: %v", err)
	}
	topData := make([]byte, cs)
	fillPattern(topData, 3)
	if err := top.WriteAt(cs*2, topData); err != nil {
		t.Fatalf("top WriteAt: %v", err)
	}
	if err := top.Close(); err != nil {
		t.Fatalf("top Close: %v", err)
	}

	opened, err := Open(acc, topOpt, mid)
	if err != nil {
		t.Fatalf("Open top: %v", err)
	}
	defer opened.Close()

	buf0 := make([]byte, cs)
	if _, err := opened.ReadAt(0, buf0); err != nil {
		t.Fatalf("ReadAt cluster 0: %v", err)
	}
	if !bytes.Equal(buf0, baseData[0:cs]) {
		t.Fatalf("cluster 0 should come from base")
	}
	buf1 := make([]byte, cs)
	if _, err := opened.ReadAt(cs, buf1); err != nil {
		t.Fatalf("ReadAt cluster 1: %v", err)
	}
	if !bytes.Equal(buf1, midData) {
		t.Fatalf("cluster 1 should come from mid")
	}
	buf2 := make([]byte, cs)
	if _, err := opened.ReadAt(cs*2, buf2); err != nil {
		t.Fatalf("ReadAt cluster 2: %v", err)
	}
	if !bytes.Equal(buf2, topData) {
		t.Fatalf("cluster 2 should come from top")
	}
}

// TestEncryptOverlayCommit 验证加密 overlay 提交到加密 backing 的一致性。
func TestEncryptOverlayCommit(t *testing.T) {
	if err := SetEncryptionKey(make32ByteKey()); err != nil {
		t.Fatalf("SetEncryptionKey: %v", err)
	}
	t.Cleanup(func() { encryptionKey = nil })

	acc := newLocalFsAccessor(t)
	cs := testOptions().ClusterSize

	baseOpt := testOptions()
	baseOpt.Encrypt = true
	baseOpt.Compress = true
	baseOpt.Check = true
	base, err := Create(acc, baseOpt)
	if err != nil {
		t.Fatalf("Create base: %v", err)
	}
	defer base.Close()
	baseData := make([]byte, cs)
	fillPattern(baseData, 1)
	if err := base.WriteAt(0, baseData); err != nil {
		t.Fatalf("base WriteAt: %v", err)
	}
	// 保持 base 打开（不 Close），供 commit。

	overlayOpt := testOptions()
	overlayOpt.DiskID = "disk0002"
	overlayOpt.Encrypt = true
	overlayOpt.Compress = true
	overlayOpt.Check = true
	overlay, err := Create(acc, overlayOpt, base)
	if err != nil {
		t.Fatalf("Create overlay: %v", err)
	}
	overData := make([]byte, cs)
	fillPattern(overData, 2)
	if err := overlay.WriteAt(0, overData); err != nil {
		t.Fatalf("overlay WriteAt: %v", err)
	}
	if err := overlay.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	buf := make([]byte, cs)
	if _, err := base.ReadAt(0, buf); err != nil {
		t.Fatalf("base ReadAt after commit: %v", err)
	}
	if !bytes.Equal(buf, overData) {
		t.Fatalf("base should have encrypted overlay data committed")
	}
}

// TestEncryptRebase 验证加密 backing 下 rebase 后的读回落。
func TestEncryptRebase(t *testing.T) {
	if err := SetEncryptionKey(make32ByteKey()); err != nil {
		t.Fatalf("SetEncryptionKey: %v", err)
	}
	t.Cleanup(func() { encryptionKey = nil })

	acc := newLocalFsAccessor(t)
	cs := testOptions().ClusterSize

	newBase := func(diskID string, seed byte) *HKD {
		opt := testOptions()
		opt.DiskID = diskID
		opt.Encrypt = true
		opt.Check = true
		b, err := Create(acc, opt)
		if err != nil {
			t.Fatalf("Create base %s: %v", diskID, err)
		}
		data := make([]byte, cs)
		fillPattern(data, seed)
		if err := b.WriteAt(0, data); err != nil {
			t.Fatalf("base %s WriteAt: %v", diskID, err)
		}
		if err := b.Close(); err != nil {
			t.Fatalf("base %s Close: %v", diskID, err)
		}
		return b
	}

	base1 := newBase("disk0001", 1)
	base2 := newBase("disk0002", 2)

	overlayOpt := testOptions()
	overlayOpt.DiskID = "disk0003"
	overlayOpt.Encrypt = true
	overlayOpt.Check = true
	overlay, err := Create(acc, overlayOpt, base1)
	if err != nil {
		t.Fatalf("Create overlay: %v", err)
	}
	defer overlay.Close()

	buf := make([]byte, cs)
	if _, err := overlay.ReadAt(0, buf); err != nil {
		t.Fatalf("ReadAt before rebase: %v", err)
	}
	want1 := make([]byte, cs)
	fillPattern(want1, 1)
	if !bytes.Equal(buf, want1) {
		t.Fatalf("before rebase should read base1")
	}

	overlay.Rebase(base2)
	if _, err := overlay.ReadAt(0, buf); err != nil {
		t.Fatalf("ReadAt after rebase: %v", err)
	}
	want2 := make([]byte, cs)
	fillPattern(want2, 2)
	if !bytes.Equal(buf, want2) {
		t.Fatalf("after rebase should read base2")
	}
}
