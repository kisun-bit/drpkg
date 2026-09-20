// Package filelock provides a cross-platform exclusive, non-blocking file
// lock. A lock is held on a regular file and claimed by Acquire; it is
// released (and the underlying handle closed) by Lock.Unlock.
package filelock

import "errors"

// Lock is an exclusive OS-level lock held on a file. Unlock releases the
// lock and closes the underlying file handle.
type Lock interface {
	Unlock() error
}

// ErrLocked is returned by Acquire when another holder already has an
// exclusive lock on the target file.
var ErrLocked = errors.New("filelock: file is already locked")

// Acquire takes an exclusive, non-blocking lock on the file at path,
// creating the file and any missing parent directories as necessary. It
// returns ErrLocked when the lock is already held elsewhere; the returned
// Lock must be released with Unlock.
//
// Platform implementations live in filelock_windows.go and
// filelock_other.go.
func Acquire(path string) (Lock, error) {
	return acquire(path)
}
