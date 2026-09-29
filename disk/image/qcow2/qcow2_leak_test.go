//go:build windows || linux

package qcow2

import (
	"os"
	"path/filepath"
	"testing"
)

const leakTestDiskSize = uint64(1 << 20) // 1 MiB

// TestCreateImageFromBackingReleasesBackingHandle
//
// CreateImageFromBacking 打开 backing 仅用于读取虚拟磁盘大小, 读取后必须立即释放,
// 否则该只读句柄会一直存活 (Windows 上会阻塞 backing 文件的删除/重命名)。
func TestCreateImageFromBackingReleasesBackingHandle(t *testing.T) {
	dir := t.TempDir()
	backing := filepath.Join(dir, "backing.qcow2")
	child := filepath.Join(dir, "child.qcow2")

	factory := NoCacheImageFactory()

	backingImage, err := factory.CreateImage(backing, leakTestDiskSize)
	if err != nil {
		t.Fatalf("create backing: %v", err)
	}
	if err := backingImage.Close(); err != nil {
		t.Fatalf("close backing: %v", err)
	}

	childImage, err := factory.CreateImageFromBacking(child, backing)
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	if err := childImage.Close(); err != nil {
		t.Fatalf("close child: %v", err)
	}

	requireFileReleased(t, backing)
	requireFileReleased(t, child)
}

// TestOpenImageReleasesBackingChainAfterClose
//
// OpenImage 会打开整条 backing chain, 正常 Close 后必须释放整条链的所有句柄。
func TestOpenImageReleasesBackingChainAfterClose(t *testing.T) {
	dir := t.TempDir()
	backing := filepath.Join(dir, "backing.qcow2")
	child := filepath.Join(dir, "child.qcow2")

	factory := NoCacheImageFactory()

	b, err := factory.CreateImage(backing, leakTestDiskSize)
	if err != nil {
		t.Fatalf("create backing: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("close backing: %v", err)
	}

	c, err := factory.CreateImageFromBacking(child, backing)
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close child: %v", err)
	}

	img, err := factory.OpenImage(child, 10)
	if err != nil {
		t.Fatalf("open child: %v", err)
	}
	if err := img.Close(); err != nil {
		t.Fatalf("close opened image: %v", err)
	}

	requireFileReleased(t, backing)
	requireFileReleased(t, child)
}

// TestImageFromFileErrorReleasesBackingHandle
//
// imageFromFile 在「已打开 backing 之后」的后续校验失败时, 出错路径也必须释放
// 已打开的 backing 句柄与当前文件句柄。
func TestImageFromFileErrorReleasesBackingHandle(t *testing.T) {
	dir := t.TempDir()
	backing := filepath.Join(dir, "backing.qcow2")
	child := filepath.Join(dir, "child.qcow2")

	factory := NoCacheImageFactory()

	b, err := factory.CreateImage(backing, leakTestDiskSize)
	if err != nil {
		t.Fatalf("create backing: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("close backing: %v", err)
	}

	c, err := factory.CreateImageFromBacking(child, backing)
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close child: %v", err)
	}

	// 破坏 child 头部的 refcount_table_offset (header offset 48), 使其在打开 backing 之后、
	// 读取/校验 refcount 表时失败。
	corruptRefCountTableOffset(t, child)

	if _, err := factory.OpenImage(child, 10); err == nil {
		t.Fatalf("expected OpenImage to fail after corrupting refcount table offset")
	}

	requireFileReleased(t, backing)
	requireFileReleased(t, child)
}

// corruptRefCountTableOffset 将 refcount_table_offset (header offset 48) 写为远超文件大小的值。
func corruptRefCountTableOffset(t *testing.T, imagePath string) {
	t.Helper()
	f, err := os.OpenFile(imagePath, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open image for corruption: %v", err)
	}
	defer f.Close()

	if err := writeUint64At(f, uint64(1)<<40, 48); err != nil {
		t.Fatalf("corrupt refcount table offset: %v", err)
	}
}
