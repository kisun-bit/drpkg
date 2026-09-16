package backend

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalFsLifecycle(t *testing.T) {
	root := t.TempDir()

	acc, err := New(LocalFsConfig{SubPath: root})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if acc.Status() != StatusOffline {
		t.Fatalf("initial Status() = %s, want %s", acc.Status(), StatusOffline)
	}

	// 未 Online 时文件操作应失败。
	if _, err := acc.CreateFile("a/b"); err == nil {
		t.Fatalf("CreateFile before Online should fail")
	}
	if err := acc.RemoveAll("a"); err == nil {
		t.Fatalf("RemoveAll before Online should fail")
	}

	if err := acc.Online(); err != nil {
		t.Fatalf("Online: %v", err)
	}
	if acc.Status() != StatusOnline {
		t.Fatalf("Status() after Online = %s, want %s", acc.Status(), StatusOnline)
	}
	if err := acc.Test(); err != nil {
		t.Fatalf("Test: %v", err)
	}

	// 创建并写入。
	f, err := acc.CreateFile("dir/file.txt")
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	if f.Name() != "dir/file.txt" {
		t.Fatalf("Name() = %q, want %q", f.Name(), "dir/file.txt")
	}
	if _, err := f.Write([]byte("hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// 重新打开并读回。
	f2, err := acc.CreateFile("dir/file.txt")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	buf := make([]byte, 5)
	n, err := f2.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if n != 5 || string(buf) != "hello" {
		t.Fatalf("Read got %q (%d bytes), want %q", buf[:n], n, "hello")
	}
	_ = f2.Close()

	// 递归删除。
	if err := acc.RemoveAll("dir"); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "dir")); !os.IsNotExist(err) {
		t.Fatalf("dir should be removed, stat err=%v", err)
	}

	// 离线后文件操作应失败。
	if err := acc.Offline(); err != nil {
		t.Fatalf("Offline: %v", err)
	}
	if acc.Status() != StatusOffline {
		t.Fatalf("Status() after Offline = %s, want %s", acc.Status(), StatusOffline)
	}
	if _, err := acc.CreateFile("x"); err == nil {
		t.Fatalf("CreateFile after Offline should fail")
	}
}

func TestLocalFsOpenFile(t *testing.T) {
	root := t.TempDir()
	acc, err := New(LocalFsConfig{SubPath: root})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// localfs 访问器应满足 OpenFile 接口。
	reader, ok := acc.(OpenFile)
	if !ok {
		t.Fatalf("localfs accessor should satisfy OpenFile")
	}

	if err := acc.Online(); err != nil {
		t.Fatalf("Online: %v", err)
	}

	// 写入一个文件。
	f, err := acc.CreateFile("data.bin")
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	if _, err := f.Write([]byte("read me back")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// 只读打开并读回。
	rf, err := reader.OpenFile("data.bin")
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	buf := make([]byte, len("read me back"))
	if _, err := io.ReadFull(rf, buf); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(buf) != "read me back" {
		t.Fatalf("read %q, want %q", buf, "read me back")
	}
	st, err := rf.Stat()
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if st.Size() != int64(len("read me back")) {
		t.Fatalf("Size = %d, want %d", st.Size(), len("read me back"))
	}
	if err := rf.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// 打开不存在的文件应报错。
	if _, err := reader.OpenFile("nope"); err == nil {
		t.Fatalf("OpenFile(nonexistent) should fail")
	}
}
