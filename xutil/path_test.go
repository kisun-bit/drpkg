package xutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToExtendedPath_NormalPath(t *testing.T) {
	// 获取当前工作目录的绝对路径
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	result := ToExtendedPath(cwd)
	if !strings.HasPrefix(result, ExtendedPathPrefix) {
		t.Fatalf("expected prefix %s, got %s", ExtendedPathPrefix, result)
	}
	if !strings.HasSuffix(result, cwd) {
		t.Fatalf("expected suffix %s, got %s", cwd, result)
	}
}

func TestToExtendedPath_AlreadyExtended(t *testing.T) {
	path := `\\?\C:\Windows`
	result := ToExtendedPath(path)
	if result != path {
		t.Fatalf("expected %s, got %s", path, result)
	}
}

func TestToExtendedPath_UNC(t *testing.T) {
	// UNC 路径应转换为 \\?\UNC\ 格式
	path := `\\server\share\folder`
	result := ToExtendedPath(path)
	if !strings.HasPrefix(result, UNCPathPrefix) {
		t.Fatalf("expected UNC prefix %s, got %s", UNCPathPrefix, result)
	}
}

func TestToExtendedPath_GlobalRoot(t *testing.T) {
	// VSS 快照裸卷名（5 个反斜杠）需要追加尾部分隔符
	path := `\\?\GLOBALROOT\Device\HarddiskVolumeShadowCopy1`
	result := ToExtendedPath(path)
	if !strings.HasSuffix(result, string(filepath.Separator)) {
		t.Fatalf("expected trailing separator, got %s", result)
	}

	// 带子目录的 VSS 快照路径不需要追加
	path2 := `\\?\GLOBALROOT\Device\HarddiskVolumeShadowCopy1\folder`
	result2 := ToExtendedPath(path2)
	if strings.HasSuffix(result2, string(filepath.Separator)+string(filepath.Separator)) {
		t.Fatalf("expected no double separator, got %s", result2)
	}
}

func TestToExtendedPath_UNCAlreadyExtended(t *testing.T) {
	path := `\\?\UNC\server\share\folder`
	result := ToExtendedPath(path)
	if result != path {
		t.Fatalf("expected %s, got %s", path, result)
	}
}

func TestExtractVolumeName_NormalPath(t *testing.T) {
	vol, err := ExtractVolumeName(`C:\Windows\System32`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ToLower(vol) != "c:" {
		t.Fatalf("expected c:, got %s", vol)
	}
}

func TestExtractVolumeName_ExtendedPath(t *testing.T) {
	vol, err := ExtractVolumeName(`\\?\C:\Windows\System32`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ToLower(vol) != "c:" {
		t.Fatalf("expected c:, got %s", vol)
	}
}

func TestExtractVolumeName_GlobalRoot(t *testing.T) {
	// VSS 快照路径
	path := `\\?\GLOBALROOT\Device\HarddiskVolumeShadowCopy1\folder\file.txt`
	vol, err := ExtractVolumeName(path)
	if err != nil {
		t.Fatal(err)
	}
	expected := `\\?\GLOBALROOT\Device\HarddiskVolumeShadowCopy1`
	if vol != expected {
		t.Fatalf("expected %s, got %s", expected, vol)
	}
}

func TestExtractVolumeName_GlobalRootRoot(t *testing.T) {
	// VSS 快照根路径
	path := `\\?\GLOBALROOT\Device\HarddiskVolumeShadowCopy1\`
	vol, err := ExtractVolumeName(path)
	if err != nil {
		t.Fatal(err)
	}
	expected := `\\?\GLOBALROOT\Device\HarddiskVolumeShadowCopy1`
	if vol != expected {
		t.Fatalf("expected %s, got %s", expected, vol)
	}
}

func TestExtractVolumeName_VolumeGUID(t *testing.T) {
	// 卷 GUID 路径
	path := `\\?\Volume{39b9cac2-bcdb-4d51-97c8-0d0677d607fb}\folder`
	vol, err := ExtractVolumeName(path)
	if err != nil {
		t.Fatal(err)
	}
	expected := `\\?\Volume{39b9cac2-bcdb-4d51-97c8-0d0677d607fb}`
	if strings.ToLower(vol) != strings.ToLower(expected) {
		t.Fatalf("expected %s, got %s", expected, vol)
	}
}

func TestExtractVolumeName_UNCPath(t *testing.T) {
	// UNC 扩展路径
	path := `\\?\UNC\server\share\folder`
	vol, err := ExtractVolumeName(path)
	if err != nil {
		t.Fatal(err)
	}
	expected := `\\server\share`
	if strings.ToLower(vol) != strings.ToLower(expected) {
		t.Fatalf("expected %s, got %s", expected, vol)
	}
}

func TestExtractVolumeName_RelativePath(t *testing.T) {
	// 相对路径应转为绝对路径后提取卷名
	vol, err := ExtractVolumeName(`.`)
	if err != nil {
		t.Fatal(err)
	}
	// 应返回当前驱动器的卷名
	if vol == "" {
		t.Fatal("expected non-empty volume name")
	}
}

func TestExtractVolumeName_EmptyPath(t *testing.T) {
	vol, err := ExtractVolumeName("")
	// 空路径可能返回错误或空卷名
	if err == nil && vol == "" {
		t.Log("empty path returned empty volume name")
	}
}

// =============================================================================
// 往返测试：ToExtendedPath 处理后 ExtractVolumeName 应能正确提取卷名
// =============================================================================

func TestRoundTrip_NormalPath(t *testing.T) {
	path := `C:\Windows`
	extPath := ToExtendedPath(path)
	vol, err := ExtractVolumeName(extPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ToLower(vol) != "c:" {
		t.Fatalf("roundtrip failed: %s → %s → vol=%s", path, extPath, vol)
	}
}

func TestRoundTrip_GlobalRoot(t *testing.T) {
	path := `\\?\GLOBALROOT\Device\HarddiskVolumeShadowCopy1`
	extPath := ToExtendedPath(path)
	vol, err := ExtractVolumeName(extPath)
	if err != nil {
		t.Fatal(err)
	}
	expected := `\\?\GLOBALROOT\Device\HarddiskVolumeShadowCopy1`
	if vol != expected {
		t.Fatalf("roundtrip failed: %s → %s → vol=%s", path, extPath, vol)
	}
}

func TestRoundTrip_UNCPath(t *testing.T) {
	path := `\\server\share\folder`
	extPath := ToExtendedPath(path)
	vol, err := ExtractVolumeName(extPath)
	if err != nil {
		t.Fatal(err)
	}
	expected := `\\server\share`
	if strings.ToLower(vol) != strings.ToLower(expected) {
		t.Fatalf("roundtrip failed: %s → %s → vol=%s", path, extPath, vol)
	}
}

// =============================================================================
// 常量测试
// =============================================================================

func TestPathConstants(t *testing.T) {
	if ExtendedPathPrefix != `\\?\` {
		t.Fatal("ExtendedPathPrefix mismatch")
	}
	if UNCPathPrefix != `\\?\UNC\` {
		t.Fatal("UNCPathPrefix mismatch")
	}
	if GlobalRootPrefix != `\\?\GLOBALROOT\` {
		t.Fatal("GlobalRootPrefix mismatch")
	}
	if VolumeGUIDPrefix != `\\?\Volume{` {
		t.Fatal("VolumeGUIDPrefix mismatch")
	}
}