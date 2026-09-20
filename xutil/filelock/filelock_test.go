package filelock

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAcquireExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")

	l, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	// A second acquire on the same file must fail while it is held.
	if _, err := Acquire(path); !errors.Is(err, ErrLocked) {
		t.Fatalf("second Acquire = %v, want ErrLocked", err)
	}

	if err := l.Unlock(); err != nil {
		t.Fatalf("Unlock: %v", err)
	}

	// After release the lock can be taken again.
	l2, err := Acquire(path)
	if err != nil {
		t.Fatalf("re-Acquire: %v", err)
	}
	if err := l2.Unlock(); err != nil {
		t.Fatalf("second Unlock: %v", err)
	}
}

func TestAcquireCreatesParentDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "c.lock")

	l, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer l.Unlock()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("lock file not created: %v", err)
	}
}

func TestAcquireRecordsHolderPID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pid.lock")

	l, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := l.Unlock(); err != nil {
		t.Fatalf("Unlock: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.HasPrefix(string(data), "pid=") {
		t.Fatalf("lock file content = %q, want pid prefix", string(data))
	}
}
