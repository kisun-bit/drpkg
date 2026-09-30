package xutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// 测试数据路径
// ---------------------------------------------------------------------------

// smallInfDir 是项目自带驱动库的 INF 目录（3 个 INF：viostor/vioscsi/netkvm）。
func smallInfDir(t *testing.T) string {
	t.Helper()
	p := `D:\workspace\drpkg\platform\recovery\x2xlib\library\driverstore.H0nK1\windows\MICROSOFT\amd64\5bb86f9afd614ec3909ce26ae2e73c63`
	if _, err := os.Stat(p); err != nil {
		t.Skip("small INF dir not found, skipping")
	}
	return p
}

// realDriversRoot 是 PEDriverRelpaceGUI 的物理机驱动库根目录（55 个 INF）。
func realDriversRoot(t *testing.T) string {
	t.Helper()
	p := `D:\workspace\honki-sundry\dr\PEDriverReplace-鼎甲Windows恢复PE\Program Files\PEDriverRelpaceGUI\Drivers`
	if _, err := os.Stat(p); err != nil {
		t.Skip("real drivers dir not found, skipping")
	}
	return p
}

// ---------------------------------------------------------------------------
// 基础功能测试（小数据集）
// ---------------------------------------------------------------------------

func TestBuildInfIndex_Basic(t *testing.T) {
	dir := smallInfDir(t)
	if dir == "" {
		return
	}

	t.Logf("building index from: %s", dir)
	idx, err := BuildInfIndex(dir)
	if err != nil {
		t.Fatalf("BuildInfIndex: %v", err)
	}

	if idx.Size() == 0 {
		t.Fatal("index is empty, expected at least 1 entry")
	}
	t.Logf("index: %d entries, %d unique INFs, %d unique HWIDs",
		idx.Size(), idx.NumInfs(), len(idx.AllHwids()))

	// viostor HWID
	drivers := idx.FindByHwid("pci\\ven_1af4&dev_1001")
	if len(drivers) == 0 {
		drivers = idx.FindByHwid("pci\\ven_1af4&dev_1001&subsys_00021af4")
	}
	if len(drivers) == 0 {
		t.Fatal("viostor HWID not found")
	}

	d := drivers[0]
	if d.InfName == "" {
		t.Error("InfName is empty")
	}
	if d.ClassGUID == "" {
		t.Error("ClassGUID is empty")
	}
	if len(d.SvcNames) == 0 {
		t.Error("no service names found")
	}
	if len(d.SysFiles) == 0 {
		t.Error("no sys files found")
	}
	if d.Provider == "" {
		t.Error("Provider is empty")
	}
	if d.DriverVer == "" {
		t.Error("DriverVer is empty")
	}

	t.Logf("viostor: INF=%s Class=%s Provider=%s Ver=%s Svcs=%v Sys=%v",
		d.InfName, d.Class, d.Provider, d.DriverVer, d.SvcNames, d.SysFiles)
}

func TestBuildInfIndex_FindByHwid_CaseInsensitive(t *testing.T) {
	dir := smallInfDir(t)
	if dir == "" {
		return
	}
	idx, err := BuildInfIndex(dir)
	if err != nil {
		t.Fatalf("BuildInfIndex: %v", err)
	}

	all := idx.AllHwids()
	if len(all) == 0 {
		t.Skip("no HWIDs in index")
	}

	hwid := all[0]
	upper := strings.ToUpper(hwid)
	lower := strings.ToLower(hwid)

	r1 := idx.FindByHwid(upper)
	r2 := idx.FindByHwid(lower)
	if len(r1) != len(r2) {
		t.Fatalf("case mismatch: %q→%d vs %q→%d", upper, len(r1), lower, len(r2))
	}
}

func TestBuildInfIndex_UnknownHwid(t *testing.T) {
	dir := smallInfDir(t)
	if dir == "" {
		return
	}
	idx, _ := BuildInfIndex(dir)
	if r := idx.FindByHwid("pci\\ven_dead&dev_beef"); r != nil {
		t.Fatalf("expected nil for unknown HWID, got %d results", len(r))
	}
}

func TestBuildInfIndex_EmptyDir(t *testing.T) {
	idx, err := BuildInfIndex(t.TempDir())
	if err != nil {
		t.Fatalf("BuildInfIndex on empty dir: %v", err)
	}
	if idx.Size() != 0 {
		t.Fatalf("expected 0 entries, got %d", idx.Size())
	}
}

func TestBuildInfIndex_Dedup(t *testing.T) {
	dir := smallInfDir(t)
	if dir == "" {
		return
	}
	idx, _ := BuildInfIndex(dir)
	for hwid := range idx.hwidMap {
		drivers := idx.FindByHwid(hwid)
		seen := make(map[string]bool)
		for _, d := range drivers {
			key := d.InfPath + "|" + d.Hwid
			if seen[key] {
				t.Errorf("duplicate result for %s: %s", hwid, key)
			}
			seen[key] = true
		}
		break
	}
}

// ---------------------------------------------------------------------------
// 递归构建 + 真实驱动库测试
// ---------------------------------------------------------------------------

func TestRecursiveBuild_RealDrivers(t *testing.T) {
	root := realDriversRoot(t)
	if root == "" {
		return
	}

	t.Logf("building recursive index from: %s", root)
	idx, err := BuildInfIndexRecursive(root)
	if err != nil {
		t.Fatalf("BuildInfIndexRecursive: %v", err)
	}

	nEntries := idx.Size()
	nInfs := idx.NumInfs()
	nHwids := len(idx.AllHwids())

	t.Logf("index: %d entries, %d unique INFs, %d unique HWIDs", nEntries, nInfs, nHwids)

	if nEntries == 0 {
		t.Fatal("index is empty, expected entries from real drivers")
	}
	if nInfs == 0 {
		t.Fatal("no INF files indexed")
	}

	// 55 个 INF 里大部分是存储/网络类，应至少有 10 个被收录
	if nInfs < 10 {
		t.Errorf("too few INF files indexed: %d (expected >= 10)", nInfs)
	}

	// ClassGUID 必须有分布
	guids := idx.AllClassGUIDs()
	t.Logf("ClassGUIDs: %v", guids)
	if len(guids) == 0 {
		t.Error("no ClassGUIDs found")
	}

	// Provider 必须非空
	providers := idx.AllProviders()
	t.Logf("Providers (%d): %v", len(providers), providers)

	// 打印每种 ClassGUID 的条目数
	for _, g := range guids {
		count := 0
		for _, hwid := range idx.AllHwids() {
			for _, d := range idx.FindByHwid(hwid) {
				if strings.EqualFold(d.ClassGUID, g) {
					count++
					break
				}
			}
		}
		t.Logf("  %s: ~%d HWIDs", g, count)
	}
}

// TestRecursiveBuild_KnownHwids 验证已知厂商的 HWID 能正确匹配。
func TestRecursiveBuild_KnownHwids(t *testing.T) {
	root := realDriversRoot(t)
	if root == "" {
		return
	}

	idx, err := BuildInfIndexRecursive(root)
	if err != nil {
		t.Fatalf("BuildInfIndexRecursive: %v", err)
	}

	tests := []struct {
		hwid    string
		wantSvc string // 期望的服务名中的关键字
		desc    string
	}{
		{
			hwid:    "pci\\ven_1af4&dev_1001&subsys_00021af4&rev_00",
			wantSvc: "viostor",
			desc:    "VirtIO block (viostor)",
		},
		{
			hwid:    "pci\\ven_1af4&dev_1004&subsys_00081af4&rev_00",
			wantSvc: "vioscsi",
			desc:    "VirtIO SCSI (vioscsi)",
		},
		{
			hwid:    "pci\\ven_1af4&dev_1000",
			wantSvc: "netkvm",
			desc:    "VirtIO net (netkvm)",
		},
		{
			hwid:    "pci\\ven_9005&dev_0285&subsys_0286108e",
			wantSvc: "arcsas",
			desc:    "Adaptec SAS (arcsas)",
		},
		{
			hwid:    "pci\\ven_15ad&dev_07b0",
			wantSvc: "vmxnet3",
			desc:    "VMware vmxnet3",
		},
		{
			hwid:    "pci\\ven_15b3&dev_1013",
			wantSvc: "mlx5",
			desc:    "Mellanox ConnectX-4 (mlx5)",
		},
		{
			hwid:    "pci\\ven_8086&dev_153a",
			wantSvc: "e1d",
			desc:    "Intel I210 Gigabit (e1dexpress)",
		},
		{
			hwid:    "pci\\ven_104b&dev_1040",
			wantSvc: "vmscsi",
			desc:    "VMware PVSCSI",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			drivers := idx.FindByHwid(tt.hwid)
			if len(drivers) == 0 {
				t.Fatalf("HWID %s not found in driver library", tt.hwid)
			}

			t.Logf("found %d driver(s) for %s:", len(drivers), tt.hwid)
			matched := false
			for _, d := range drivers {
				t.Logf("  INF=%s SvcNames=%v SysFiles=%v", d.InfName, d.SvcNames, d.SysFiles)
				for _, svc := range d.SvcNames {
					if strings.Contains(strings.ToLower(svc), tt.wantSvc) {
						matched = true
					}
				}
			}
			if !matched {
				t.Errorf("expected service containing %q, got services: see log above", tt.wantSvc)
			}
		})
	}
}

// TestRecursiveBuild_NoDups 验证大型索引中无 INF+HWID 重复。
func TestRecursiveBuild_NoDups(t *testing.T) {
	root := realDriversRoot(t)
	if root == "" {
		return
	}

	idx, err := BuildInfIndexRecursive(root)
	if err != nil {
		t.Fatalf("BuildInfIndexRecursive: %v", err)
	}

	// 统计每个 INF+HWID 组合
	type key struct{ path, hwid string }
	counts := make(map[key]int, idx.Size())
	for _, hwid := range idx.AllHwids() {
		for _, d := range idx.FindByHwid(hwid) {
			k := key{d.InfPath, hwid}
			counts[k]++
		}
	}

	dups := 0
	for k, c := range counts {
		if c > 1 {
			t.Errorf("duplicate: %s → %s (%d times)", k.path, k.hwid, c)
			dups++
		}
	}
	if dups > 0 {
		t.Fatalf("found %d duplicate INF+HWID combinations", dups)
	}
	t.Logf("checked %d unique INF+HWID combinations, no duplicates", len(counts))
}

// TestRecursiveBuild_CaseInsensitive 验证真实驱动库的大小写匹配。
func TestRecursiveBuild_CaseInsensitive(t *testing.T) {
	root := realDriversRoot(t)
	if root == "" {
		return
	}

	idx, err := BuildInfIndexRecursive(root)
	if err != nil {
		t.Fatalf("BuildInfIndexRecursive: %v", err)
	}

	all := idx.AllHwids()
	if len(all) == 0 {
		t.Skip("no HWIDs")
	}

	// 测试前 10 个 HWID 的各种大小写变体
	n := 10
	if len(all) < n {
		n = len(all)
	}
	for i := 0; i < n; i++ {
		hwid := all[i]
		for _, variant := range []string{
			strings.ToUpper(hwid),
			strings.ToLower(hwid),
		} {
			r := idx.FindByHwid(variant)
			if len(r) == 0 {
				t.Errorf("no result for variant %q of %s", variant, hwid)
			}
		}
	}
}

// TestRecursiveBuild_FieldIntegrity 验证每个 DriverInfo 的关键字段非空。
func TestRecursiveBuild_FieldIntegrity(t *testing.T) {
	root := realDriversRoot(t)
	if root == "" {
		return
	}

	idx, err := BuildInfIndexRecursive(root)
	if err != nil {
		t.Fatalf("BuildInfIndexRecursive: %v", err)
	}

	all := idx.AllHwids()
	if len(all) == 0 {
		t.Skip("no HWIDs")
	}

	checked := 0
	emptyFields := make(map[string]int)

	for _, hwid := range all {
		for _, d := range idx.FindByHwid(hwid) {
			checked++
			if d.InfPath == "" {
				emptyFields["InfPath"]++
			}
			if d.InfName == "" {
				emptyFields["InfName"]++
			}
			if d.Hwid == "" {
				emptyFields["Hwid"]++
			}

			// ClassGUID 可能为空（如无 [Version] 段的 INF），
			// 但 SvcNames 必须非空（否则驱动无法工作）
			if len(d.SvcNames) == 0 {
				emptyFields[fmt.Sprintf("SvcNames(%s)", d.InfName)]++
			}
		}
	}

	t.Logf("checked %d driver results", checked)
	for k, v := range emptyFields {
		if v > 0 && strings.HasPrefix(k, "SvcNames") {
			t.Errorf("missing service names for %s", k)
		} else if v > 0 {
			t.Errorf("%d entries have empty %s", v, k)
		}
	}
}

// ---------------------------------------------------------------------------
// Example
// ---------------------------------------------------------------------------

func ExampleBuildInfIndex() {
	dir, err := os.MkdirTemp("", "infidx_example")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer os.RemoveAll(dir)

	inf := `[Version]
Signature = "$WINDOWS NT$"
Class = SCSIAdapter
ClassGUID = {4D36E97B-E325-11CE-BFC1-08002BE10318}
Provider = %Vendor%
DriverVer = 01/01/2024,1.0.0.0
CatalogFile = example.cat

[Manufacturer]
%Vendor% = DeviceSection

[DeviceSection]
%DeviceDesc% = InstallSection, PCI\VEN_1234&DEV_5678

[InstallSection]
AddReg = InstallSection.AddReg

[InstallSection.Services]
AddService = example, 2, ExampleService

[ExampleService]
ServiceType = 1
StartType = 0
ErrorControl = 1
ServiceBinary = %12%\example.sys

[Strings]
Vendor = "Example Corp"
DeviceDesc = "Example Storage Controller"
`
	os.WriteFile(filepath.Join(dir, "example.inf"), []byte(inf), 0644)

	idx, err := BuildInfIndex(dir)
	if err != nil {
		fmt.Println(err)
		return
	}

	drivers := idx.FindByHwid("pci\\ven_1234&dev_5678")
	if len(drivers) > 0 {
		fmt.Printf("Class: %s\n", drivers[0].Class)
		fmt.Printf("Services: %s\n", strings.Join(drivers[0].SvcNames, ","))
	}
	// Output:
	// Class: SCSIAdapter
	// Services: example
}

// ---------------------------------------------------------------------------
// Benchmark（递归全量）
// ---------------------------------------------------------------------------

func BenchmarkBuildInfIndexRecursive(b *testing.B) {
	root := `D:\workspace\honki-sundry\dr\PEDriverReplace-鼎甲Windows恢复PE\Program Files\PEDriverRelpaceGUI\Drivers`
	if _, err := os.Stat(root); err != nil {
		b.Skip("real drivers dir not found")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := BuildInfIndexRecursive(root)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFindByHwid_Real(b *testing.B) {
	root := `D:\workspace\honki-sundry\dr\PEDriverReplace-鼎甲Windows恢复PE\Program Files\PEDriverRelpaceGUI\Drivers`
	if _, err := os.Stat(root); err != nil {
		b.Skip("real drivers dir not found")
	}

	idx, err := BuildInfIndexRecursive(root)
	if err != nil {
		b.Fatal(err)
	}

	all := idx.AllHwids()
	if len(all) == 0 {
		b.Fatal("no HWIDs")
	}
	hwid := all[0]

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = idx.FindByHwid(hwid)
	}
}