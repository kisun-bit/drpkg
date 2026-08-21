//go:build windows

package backup

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// windowsFileLock is an exclusive LockFileEx-based lock on a file handle.
type windowsFileLock struct {
	h windows.Handle
}

// acquireFileLock takes an exclusive non-blocking LockFileEx lock on the
// file at path (creating it if needed). It returns
// ErrEngineAlreadyRunning when another process holds the lock.
func acquireFileLock(path string) (fileLock, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(name,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, fmt.Errorf("backup: open lock file: %v", err)
	}

	var ol windows.Overlapped
	err = windows.LockFileEx(h,
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, &ol)
	if err != nil {
		windows.CloseHandle(h)
		if err == windows.ERROR_LOCK_VIOLATION {
			return nil, ErrEngineAlreadyRunning
		}
		return nil, fmt.Errorf("backup: lock file: %v", err)
	}

	// Record the holder's pid for diagnostics.
	_, _ = windows.SetFilePointer(h, 0, nil, windows.FILE_BEGIN)
	_ = windows.SetEndOfFile(h)
	_, _ = windows.Write(h, []byte(fmt.Sprintf("pid=%d\n", os.Getpid())))

	return &windowsFileLock{h: h}, nil
}

// Unlock releases the lock and closes the handle.
func (l *windowsFileLock) Unlock() error {
	var ol windows.Overlapped
	unlockErr := windows.UnlockFileEx(l.h, 0, 1, 0, &ol)
	closeErr := windows.CloseHandle(l.h)
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
