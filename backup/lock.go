package backup

// fileLock is an exclusive OS-level lock held on a file. Unlock releases
// the lock and closes the underlying file handle.
type fileLock interface {
	Unlock() error
}

// acquireFileLock attempts to take an exclusive, non-blocking lock on the
// file at path, creating the file if necessary. It returns
// ErrEngineAlreadyRunning when another process already holds the lock.
// Platform implementations live in lock_windows.go and lock_other.go.
//
// acquireFileLock(path string) (fileLock, error)
