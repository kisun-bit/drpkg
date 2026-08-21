//go:build !windows

package backup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// unixFileLock is an exclusive flock-based lock on an open file.
type unixFileLock struct {
	f *os.File
}

// acquireFileLock takes an exclusive non-blocking flock on the file at
// path (creating it if needed). It returns ErrEngineAlreadyRunning when
// another process holds the lock.
func acquireFileLock(path string) (fileLock, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("backup: open lock file: %v", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrEngineAlreadyRunning
		}
		return nil, fmt.Errorf("backup: lock file: %v", err)
	}

	// Record the holder's pid for diagnostics.
	_ = f.Truncate(0)
	_, _ = f.Seek(0, 0)
	_, _ = fmt.Fprintf(f, "pid=%d\n", os.Getpid())

	return &unixFileLock{f: f}, nil
}

// Unlock releases the lock and closes the file.
func (l *unixFileLock) Unlock() error {
	err := syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	closeErr := l.f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
