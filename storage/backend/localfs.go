package backend

import (
	"fmt"
	"os"
)

// LocalFsConfig 描述本地文件系统存储的访问信息。
type LocalFsConfig struct {
	// FileSystemUUID 文件系统 UUID，用于标识介质。
	FileSystemUUID string
	// DevicePath 设备路径，如 "/dev/mapper/centos-data"。
	DevicePath string
	// SubPath 备份数据存储目录。
	SubPath string
}

// Type 返回本地文件系统介质类型。
func (LocalFsConfig) Type() Type { return StorageTypeLocalFilesystem }

// localFsAccessor 实现本地文件系统存储的访问能力。
//
// 本地文件系统无需挂载，直接以 SubPath 作为访问根目录。
type localFsAccessor struct {
	fsState
	cfg LocalFsConfig
}

func init() {
	Register(StorageTypeLocalFilesystem, newLocalFs)
}

func newLocalFs(cfg Config) (Accessor, error) {
	c, ok := cfg.(LocalFsConfig)
	if !ok {
		return nil, fmt.Errorf("backend: localfs requires LocalFsConfig, got %T", cfg)
	}
	return &localFsAccessor{fsState: newFSState(), cfg: c}, nil
}

func (a *localFsAccessor) Type() Type { return StorageTypeLocalFilesystem }

// Online 为本地文件系统建立访问能力，仅需确保存储目录存在。
func (a *localFsAccessor) Online() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.cfg.SubPath == "" {
		a.status = StatusError
		return fmt.Errorf("backend: localfs requires SubPath")
	}
	if err := os.MkdirAll(a.cfg.SubPath, 0o755); err != nil {
		a.status = StatusError
		return fmt.Errorf("backend: create storage dir %s: %w", a.cfg.SubPath, err)
	}

	a.root = a.cfg.SubPath
	a.status = StatusOnline
	return nil
}

// Offline 释放本地文件系统的访问能力。
func (a *localFsAccessor) Offline() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.status == StatusOffline {
		return nil
	}
	a.root = ""
	a.status = StatusOffline
	return nil
}

// Test 检测存储目录是否可访问。
func (a *localFsAccessor) Test() error {
	a.mu.Lock()
	root := a.root
	a.mu.Unlock()

	if root == "" {
		root = a.cfg.SubPath
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
