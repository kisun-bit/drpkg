package xutil

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
)

// ============================================================================
// normalizeVolumePath
// ============================================================================

func TestNormalizeVolumePath(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{`D:`, `\\.\D:`, true},
		{`D:\`, `\\.\D:`, true},
		{`\\.\D:`, `\\.\D:`, true},
		{"", "", false},
		{`C:`, `\\.\C:`, true},
	}

	for _, tt := range tests {
		got, err := normalizeVolumePath(tt.in)
		if tt.ok && err != nil {
			t.Errorf("normalizeVolumePath(%q) unexpected error: %v", tt.in, err)
		}
		if !tt.ok && err == nil {
			t.Errorf("normalizeVolumePath(%q) expected error, got %q", tt.in, got)
		}
		if tt.ok && got != tt.want {
			t.Errorf("normalizeVolumePath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// ============================================================================
// getVolumeDiskExtents (需管理员)
// ============================================================================

func TestGetVolumeDiskExtents_Drive(t *testing.T) {
	if !isAdmin() {
		t.Skip("need admin rights")
	}

	volPath := `\\.\D:`
	hVol, err := openVolumeRO(volPath)
	if err != nil {
		t.Fatalf("open %s: %v", volPath, err)
	}
	defer closeVolume(hVol)

	extents, err := getVolumeDiskExtents(hVol)
	if err != nil {
		t.Fatalf("getVolumeDiskExtents: %v", err)
	}

	t.Logf("D: has %d disk extent(s):", len(extents))
	var total int64
	for i, e := range extents {
		t.Logf("  [%d] Disk=%d Offset=%d Length=%d (%.2f GiB)",
			i, e.DiskNumber, e.StartingOffset, e.ExtentLength,
			float64(e.ExtentLength)/(1024*1024*1024))
		total += e.ExtentLength
	}
	t.Logf("Total size: %d bytes (%.2f GiB)", total, float64(total)/(1024*1024*1024))

	if len(extents) == 0 {
		t.Fatal("expected at least 1 extent")
	}
}

// ============================================================================
// ShrinkVolume D: 实际缩容测试 (需管理员)
// ============================================================================

func TestShrinkVolume_DriveD(t *testing.T) {
	if !isAdmin() {
		t.Skip("need admin rights")
	}

	const shrinkMB = 100
	shrinkBytes := int64(shrinkMB) * 1024 * 1024

	t.Logf("Shrinking D: by %d MB...", shrinkMB)

	freed, err := ShrinkVolume("D:", shrinkBytes, false)
	if err != nil {
		t.Fatalf("ShrinkVolume: %v", err)
	}

	if len(freed) == 0 {
		t.Fatal("expected at least 1 freed extent")
	}

	for i, f := range freed {
		t.Logf("Freed[%d]: Disk=%d Offset=%d Length=%d (%.2f MiB)",
			i, f.DiskNumber, f.Offset, f.Length,
			float64(f.Length)/(1024*1024))
		if f.Length <= 0 {
			t.Errorf("Freed[%d].Length should be positive, got %d", i, f.Length)
		}
	}

	// 扩展回去
	t.Logf("Extending D: back...")
	if err := extendVolumeWithDiskpart("D:"); err != nil {
		t.Errorf("extend failed: %v (may need manual recovery)", err)
	}
}

func TestShrinkVolume_ExcessiveShrink(t *testing.T) {
	if !isAdmin() {
		t.Skip("need admin rights")
	}

	// 100 TB：远超 D 盘大小
	_, err := ShrinkVolume("D:", 100*1024*1024*1024*1024, false)
	if err == nil {
		t.Error("expected error for excessive shrink")
	}
	t.Logf("Expected error: %v", err)
}

func TestShrinkVolume_ZeroSize(t *testing.T) {
	_, err := ShrinkVolume("D:", 0, false)
	if err == nil {
		t.Error("expected error for zero shrink")
	}
}

func TestShrinkVolume_InvalidDrive(t *testing.T) {
	_, err := ShrinkVolume("Z:", 100*1024*1024, false)
	if err == nil {
		t.Error("expected error for invalid drive")
	}
}

func TestShrinkVolume_Reuse(t *testing.T) {
	if !isAdmin() {
		t.Skip("need admin rights")
	}

	const shrinkMB = 100
	shrinkBytes := int64(shrinkMB) * 1024 * 1024

	// 1. 先执行一次缩容，制造空闲空间
	t.Logf("Step 1: shrinking D: by %d MB...", shrinkMB)
	freed1, err := ShrinkVolume("D:", shrinkBytes, false)
	if err != nil {
		t.Fatalf("first shrink: %v", err)
	}
	t.Logf("First shrink freed: Disk=%d Offset=%d Length=%d",
		freed1[0].DiskNumber, freed1[0].Offset, freed1[0].Length)

	// 2. 尝试以 reuse=true 复用已有空闲空间
	t.Logf("Step 2: reuse check (should find existing free space)...")
	freed2, err := ShrinkVolume("D:", shrinkBytes/2, true) // 只需要 50 MB
	if err != nil {
		t.Fatalf("reuse shrink: %v", err)
	}
	// reuse 模式下不应再缩容，直接返回已存在的空闲空间
	// 空闲空间至少 >= shrinkBytes
	if freed2[0].Length < shrinkBytes/2 {
		t.Errorf("reused space too small: %d < %d", freed2[0].Length, shrinkBytes/2)
	}
	t.Logf("Reused: Disk=%d Offset=%d Length=%d",
		freed2[0].DiskNumber, freed2[0].Offset, freed2[0].Length)

	// 3. 扩展回去
	t.Logf("Step 3: extending back...")
	if err := extendVolumeWithDiskpart("D:"); err != nil {
		t.Errorf("extend failed: %v", err)
	}
}

func TestShrinkVolume_ReuseNoSpace(t *testing.T) {
	if !isAdmin() {
		t.Skip("need admin rights")
	}

	// D: 当前无空闲空间（未缩容）时，reuse 应失败并回退到实际缩容
	const shrinkMB = 10
	shrinkBytes := int64(shrinkMB) * 1024 * 1024

	t.Logf("Shrinking D: by %d MB with reuse=true...", shrinkMB)
	freed, err := ShrinkVolume("D:", shrinkBytes, true)
	if err != nil {
		t.Fatalf("ShrinkVolume with reuse: %v", err)
	}
	t.Logf("Freed: Disk=%d Offset=%d Length=%d",
		freed[0].DiskNumber, freed[0].Offset, freed[0].Length)

	// 扩展回去
	if err := extendVolumeWithDiskpart("D:"); err != nil {
		t.Errorf("extend failed: %v", err)
	}
}

// ============================================================================
// 辅助
// ============================================================================

func openVolumeRO(path string) (syscall.Handle, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return syscall.CreateFile(p, syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil,
		syscall.OPEN_EXISTING, 0, 0)
}

func closeVolume(h syscall.Handle) {
	syscall.CloseHandle(h)
}

// extendVolumeWithDiskpart 使用 diskpart 将卷扩展到最大。
func extendVolumeWithDiskpart(vol string) error {
	vol = strings.TrimRight(vol, `\`)
	vol = strings.TrimPrefix(vol, `\\.\`)

	script := fmt.Sprintf("select volume=%s\nextend\nexit\n", vol)
	cmd := exec.Command("diskpart")
	cmd.Stdin = strings.NewReader(script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("diskpart extend %s: %v\n%s", vol, err, string(out))
	}
	return nil
}

// isAdmin 检查是否有管理员权限
func isAdmin() bool {
	_, err := os.Open(`\\.\PHYSICALDRIVE0`)
	return err == nil
}
