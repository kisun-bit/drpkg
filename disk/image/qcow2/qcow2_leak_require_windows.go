//go:build windows

package qcow2

import (
	"os"
	"testing"
)

// requireFileReleased 通过重命名文件断言没有句柄仍持有该文件。
//
// Go 在 Windows 上打开文件时不带 FILE_SHARE_DELETE, 因此若存在未关闭的句柄,
// os.Rename 会因共享冲突而失败。
func requireFileReleased(t *testing.T, path string) {
	t.Helper()

	moved := path + ".released-check"
	if err := os.Rename(path, moved); err != nil {
		t.Fatalf("file %s is still held open: %v", path, err)
	}
	if err := os.Rename(moved, path); err != nil {
		t.Fatalf("rename back %s: %v", path, err)
	}
}
