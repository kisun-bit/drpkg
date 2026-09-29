//go:build linux

package qcow2

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// requireFileReleased 断言给定文件不再被任何文件描述符持有。
//
// Linux 允许删除仍被打开的文件, 因此改用 /proc/self/fd 的符号链接来精确检测。
// 泄漏的句柄若仅依赖 os.File 的 finalizer 关闭, 这里通过 runtime.GC 给其执行机会;
// 但我们的修复是显式 Close, 因此修复后应立即释放、无需等待 GC。
func requireFileReleased(t *testing.T, path string) {
	t.Helper()

	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("abs path %s: %v", path, err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		runtime.GC()
		if !hasOpenFD(abs) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("file %s is still held open", path)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// hasOpenFD 检查 /proc/self/fd 中是否存在指向目标文件的文件描述符。
func hasOpenFD(abs string) bool {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		// 无法判定时保守视为仍持有, 触发重试直至超时。
		return true
	}
	for _, entry := range entries {
		link, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
		if err == nil && link == abs {
			return true
		}
	}
	return false
}
