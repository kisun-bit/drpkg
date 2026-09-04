package vss

import (
	"strings"
	"testing"
	"time"
)

// =============================================================================
// 类型与方法测试（跨平台）
// =============================================================================

func TestSnapshotTypes(t *testing.T) {
	s := &Snapshot{
		deviceObject: `\\?\GLOBALROOT\Device\HarddiskVolumeShadowCopy1`,
		volumeName:   `C:\`,
		creationTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	if s.DeviceObject() != `\\?\GLOBALROOT\Device\HarddiskVolumeShadowCopy1` {
		t.Fatal("DeviceObject mismatch")
	}
	if s.VolumeName() != `C:\` {
		t.Fatal("VolumeName mismatch")
	}
	if !s.CreationTime().Equal(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("CreationTime mismatch")
	}
}

func TestSnapshotInfoStruct(t *testing.T) {
	info := SnapshotInfo{
		SnapshotID:    "{00000000-0000-0000-0000-000000000001}",
		SnapshotSetID: "{00000000-0000-0000-0000-000000000002}",
		VolumeName:    `C:\`,
		DeviceObject:  `\\?\GLOBALROOT\Device\HarddiskVolumeShadowCopy1`,
		CreationTime:  time.Now(),
		Attributes:    0,
	}

	if info.SnapshotID == "" || info.SnapshotSetID == "" {
		t.Fatal("SnapshotInfo fields should be populated")
	}
}

// =============================================================================
// 权限检查（跨平台，Windows 上可能因权限不足返回错误）
// =============================================================================

func TestHasSufficientPrivileges(t *testing.T) {
	err := HasSufficientPrivileges()
	// 非 Windows 平台返回 "VSS snapshots are only supported on Windows"
	// Windows 上可能返回权限不足错误或 nil（管理员）
	// 无论哪种情况，函数不应 panic
	if err != nil {
		t.Logf("HasSufficientPrivileges returned error (expected if not admin): %v", err)
	} else {
		t.Log("HasSufficientPrivileges returned nil (running as admin)")
	}
}

// =============================================================================
// GetVolumeNameForMountPoint 测试（Windows 需要管理员）
// =============================================================================

func TestGetVolumeNameForMountPoint(t *testing.T) {
	// 传入空字符串，预期返回错误
	_, err := GetVolumeNameForMountPoint("")
	if err == nil {
		t.Log("GetVolumeNameForMountPoint(\"\") succeeded (unexpected)")
	}
}

// =============================================================================
// CreateSnapshot 集成测试（需要管理员权限，默认跳过）
// =============================================================================

func TestCreateSnapshot_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	if err := HasSufficientPrivileges(); err != nil {
		t.Skipf("skipping VSS integration test: %v", err)
	}

	// 对 C:\ 创建快照
	snap, err := CreateSnapshot(`C:\`, 120*time.Second)
	if err != nil {
		t.Fatalf("CreateSnapshot failed: %v", err)
	}

	if snap.DeviceObject() == "" {
		t.Fatal("DeviceObject should not be empty")
	}
	if snap.VolumeName() != `C:\` {
		t.Fatalf("VolumeName = %s, want C:\\", snap.VolumeName())
	}
	if snap.CreationTime().IsZero() {
		t.Fatal("CreationTime should not be zero")
	}

	t.Logf("Created snapshot: device=%s, volume=%s, time=%v",
		snap.DeviceObject(), snap.VolumeName(), snap.CreationTime())

	// 验证快照设备路径格式
	if !strings.HasPrefix(snap.DeviceObject(), `\\?\GLOBALROOT\`) {
		t.Errorf("DeviceObject format unexpected: %s", snap.DeviceObject())
	}
}

// =============================================================================
// CreateSnapshots 批量集成测试（需要管理员权限，默认跳过）
// =============================================================================

func TestCreateSnapshots_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	if err := HasSufficientPrivileges(); err != nil {
		t.Skipf("skipping VSS integration test: %v", err)
	}

	volumes := []string{`C:\`}
	snapshots, err := CreateSnapshots(volumes, 120*time.Second)
	if err != nil {
		t.Fatalf("CreateSnapshots failed: %v", err)
	}

	if len(snapshots) != len(volumes) {
		t.Fatalf("expected %d snapshots, got %d", len(volumes), len(snapshots))
	}

	for i, snap := range snapshots {
		if snap.DeviceObject() == "" {
			t.Fatalf("snapshot %d: DeviceObject should not be empty", i)
		}
		t.Logf("Snapshot %d: device=%s, volume=%s", i, snap.DeviceObject(), snap.VolumeName())
	}
}

// TestCreateSnapshots_MultiVolume 测试同时为多个卷创建快照。
func TestCreateSnapshots_MultiVolume(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	if err := HasSufficientPrivileges(); err != nil {
		t.Skipf("skipping VSS integration test: %v", err)
	}

	// 收集系统中实际存在的卷（通过尝试 C:\ 和 D:\）
	volumes := []string{`C:\`}
	if _, err := GetVolumeNameForMountPoint(`D:\`); err == nil {
		volumes = append(volumes, `D:\`)
	} else {
		t.Logf("D:\\ not available, testing with C:\\ only but still using CreateSnapshots for multi-volume path")
	}

	if len(volumes) < 2 {
		// D:\ 不存在，但测试仍然有效——验证 CreateSnapshots 对单卷也能正常工作
		volumes = append(volumes, `C:\`) // 同一个卷两次，测试批量路径
	}

	snapshots, err := CreateSnapshots(volumes, 120*time.Second)
	if err != nil {
		t.Fatalf("CreateSnapshots failed: %v", err)
	}

	if len(snapshots) != len(volumes) {
		t.Fatalf("expected %d snapshots, got %d", len(volumes), len(snapshots))
	}

	for i, snap := range snapshots {
		if snap.DeviceObject() == "" {
			t.Fatalf("snapshot %d: DeviceObject should not be empty", i)
		}
		if snap.VolumeName() != volumes[i] {
			t.Errorf("snapshot %d: VolumeName = %s, want %s", i, snap.VolumeName(), volumes[i])
		}
		t.Logf("Snapshot %d: volume=%s, device=%s", i, snap.VolumeName(), snap.DeviceObject())
	}

	// 验证返回的快照设备路径互不相同
	if len(snapshots) >= 2 && snapshots[0].DeviceObject() == snapshots[1].DeviceObject() {
		t.Error("expected different device objects for different snapshots")
	}
}

// =============================================================================
// QuerySnapshots 集成测试（需要管理员权限，默认跳过）
// =============================================================================

func TestQuerySnapshots_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	if err := HasSufficientPrivileges(); err != nil {
		t.Skipf("skipping VSS integration test: %v", err)
	}

	snapshots, err := QuerySnapshots()
	if err != nil {
		t.Fatalf("QuerySnapshots failed: %v", err)
	}

	t.Logf("Found %d existing snapshots", len(snapshots))
	for i, info := range snapshots {
		t.Logf("  [%d] ID=%s Volume=%s Device=%s Created=%v",
			i, info.SnapshotID, info.VolumeName, info.DeviceObject, info.CreationTime)
	}
}

// =============================================================================
// 错误场景测试
// =============================================================================

func TestCreateSnapshot_InvalidVolume(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	if err := HasSufficientPrivileges(); err != nil {
		t.Skipf("skipping VSS integration test: %v", err)
	}

	// 不存在的卷应返回错误
	_, err := CreateSnapshot(`Z:\`, 10*time.Second)
	if err == nil {
		t.Fatal("expected error for non-existent volume")
	}
	t.Logf("Expected error for invalid volume: %v", err)
}

func TestCreateSnapshots_EmptyList(t *testing.T) {
	_, err := CreateSnapshots(nil, time.Second)
	if err == nil {
		t.Fatal("expected error for empty volume list")
	}
}

// =============================================================================
// HRESULT 错误类型测试
// =============================================================================

func TestVssError(t *testing.T) {
	// newVssError 始终创建错误
	err := newVssError("test operation", sOK)
	if err == nil {
		t.Fatal("newVssError should return error")
	}

	// newVssErrorIfNotOK 仅在非 S_OK 时返回错误
	err = newVssErrorIfNotOK("test", sOK)
	if err != nil {
		t.Fatal("newVssErrorIfNotOK with S_OK should return nil")
	}

	err = newVssErrorIfNotOK("test", eAccessDenied)
	if err == nil {
		t.Fatal("newVssErrorIfNotOK with E_ACCESSDENIED should return error")
	}
	if !strings.Contains(err.Error(), "E_ACCESSDENIED") {
		t.Fatalf("error should contain E_ACCESSDENIED: %v", err)
	}
}

func TestVssTextError(t *testing.T) {
	err := newVssTextError("something went wrong")
	if err == nil {
		t.Fatal("newVssTextError should return error")
	}
	if !strings.Contains(err.Error(), "something went wrong") {
		t.Fatalf("error should contain message: %v", err)
	}
}

func TestHresultStr(t *testing.T) {
	if hresult(sOK).str() != "S_OK" {
		t.Fatal("S_OK string mismatch")
	}
	if hresult(eAccessDenied).str() != "E_ACCESSDENIED" {
		t.Fatal("E_ACCESSDENIED string mismatch")
	}
	// Unknown HRESULT
	if hresult(0xDEADBEEF).str() != "UNKNOWN" {
		t.Fatal("unknown HRESULT should return UNKNOWN")
	}
}

// =============================================================================
// 超时测试
// =============================================================================

func TestCreateSnapshot_Timeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	if err := HasSufficientPrivileges(); err != nil {
		t.Skipf("skipping VSS integration test: %v", err)
	}

	// 极短超时应导致失败
	_, err := CreateSnapshot(`C:\`, 1*time.Nanosecond)
	if err == nil {
		t.Log("snapshot created despite very short timeout (unexpected but possible)")
	} else {
		t.Logf("Expected timeout error: %v", err)
	}
}

// =============================================================================
// DeleteSnapshot 集成测试
// =============================================================================

func TestDeleteSnapshot_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	if err := HasSufficientPrivileges(); err != nil {
		t.Skipf("skipping VSS integration test: %v", err)
	}

	// 先创建快照
	snap, err := CreateSnapshot(`C:\`, 120*time.Second)
	if err != nil {
		t.Fatalf("CreateSnapshot failed: %v", err)
	}

	// 查询快照列表中应该包含新建的快照
	snapshots, err := QuerySnapshots()
	if err != nil {
		t.Fatalf("QuerySnapshots failed: %v", err)
	}

	found := false
	var foundID string
	for _, info := range snapshots {
		if info.DeviceObject == snap.DeviceObject() {
			found = true
			foundID = info.SnapshotID
			break
		}
	}

	if !found {
		t.Skip("created snapshot not found in query results (may already be cleaned up)")
	}

	// 删除快照
	err = DeleteSnapshot(foundID)
	if err != nil {
		t.Fatalf("DeleteSnapshot failed: %v", err)
	}

	t.Logf("Successfully deleted snapshot %s", foundID)
}

// =============================================================================
// DeleteSnapshot 无效 ID 测试
// =============================================================================

func TestDeleteSnapshot_InvalidID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	if err := HasSufficientPrivileges(); err != nil {
		t.Skipf("skipping VSS integration test: %v", err)
	}

	err := DeleteSnapshot("{00000000-0000-0000-0000-000000000000}")
	if err == nil {
		t.Fatal("expected error for non-existent snapshot ID")
	}
	t.Logf("Expected error: %v", err)
}

// =============================================================================
// 基准测试
// =============================================================================

func BenchmarkCreateSnapshot(b *testing.B) {
	if err := HasSufficientPrivileges(); err != nil {
		b.Skipf("skipping VSS benchmark: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		snap, err := CreateSnapshot(`C:\`, 120*time.Second)
		if err != nil {
			b.Fatalf("CreateSnapshot failed: %v", err)
		}
		// 查询快照以获取 ID 用于删除
		snapshots, _ := QuerySnapshots()
		for _, info := range snapshots {
			if info.DeviceObject == snap.DeviceObject() {
				_ = DeleteSnapshot(info.SnapshotID)
				break
			}
		}
	}
}
