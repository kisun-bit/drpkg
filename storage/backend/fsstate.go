package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// fsState 记录以目录为访问根的文件系统访问器的公共状态，并提供与具体介质
// 无关的文件操作。localfs 直接内嵌 fsState，cifs/nfs 再通过 netFs 内嵌。
type fsState struct {
	mu     sync.Mutex
	status Status
	root   string
}

// newFSState 返回初始状态为离线的文件系统访问状态。
func newFSState() fsState {
	return fsState{status: StatusOffline}
}

// Status 返回当前状态。
func (s *fsState) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// OpenFile 以只读方式打开访问根下的已有文件。
func (s *fsState) OpenFile(path string) (*StorageFile, error) {
	root, err := s.currentRoot()
	if err != nil {
		return nil, err
	}
	full, err := storagePath(root, path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, err
	}
	return &StorageFile{name: path, f: f}, nil
}

// CreateFile 创建或打开访问根下的相对路径文件。
func (s *fsState) CreateFile(path string) (*StorageFile, error) {
	root, err := s.currentRoot()
	if err != nil {
		return nil, err
	}
	full, err := storagePath(root, path)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(full, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	return &StorageFile{name: path, f: f}, nil
}

// RemoveAll 递归删除访问根下的相对路径。
func (s *fsState) RemoveAll(path string) error {
	root, err := s.currentRoot()
	if err != nil {
		return err
	}
	full, err := storagePath(root, path)
	if err != nil {
		return err
	}
	return os.RemoveAll(full)
}

// currentRoot 返回当前访问根目录，要求介质处于在线状态。
func (s *fsState) currentRoot() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.root == "" || s.status != StatusOnline {
		return "", fmt.Errorf("backend: storage is not online")
	}
	return s.root, nil
}
