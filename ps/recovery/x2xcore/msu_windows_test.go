package x2xcore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kisun-bit/drpkg/ps/recovery/x2xlib"
)

// setupMsuDir 创建临时驱动资源目录，写入指定的 .msu 占位文件，
// 可选写入 order 文件，返回目录路径。
func setupMsuDir(t *testing.T, msus []string, orderContent *string) string {
	t.Helper()

	dir := t.TempDir()

	for _, name := range msus {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	if orderContent != nil {
		if err := os.WriteFile(
			filepath.Join(dir, x2xlib.MsuOrderFileName),
			[]byte(*orderContent),
			0o644,
		); err != nil {
			t.Fatalf("write order: %v", err)
		}
	}

	return dir
}

func baseNames(paths []string) []string {
	names := make([]string, 0, len(paths))
	for _, p := range paths {
		names = append(names, filepath.Base(p))
	}
	return names
}

// 无 order 文件：按文件名升序返回全部 MSU。
func TestListMsuPackages_NoOrder(t *testing.T) {
	dir := setupMsuDir(t, []string{
		"Windows6.1-KB3087873-v2-x86.msu",
		"Windows6.1-KB2990941-v3-x86.msu",
	}, nil)

	msus, err := listMsuPackages(dir)
	if err != nil {
		t.Fatalf("listMsuPackages: %v", err)
	}

	got := baseNames(msus)
	want := []string{
		"Windows6.1-KB2990941-v3-x86.msu",
		"Windows6.1-KB3087873-v2-x86.msu",
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %v, want %v", got, want)
	}

	planned, err := planMsuInstallOrder(dir, msus)
	if err != nil {
		t.Fatalf("planMsuInstallOrder: %v", err)
	}
	if len(planned) != 2 {
		t.Fatalf("planned %d packages, want 2", len(planned))
	}
}

// 无 MSU 文件：返回空列表。
func TestListMsuPackages_Empty(t *testing.T) {
	dir := setupMsuDir(t, []string{"readme.txt"}, nil)

	msus, err := listMsuPackages(dir)
	if err != nil {
		t.Fatalf("listMsuPackages: %v", err)
	}
	if len(msus) != 0 {
		t.Fatalf("got %d packages, want 0", len(msus))
	}
}

// order 文件存在：按行序安装，空行与空白行忽略，大小写不敏感。
func TestPlanMsuInstallOrder_WithOrder(t *testing.T) {
	order := "Windows6.1-KB3087873-v2-x86.msu\r\n" +
		"\r\n" +
		"windows6.1-kb2990941-v3-x86.msu\n"

	dir := setupMsuDir(t, []string{
		"Windows6.1-KB2990941-v3-x86.msu",
		"Windows6.1-KB3087873-v2-x86.msu",
	}, &order)

	msus, err := listMsuPackages(dir)
	if err != nil {
		t.Fatalf("listMsuPackages: %v", err)
	}

	planned, err := planMsuInstallOrder(dir, msus)
	if err != nil {
		t.Fatalf("planMsuInstallOrder: %v", err)
	}

	got := baseNames(planned)
	want := []string{
		"Windows6.1-KB3087873-v2-x86.msu",
		"Windows6.1-KB2990941-v3-x86.msu",
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// order 覆盖不全：报错。
func TestPlanMsuInstallOrder_MissingEntry(t *testing.T) {
	order := "Windows6.1-KB2990941-v3-x86.msu\n"

	dir := setupMsuDir(t, []string{
		"Windows6.1-KB2990941-v3-x86.msu",
		"Windows6.1-KB3087873-v2-x86.msu",
	}, &order)

	msus, err := listMsuPackages(dir)
	if err != nil {
		t.Fatalf("listMsuPackages: %v", err)
	}

	_, err = planMsuInstallOrder(dir, msus)
	if err == nil {
		t.Fatal("expected error for order not covering all packages")
	}
	if !strings.Contains(err.Error(), "does not cover") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// order 列出多余条目：报错。
func TestPlanMsuInstallOrder_UnknownEntry(t *testing.T) {
	order := "Windows6.1-KB2990941-v3-x86.msu\n" +
		"not-exist.msu\n"

	dir := setupMsuDir(t, []string{
		"Windows6.1-KB2990941-v3-x86.msu",
	}, &order)

	msus, err := listMsuPackages(dir)
	if err != nil {
		t.Fatalf("listMsuPackages: %v", err)
	}

	_, err = planMsuInstallOrder(dir, msus)
	if err == nil {
		t.Fatal("expected error for unknown order entry")
	}
	if !strings.Contains(err.Error(), "non-existent") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// order 重复列出：报错。
func TestPlanMsuInstallOrder_DuplicateEntry(t *testing.T) {
	order := "Windows6.1-KB2990941-v3-x86.msu\n" +
		"Windows6.1-KB2990941-v3-x86.msu\n"

	dir := setupMsuDir(t, []string{
		"Windows6.1-KB2990941-v3-x86.msu",
	}, &order)

	msus, err := listMsuPackages(dir)
	if err != nil {
		t.Fatalf("listMsuPackages: %v", err)
	}

	_, err = planMsuInstallOrder(dir, msus)
	if err == nil {
		t.Fatal("expected error for duplicate order entry")
	}
	if !strings.Contains(err.Error(), "more than once") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// 已安装判断：以 DISM 返回码为第一判据（0x800F081E，
// Go 侧表现为 int32 截断后的 -2146498530）；返回码非该值时，
// 兜底核对输出中的已安装错误码。返回码 0 为本次安装成功，
// 不属于已安装。
func TestIsMsuAlreadyInstalled(t *testing.T) {
	cases := []struct {
		name   string
		exit   int
		output string
		want   bool
	}{
		{
			name:   "exit 0 means freshly installed, not already installed",
			exit:   0,
			output: "",
			want:   false,
		},
		{
			name:   "dism exit code 0x800F081E as negative int32",
			exit:   -2146498530, // int32(0x800F081E)
			output: "Error: 0x800F081E\nThe component store is not in a valid state.",
			want:   true,
		},
		{
			name:   "dism exit code 0x800F081E as unsigned value",
			exit:   int(0x800F081E), // 2148468766
			output: "Error: 0x800F081E",
			want:   true,
		},
		{
			name:   "lowercase token in output as fallback",
			exit:   1,
			output: "dism returned 0x800f081e",
			want:   true,
		},
		{
			name:   "generic failure without token",
			exit:   1,
			output: "Error: 0x80070005 Access is denied.",
			want:   false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := isMsuAlreadyInstalled(c.exit, c.output)
			if got != c.want {
				t.Fatalf("isMsuAlreadyInstalled(%d, %q) = %v, want %v",
					c.exit, c.output, got, c.want)
			}
		})
	}
}
