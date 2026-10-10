package x2xcore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

// writeInf 将 INF 文本按指定编码写入临时文件并返回路径。
// enc 为 "utf16le" 时按 UTF-16 LE（含 BOM）编码，否则按 UTF-8 原样写入。
func writeInf(t *testing.T, content, enc string) string {
	t.Helper()

	var data []byte
	if enc == "utf16le" {
		data = append(data, 0xFF, 0xFE) // UTF-16 LE BOM
		for _, r := range content {
			for _, u := range utf16.Encode([]rune{r}) {
				data = append(data, byte(u), byte(u>>8))
			}
		}
	} else {
		data = []byte(content)
	}

	path := filepath.Join(t.TempDir(), "test.inf")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestParseINF_UTF16LE 验证 UTF-16 LE（带 BOM）编码的 INF 能正确解析出硬件 ID。
// 微软提供的驱动 INF（如 Intel 网卡驱动）通常是这种编码。
func TestParseINF_UTF16LE(t *testing.T) {
	content := `[Version]
Signature = "$Windows NT$"
Class = Net
ClassGUID = {4d36e972-e325-11ce-bfc1-08002be10318}
Provider = %Intel%

[Manufacturer]
%Intel% = Intel, NTamd64.6.2, NTamd64.6.2.1

[Intel.NTamd64.6.2]
%Desc% = E10DE, PCI\VEN_8086&DEV_10DE

[Intel.NTamd64.6.2.1]
%Desc% = E10DE, PCI\VEN_8086&DEV_10DE&SUBSYS_10DE8086

[Strings]
Intel = "Intel"
Desc = "Intel NIC"
`

	inf, err := ParseINF(writeInf(t, content, "utf16le"))
	if err != nil {
		t.Fatalf("ParseINF: %v", err)
	}

	ids := inf.HardwareIds()
	want := map[string]bool{
		"pci\\ven_8086&dev_10de":                 true,
		"pci\\ven_8086&dev_10de&subsys_10de8086": true,
	}

	got := map[string]bool{}
	for _, id := range ids {
		got[id] = true
	}

	for id := range want {
		if !got[id] {
			t.Errorf("missing hardware id %q (got %v)", id, ids)
		}
	}
	if len(ids) != len(want) {
		t.Errorf("unexpected hardware ids: %v", ids)
	}
}

// TestParseINF_UTF8_NoBOM 验证无 BOM 的 UTF-8 INF（无架构装饰的
// Manufacturer）仍能正常解析，防止编码修复破坏原有路径。
func TestParseINF_UTF8_NoBOM(t *testing.T) {
	content := `[Version]
Signature = "$Windows NT$"
Class = SCSIAdapter

[Manufacturer]
%Vendor% = DeviceSection

[DeviceSection]
%Desc% = InstallSection, PCI\VEN_1234&DEV_5678

[Strings]
Vendor = "Example Corp"
Desc = "Example Controller"
`

	inf, err := ParseINF(writeInf(t, content, "utf8"))
	if err != nil {
		t.Fatalf("ParseINF: %v", err)
	}

	ids := inf.HardwareIds()
	if len(ids) != 1 || ids[0] != "pci\\ven_1234&dev_5678" {
		t.Fatalf("unexpected hardware ids: %v", ids)
	}

	if !strings.EqualFold(inf.Class(), "SCSIAdapter") {
		t.Errorf("unexpected class: %q", inf.Class())
	}
}
