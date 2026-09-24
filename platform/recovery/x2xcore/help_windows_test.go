package x2xcore

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// TestDetectWindowsVersion 覆盖版本判定纯函数的所有分支。
// 期望值只依赖输入，不依赖运行环境，因此是确定性的。
func TestDetectWindowsVersion(t *testing.T) {
	cases := []struct {
		name     string
		isServer bool
		major    uint32
		minor    uint32
		build    uint32
		want     string
	}{
		// 现代客户端
		{name: "win10 19045", major: 10, minor: 0, build: 19045, want: "win10"},
		{name: "win11 boundary 22000", major: 10, minor: 0, build: 22000, want: "win11"},
		{name: "win11 22621", major: 10, minor: 0, build: 22621, want: "win11"},

		// 现代服务器
		{name: "server low build -> 2016", isServer: true, major: 10, minor: 0, build: 10586, want: "win2k16"},
		{name: "server 2016 14393", isServer: true, major: 10, minor: 0, build: 14393, want: "win2k16"},
		{name: "server 2019 17763", isServer: true, major: 10, minor: 0, build: 17763, want: "win2k19"},
		{name: "server 2022 20348", isServer: true, major: 10, minor: 0, build: 20348, want: "win2k22"},
		{name: "server 2025 26100", isServer: true, major: 10, minor: 0, build: 26100, want: "win2k25"},

		// 旧系统
		{name: "win2k 5.0", major: 5, minor: 0, build: 2195, want: "win2k"},
		{name: "winxp 5.1", major: 5, minor: 1, build: 2600, want: "winxp"},
		{name: "winxp x64 5.2", major: 5, minor: 2, build: 3790, want: "winxp"},
		{name: "server 2003 5.2", isServer: true, major: 5, minor: 2, build: 3790, want: "win2k3"},
		{name: "vista 6.0", major: 6, minor: 0, build: 6002, want: "winvista"},
		{name: "server 2008 6.0", isServer: true, major: 6, minor: 0, build: 6002, want: "win2k8"},
		{name: "win7 6.1", major: 6, minor: 1, build: 7601, want: "win7"},
		{name: "server 2008r2 6.1", isServer: true, major: 6, minor: 1, build: 7601, want: "win2k8r2"},
		{name: "win8 6.2", major: 6, minor: 2, build: 9200, want: "win8"},
		{name: "server 2012 6.2", isServer: true, major: 6, minor: 2, build: 9200, want: "win2k12"},
		{name: "win8.1 6.3", major: 6, minor: 3, build: 9600, want: "win8.1"},
		{name: "server 2012r2 6.3", isServer: true, major: 6, minor: 3, build: 9600, want: "win2k12r2"},

		// 未知
		{name: "nt4 4.0 unknown", major: 4, minor: 0, build: 1381, want: "Unknown"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := detectWindowsVersion(c.isServer, c.major, c.minor, c.build)
			if got != c.want {
				t.Fatalf("detectWindowsVersion(server=%v, %d.%d.%d) = %q, want %q",
					c.isServer, c.major, c.minor, c.build, got, c.want)
			}
		})
	}
}

// TestReadFileVersion 以本机 ntoskrnl.exe 为样本，交叉验证 readFileVersion
// 从 ProductVersion 读出的 major/minor 与 RtlGetVersion() 报告的内核版本一致。
//
// build 是"基座 build"（不含 UBR 累加的 +N），与 RtlGetVersion 报告的 build
// 可能不同（例如 19041 vs 19045），因此这里只校验 major/minor 与 build 非零。
func TestReadFileVersion(t *testing.T) {
	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		t.Skip("SystemRoot not set")
	}

	ntoskrnl := filepath.Join(systemRoot, "System32", "ntoskrnl.exe")
	if _, err := os.Stat(ntoskrnl); err != nil {
		t.Skipf("ntoskrnl.exe not available: %v", err)
	}

	major, minor, build, err := readFileVersion(ntoskrnl)
	if err != nil {
		t.Fatalf("readFileVersion(%s): %v", ntoskrnl, err)
	}

	v := windows.RtlGetVersion()
	if major != v.MajorVersion || minor != v.MinorVersion {
		t.Fatalf("readFileVersion(%s) = %d.%d.%d, want kernel major.minor %d.%d",
			ntoskrnl, major, minor, build, v.MajorVersion, v.MinorVersion)
	}
	if build == 0 {
		t.Fatalf("readFileVersion(%s): build == 0", ntoskrnl)
	}
}

// TestReadFileVersion_NotExist 读不存在的文件应返回错误。
func TestReadFileVersion_NotExist(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-file.exe")
	if _, _, _, err := readFileVersion(missing); err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}
