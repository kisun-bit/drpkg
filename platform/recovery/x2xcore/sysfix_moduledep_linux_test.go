package x2xcore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kisun-bit/drpkg/platform/info"
)

// modules.dep 已存在时不应触发 depmod 重建；测试环境没有可 chroot 的 rootfs，
// 若跳过存在性检查会走到 executeWithChroot 并失败，从而暴露回归。
func TestEnsureModulesDep_AlreadyExists(t *testing.T) {
	root := t.TempDir()
	ver := "3.10.0-1160.el7.x86_64"

	depFile := filepath.Join(root, "lib/modules", ver, "modules.dep")
	if err := os.MkdirAll(filepath.Dir(depFile), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(depFile, []byte(""), 0o644); err != nil {
		t.Fatalf("write modules.dep: %v", err)
	}

	fixer := &linuxSystemFixer{offsys: offlineSystem{root: root}}
	k := kernel{LinuxKernel: info.LinuxKernel{Name: ver}}

	if err := fixer.ensureModulesDep(k); err != nil {
		t.Fatalf("ensureModulesDep: %v", err)
	}
}
