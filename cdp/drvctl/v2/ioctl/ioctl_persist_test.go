package ioctl

import (
	"testing"

	biotrkmeta "github.com/kisun-bit/drpkg/cdp/drvctl/v2/meta"
)

// ============================================================================
// StartTaskRequest 序列化往返（不依赖注册表/文件系统）
// ============================================================================

func TestStartTaskRequestPersistRoundTrip(t *testing.T) {
	orig := &StartTaskRequest{
		MetadataFile:   [512]byte{},
		MetadataOffset: 4096,
		MetadataExtents: []biotrkmeta.DiskExtent{
			makeDiskExtentTest("disk0", 0, 1048576),
			makeDiskExtentTest("disk1", 2048, 4096),
		},
	}
	copy(orig.MetadataFile[:], `\\.\PHYSICALDRIVE0`)
	orig.MetadataExtentsLen = uint32(len(orig.MetadataExtents))

	// 1. 打包
	packed, err := pack(orig)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}

	// 2. 解包
	var decoded StartTaskRequest
	if err := unpack(packed, &decoded); err != nil {
		t.Fatalf("unpack: %v", err)
	}

	// 3. 验证
	if decoded.MetadataOffset != orig.MetadataOffset {
		t.Errorf("MetadataOffset: got %d, want %d", decoded.MetadataOffset, orig.MetadataOffset)
	}
	if decoded.MetadataExtentsLen != orig.MetadataExtentsLen {
		t.Errorf("MetadataExtentsLen: got %d, want %d", decoded.MetadataExtentsLen, orig.MetadataExtentsLen)
	}
	if len(decoded.MetadataExtents) != len(orig.MetadataExtents) {
		t.Fatalf("MetadataExtents count: got %d, want %d", len(decoded.MetadataExtents), len(orig.MetadataExtents))
	}
	for i := range orig.MetadataExtents {
		if decoded.MetadataExtents[i].Start != orig.MetadataExtents[i].Start {
			t.Errorf("MetadataExtents[%d].Start mismatch", i)
		}
	}
}

// ============================================================================
// PersistStartRequest + ReadPersistRequest 往返（需管理员权限 / 合适环境）
// ============================================================================

func TestPersistReadRoundTrip(t *testing.T) {
	if !persistAvailable() {
		t.Skip("persist not available (need admin rights or Linux)")
	}

	orig := &StartTaskRequest{
		MetadataFile:   [512]byte{},
		MetadataOffset: 65536,
		MetadataExtents: []biotrkmeta.DiskExtent{
			makeDiskExtentTest("persist-disk", 0, 4096),
		},
	}
	copy(orig.MetadataFile[:], `\\.\PHYSICALDRIVE0`)
	orig.MetadataExtentsLen = uint32(len(orig.MetadataExtents))

	// 写入
	if err := PersistStartRequest(orig); err != nil {
		t.Fatalf("PersistStartRequest: %v", err)
	}

	// 读取
	decoded, err := ReadPersistRequest()
	if err != nil {
		// 清理
		RemovePersist()
		t.Fatalf("ReadPersistRequest: %v", err)
	}

	// 验证
	if decoded.MetadataOffset != orig.MetadataOffset {
		t.Errorf("MetadataOffset: got %d, want %d", decoded.MetadataOffset, orig.MetadataOffset)
	}
	if len(decoded.MetadataExtents) != len(orig.MetadataExtents) {
		t.Errorf("MetadataExtents count: got %d, want %d", len(decoded.MetadataExtents), len(orig.MetadataExtents))
	}

	// 清理
	if err := RemovePersist(); err != nil {
		t.Errorf("RemovePersist: %v", err)
	}
}

func TestRemovePersistCleansUp(t *testing.T) {
	if !persistAvailable() {
		t.Skip("persist not available (need admin rights or Linux)")
	}

	req := &StartTaskRequest{
		MetadataFile:   [512]byte{},
		MetadataOffset: 0,
	}
	copy(req.MetadataFile[:], "test")

	if err := PersistStartRequest(req); err != nil {
		t.Fatalf("PersistStartRequest: %v", err)
	}

	if err := RemovePersist(); err != nil {
		t.Fatalf("RemovePersist: %v", err)
	}

	// 读取应失败
	_, err := ReadPersistRequest()
	if err == nil {
		t.Error("ReadPersistRequest after RemovePersist should fail")
	}
}

func TestReadPersistRequestNotFound(t *testing.T) {
	if !persistAvailable() {
		t.Skip("persist not available (need admin rights or Linux)")
	}

	// 确保之前的状态已清理
	RemovePersist()

	_, err := ReadPersistRequest()
	if err == nil {
		t.Error("ReadPersistRequest should fail when no persist exists")
	}
}

// ============================================================================
// persistAvailable 检查持久化环境是否可用
// ============================================================================

// persistAvailable 检查持久化功能是否可用。
//
// Windows：需要管理员权限才能写入 HKLM。通过尝试打开注册表键来判断。
// Linux：检查 /etc/biotrk 目录是否存在（或可创建）。
func persistAvailable() bool {
	// 简单检查：尝试写入并立即清理
	req := &StartTaskRequest{
		MetadataFile:   [512]byte{},
		MetadataOffset: 0,
	}
	copy(req.MetadataFile[:], "probe")

	if err := PersistStartRequest(req); err != nil {
		return false
	}
	RemovePersist()
	return true
}

// ============================================================================
// 辅助
// ============================================================================

func makeDiskExtentTest(diskID string, start, size uint64) biotrkmeta.DiskExtent {
	var dkID biotrkmeta.DiskID
	copy(dkID.ID[:], diskID)
	return biotrkmeta.DiskExtent{DiskID: dkID, Start: start, Size: size}
}

// ============================================================================
// 零值序列化
// ============================================================================

func TestPersistZeroValueRequest(t *testing.T) {
	// 零值 StartTaskRequest 也能正常序列化
	req := &StartTaskRequest{}
	packed, err := pack(req)
	if err != nil {
		t.Fatalf("pack zero: %v", err)
	}

	var decoded StartTaskRequest
	if err := unpack(packed, &decoded); err != nil {
		t.Fatalf("unpack zero: %v", err)
	}

	if decoded.MetadataOffset != 0 {
		t.Error("zero MetadataOffset should be 0")
	}
	if decoded.MetadataExtentsLen != 0 {
		t.Error("zero MetadataExtentsLen should be 0")
	}
	if len(decoded.MetadataExtents) != 0 {
		t.Error("zero MetadataExtents should be empty")
	}
}

// ============================================================================
// 持久化数据完整性验证
// ============================================================================

func TestPersistLargeRequest(t *testing.T) {
	// 大量 MetadataExtents 的序列化
	extents := make([]biotrkmeta.DiskExtent, 100)
	for i := range extents {
		var dkID biotrkmeta.DiskID
		copy(dkID.ID[:], "disk")
		extents[i] = biotrkmeta.DiskExtent{
			DiskID: dkID,
			Start:  uint64(i * 4096),
			Size:   4096,
		}
	}

	orig := &StartTaskRequest{
		MetadataFile:       [512]byte{},
		MetadataOffset:     8192,
		MetadataExtents:    extents,
		MetadataExtentsLen: uint32(len(extents)),
	}

	packed, err := pack(orig)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}

	var decoded StartTaskRequest
	if err := unpack(packed, &decoded); err != nil {
		t.Fatalf("unpack: %v", err)
	}

	if decoded.MetadataExtentsLen != 100 {
		t.Errorf("count: got %d, want 100", decoded.MetadataExtentsLen)
	}
	if len(decoded.MetadataExtents) != 100 {
		t.Errorf("len: got %d, want 100", len(decoded.MetadataExtents))
	}
	// 验证首尾元素
	if decoded.MetadataExtents[0].Start != 0 {
		t.Error("first.Start mismatch")
	}
	if decoded.MetadataExtents[99].Start != 99*4096 {
		t.Error("last.Start mismatch")
	}
}
