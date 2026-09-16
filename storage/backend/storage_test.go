package backend

import (
	"path/filepath"
	"testing"
)

// dummyConfig 是测试用的、未注册到注册表的介质类型。
type dummyConfig struct{}

func (dummyConfig) Type() Type { return Type("dummy") }

func TestNewDispatch(t *testing.T) {
	acc, err := New(LocalFsConfig{SubPath: t.TempDir()})
	if err != nil {
		t.Fatalf("New(LocalFsConfig): %v", err)
	}
	if acc.Type() != StorageTypeLocalFilesystem {
		t.Fatalf("Type() = %s, want %s", acc.Type(), StorageTypeLocalFilesystem)
	}

	s3, err := New(S3Config{Endpoint: "s3.example.com", Bucket: "bkt"})
	if err != nil {
		t.Fatalf("New(S3Config): %v", err)
	}
	if s3.Type() != StorageTypeS3 {
		t.Fatalf("s3 Type() = %s, want %s", s3.Type(), StorageTypeS3)
	}
	if s3.Status() != StatusOffline {
		t.Fatalf("s3 initial Status() = %s, want offline", s3.Status())
	}
}

func TestNewRejects(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatalf("New(nil) should fail")
	}
	if _, err := New(dummyConfig{}); err == nil {
		t.Fatalf("New(dummyConfig) should fail (unregistered type)")
	}
}

func TestAccessorInterfaceSplit(t *testing.T) {
	acc, err := New(LocalFsConfig{SubPath: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := acc.(Lifecycle); !ok {
		t.Fatalf("Accessor should satisfy Lifecycle")
	}
	if _, ok := acc.(FileStore); !ok {
		t.Fatalf("Accessor should satisfy FileStore")
	}
}

func TestStoragePath(t *testing.T) {
	root := filepath.Join("tmp", "storage")

	// 空路径返回根目录。
	if got, err := storagePath(root, ""); err != nil || got != root {
		t.Fatalf("storagePath(root, \"\") = %q, %v; want %q, nil", got, err, root)
	}

	// 正常相对路径。
	if got, err := storagePath(root, "a/b/c"); err != nil || got != filepath.Join(root, "a", "b", "c") {
		t.Fatalf("storagePath(root, \"a/b/c\") = %q, %v; want %q, nil",
			got, err, filepath.Join(root, "a", "b", "c"))
	}

	// 逃逸路径被拒绝。
	for _, p := range []string{"..", "../x", "a/../..", filepath.Join("a", "..", "..", "b")} {
		if _, err := storagePath(root, p); err == nil {
			t.Fatalf("storagePath(root, %q) should reject escape", p)
		}
	}

	// 绝对路径被拒绝。
	abs := filepath.Join(t.TempDir(), "x", "y")
	if _, err := storagePath(root, abs); err == nil {
		t.Fatalf("storagePath(root, %q) should reject absolute path", abs)
	}
}

func TestConfigTypes(t *testing.T) {
	if (LocalFsConfig{}).Type() != StorageTypeLocalFilesystem {
		t.Fatalf("LocalFsConfig.Type() mismatch")
	}
	if (CIFSConfig{}).Type() != StorageTypeCIFS {
		t.Fatalf("CIFSConfig.Type() mismatch")
	}
	if (NFSConfig{}).Type() != StorageTypeNFS {
		t.Fatalf("NFSConfig.Type() mismatch")
	}
	if (S3Config{}).Type() != StorageTypeS3 {
		t.Fatalf("S3Config.Type() mismatch")
	}
}
