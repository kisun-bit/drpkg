package backend

import (
	"fmt"
	"os"
)

// netFs 是 cifs/nfs 等需要挂载的网络文件系统访问器共享的状态。
//
// 各网络文件系统介质通过内嵌 netFs 复用文件操作与离线检测，仅需实现
// Type/Online/Offline；具体的挂载/卸载逻辑由平台文件提供（mount_*.go）。
type netFs struct {
	fsState

	mountDir string
	mounted  bool
}

// Test 检测挂载点当前是否可访问，不改变生命周期状态。
func (n *netFs) Test() error {
	n.mu.Lock()
	root := n.root
	n.mu.Unlock()

	if root == "" {
		return fmt.Errorf("backend: storage is offline")
	}
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("backend: %s is not a directory", root)
	}
	return nil
}
