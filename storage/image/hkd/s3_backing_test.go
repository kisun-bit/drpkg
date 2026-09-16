package hkd

import (
	"bytes"
	"testing"

	"github.com/kisun-bit/drpkg/storage/backend"
)

// s3Accessor 建立连接到真实 S3 服务的访问器；未配置 S3_ENDPOINT 时跳过。
//
// 与 newLocalFsAccessor 对应，用于在对象存储介质上验证 backing/commit/rebase
// 等依赖「读回已经写入的 Cluster」的路径，确认 S3 缓冲写语义下的一致性。
func s3Accessor(t *testing.T) backend.Accessor {
	t.Helper()
	cfg := s3ConfigFromEnv()
	if cfg == nil {
		t.Skip("S3_ENDPOINT not set; set S3_* env vars to run S3 integration test")
	}
	acc, err := backend.New(*cfg)
	if err != nil {
		t.Fatalf("backend.New: %v", err)
	}
	if err := acc.Online(); err != nil {
		t.Fatalf("Online: %v", err)
	}
	t.Cleanup(func() { _ = acc.Offline() })
	return acc
}

// newS3Opt 在 testOptions 基础上换一个唯一 DiskID，避免多个测试在同一
// bucket 中互相覆盖对象。
func newS3Opt(diskID string) Options {
	opt := testOptions()
	opt.DiskID = diskID
	return opt
}

// TestS3Backing 验证 overlay 未写入的 Cluster 回落到已持久化的 backing。
func TestS3Backing(t *testing.T) {
	acc := s3Accessor(t)

	baseOpt := newS3Opt("s3-bk-0001")
	base, err := Create(acc, baseOpt)
	if err != nil {
		t.Fatalf("Create base: %v", err)
	}
	defer base.Release()
	baseData := make([]byte, baseOpt.ClusterSize)
	fillPattern(baseData, 3)
	if err := base.WriteAt(0, baseData); err != nil {
		t.Fatalf("base WriteAt: %v", err)
	}
	if err := base.Close(); err != nil {
		t.Fatalf("base Close: %v", err)
	}

	overlayOpt := newS3Opt("s3-bk-0002")
	overlay, err := Create(acc, overlayOpt, base)
	if err != nil {
		t.Fatalf("Create overlay: %v", err)
	}
	defer overlay.Release()
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

// TestS3OverlayWriteRead 验证 overlay 覆盖写 cluster 0、cluster 1 回落 backing。
func TestS3OverlayWriteRead(t *testing.T) {
	acc := s3Accessor(t)

	baseOpt := newS3Opt("s3-ov-0001")
	base, err := Create(acc, baseOpt)
	if err != nil {
		t.Fatalf("Create base: %v", err)
	}
	defer base.Release()
	baseData := make([]byte, baseOpt.ClusterSize*2)
	fillPattern(baseData, 1)
	if err := base.WriteAt(0, baseData); err != nil {
		t.Fatalf("base WriteAt: %v", err)
	}
	if err := base.Close(); err != nil {
		t.Fatalf("base Close: %v", err)
	}

	overlayOpt := newS3Opt("s3-ov-0002")
	overlay, err := Create(acc, overlayOpt, base)
	if err != nil {
		t.Fatalf("Create overlay: %v", err)
	}
	defer overlay.Release()
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

// TestS3Commit 验证 backing 未 Close 时 overlay 提交：commit 期间 overlay
// 读自己的写入缓冲、backing 写回自己的缓冲并读回，全程不需依赖 OpenFile。
func TestS3Commit(t *testing.T) {
	acc := s3Accessor(t)

	baseOpt := newS3Opt("s3-cmt-0001")
	base, err := Create(acc, baseOpt)
	if err != nil {
		t.Fatalf("Create base: %v", err)
	}
	defer base.Release()
	baseData := make([]byte, baseOpt.ClusterSize)
	fillPattern(baseData, 1)
	if err := base.WriteAt(0, baseData); err != nil {
		t.Fatalf("base WriteAt: %v", err)
	}
	// 不 Close base，保持可写以便 commit。

	overlayOpt := newS3Opt("s3-cmt-0002")
	overlay, err := Create(acc, overlayOpt, base)
	if err != nil {
		t.Fatalf("Create overlay: %v", err)
	}
	defer overlay.Release()
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

	if overlay.index.IsWritten(0) {
		t.Fatalf("overlay cluster 0 should be empty after commit")
	}
}

// TestS3Rebase 验证 overlay 在 backing 间切换（rebase）后的读回落。
func TestS3Rebase(t *testing.T) {
	acc := s3Accessor(t)

	base1Opt := newS3Opt("s3-rb-0001")
	base1, err := Create(acc, base1Opt)
	if err != nil {
		t.Fatalf("Create base1: %v", err)
	}
	defer base1.Release()
	data1 := make([]byte, base1Opt.ClusterSize)
	fillPattern(data1, 1)
	if err := base1.WriteAt(0, data1); err != nil {
		t.Fatalf("base1 WriteAt: %v", err)
	}
	if err := base1.Close(); err != nil {
		t.Fatalf("base1 Close: %v", err)
	}

	base2Opt := newS3Opt("s3-rb-0002")
	base2, err := Create(acc, base2Opt)
	if err != nil {
		t.Fatalf("Create base2: %v", err)
	}
	defer base2.Release()
	data2 := make([]byte, base2Opt.ClusterSize)
	fillPattern(data2, 2)
	if err := base2.WriteAt(0, data2); err != nil {
		t.Fatalf("base2 WriteAt: %v", err)
	}
	if err := base2.Close(); err != nil {
		t.Fatalf("base2 Close: %v", err)
	}

	overlayOpt := newS3Opt("s3-rb-0003")
	overlay, err := Create(acc, overlayOpt, base1)
	if err != nil {
		t.Fatalf("Create overlay with base1: %v", err)
	}
	defer overlay.Release()

	buf := make([]byte, overlayOpt.ClusterSize)
	if _, err := overlay.ReadAt(0, buf); err != nil {
		t.Fatalf("ReadAt before rebase: %v", err)
	}
	if !bytes.Equal(buf, data1) {
		t.Fatalf("before rebase should read base1 data")
	}

	overlay.Rebase(base2)
	if _, err := overlay.ReadAt(0, buf); err != nil {
		t.Fatalf("ReadAt after rebase: %v", err)
	}
	if !bytes.Equal(buf, data2) {
		t.Fatalf("after rebase should read base2 data")
	}
}

// TestS3MultiLayerBacking 验证三层 backing 链（base -> mid -> top）的读回落。
func TestS3MultiLayerBacking(t *testing.T) {
	acc := s3Accessor(t)
	cs := testOptions().ClusterSize

	baseOpt := newS3Opt("s3-ml-0001")
	base, err := Create(acc, baseOpt)
	if err != nil {
		t.Fatalf("Create base: %v", err)
	}
	defer base.Release()
	baseData := make([]byte, cs*3)
	fillPattern(baseData, 1)
	if err := base.WriteAt(0, baseData); err != nil {
		t.Fatalf("base WriteAt: %v", err)
	}
	if err := base.Close(); err != nil {
		t.Fatalf("base Close: %v", err)
	}

	midOpt := newS3Opt("s3-ml-0002")
	mid, err := Create(acc, midOpt, base)
	if err != nil {
		t.Fatalf("Create mid: %v", err)
	}
	defer mid.Release()
	midData := make([]byte, cs)
	fillPattern(midData, 2)
	if err := mid.WriteAt(cs, midData); err != nil {
		t.Fatalf("mid WriteAt: %v", err)
	}
	if err := mid.Close(); err != nil {
		t.Fatalf("mid Close: %v", err)
	}

	topOpt := newS3Opt("s3-ml-0003")
	top, err := Create(acc, topOpt, mid)
	if err != nil {
		t.Fatalf("Create top: %v", err)
	}
	defer top.Release()
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

// TestS3EncryptOverlayCommit 验证加密 overlay 提交到加密 backing 的一致性。
func TestS3EncryptOverlayCommit(t *testing.T) {
	withTestKey(t)

	acc := s3Accessor(t)
	cs := testOptions().ClusterSize

	baseOpt := newS3Opt("s3-ec-0001")
	baseOpt.Encrypt = true
	baseOpt.Compress = true
	baseOpt.Check = true
	base, err := Create(acc, baseOpt)
	if err != nil {
		t.Fatalf("Create base: %v", err)
	}
	defer base.Release()
	baseData := make([]byte, cs)
	fillPattern(baseData, 1)
	if err := base.WriteAt(0, baseData); err != nil {
		t.Fatalf("base WriteAt: %v", err)
	}
	// 不 Close base，保持可写供 commit。

	overlayOpt := newS3Opt("s3-ec-0002")
	overlayOpt.Encrypt = true
	overlayOpt.Compress = true
	overlayOpt.Check = true
	overlay, err := Create(acc, overlayOpt, base)
	if err != nil {
		t.Fatalf("Create overlay: %v", err)
	}
	defer overlay.Release()
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
