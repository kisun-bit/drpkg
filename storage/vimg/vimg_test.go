package vimg

import (
	"bytes"
	"fmt"
	"os"
	"sync"
	"testing"
)

func newTestDir(t *testing.T) string {
	dir := `d:\vimgtest`
	fmt.Println(dir)
	os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func newImage(t *testing.T, dir string) (*Image, *VImg) {
	m := NewManager()

	v, err := m.Create(CreateOptions{
		Dir:         dir,
		VirtualSize: 1024 * 1024,
		ClusterSize: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}

	metaPath, err := getMetaPath(v)
	if err != nil {
		t.Fatal(err)
	}

	img, err := m.Open(metaPath)
	if err != nil {
		t.Fatal(err)
	}

	return img, v
}

/************** 基础读写 **************/

func TestBasicReadWrite(t *testing.T) {
	dir := newTestDir(t)
	img, _ := newImage(t, dir)
	defer (*img).Close()

	data := []byte("hello vimg")

	if err := (*img).WriteAt(data, 0); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, len(data))
	if err := (*img).ReadAt(buf, 0); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(data, buf) {
		t.Fatalf("data mismatch: %v vs %v", data, buf)
	}
}

/************** 跨 cluster 写 **************/

func TestCrossClusterWrite(t *testing.T) {
	dir := newTestDir(t)
	img, _ := newImage(t, dir)
	defer (*img).Close()

	data := make([]byte, 8000) // 跨 4096
	for i := range data {
		data[i] = byte(i % 256)
	}

	if err := (*img).WriteAt(data, 0); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, len(data))
	if err := (*img).ReadAt(buf, 0); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(data, buf) {
		t.Fatal("cross cluster mismatch")
	}
}

/************** 稀疏读 **************/

func TestSparseRead(t *testing.T) {
	dir := newTestDir(t)
	img, _ := newImage(t, dir)
	defer (*img).Close()

	buf := make([]byte, 4096)

	if err := (*img).ReadAt(buf, 0); err != nil {
		t.Fatal(err)
	}

	for _, b := range buf {
		if b != 0 {
			t.Fatal("expected zero")
		}
	}
}

/************** backing chain **************/

func TestBackingChain(t *testing.T) {
	dir := newTestDir(t)
	m := NewManager()

	// base
	baseV, err := m.Create(CreateOptions{
		Dir:         dir,
		VirtualSize: 1 << 20,
		ClusterSize: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	baseMeta, err := getMetaPath(baseV)
	if err != nil {
		t.Fatal(err)
	}
	baseImg, err := m.Open(baseMeta)
	if err != nil {
		t.Fatal(err)
	}

	data := []byte("base data")
	if err := (*baseImg).WriteAt(data, 0); err != nil {
		t.Fatal(err)
	}

	// child
	childV, err := m.CreateFromBacking(CreateFromBackingOptions{
		CreateOptions: CreateOptions{
			Dir:         dir,
			VirtualSize: 1 << 20,
			ClusterSize: 4096,
		},
		BackingGuid: baseV.Guid,
	})
	if err != nil {
		t.Fatal(err)
	}

	childMeta, err := getMetaPath(childV)
	if err != nil {
		t.Fatal(err)
	}
	childImg, err := m.Open(childMeta)
	if err != nil {
		t.Fatal(err)
	}
	defer (*childImg).Close()

	buf := make([]byte, len(data))
	if err := (*childImg).ReadAt(buf, 0); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(buf, data) {
		t.Fatal("backing read failed")
	}
}

/************** commit **************/

func TestCommit(t *testing.T) {
	dir := newTestDir(t)
	m := NewManager()

	baseV, err := m.Create(CreateOptions{
		Dir:         dir,
		VirtualSize: 1 << 20,
		ClusterSize: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	baseMeta, err := getMetaPath(baseV)
	if err != nil {
		t.Fatal(err)
	}
	baseImg, err := m.Open(baseMeta)
	if err != nil {
		t.Fatal(err)
	}

	childV, err := m.CreateFromBacking(CreateFromBackingOptions{
		CreateOptions: CreateOptions{
			Dir:         dir,
			VirtualSize: 1 << 20,
			ClusterSize: 4096,
		},
		BackingGuid: baseV.Guid,
	})
	if err != nil {
		t.Fatal(err)
	}

	childMeta, err := getMetaPath(childV)
	if err != nil {
		t.Fatal(err)
	}
	childImg, err := m.Open(childMeta)
	if err != nil {
		t.Fatal(err)
	}

	data := []byte("commit data")
	if err := (*childImg).WriteAt(data, 0); err != nil {
		t.Fatal(err)
	}

	if err := (*childImg).Commit(); err != nil {
		t.Fatal(err)
	}

	// 必须重新加载 baseImg 的索引，因为 childImg.Commit 修改了底层文件
	// 但 baseImg 是一个独立的对象，其内存中的 index map 不会自动更新
	if bi, ok := (*baseImg).(*image); ok {
		if err := bi.loadIndex(); err != nil {
			t.Fatal(err)
		}
	}

	buf := make([]byte, len(data))
	if err := (*baseImg).ReadAt(buf, 0); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(buf, data) {
		t.Fatal("commit failed")
	}
}

/************** rebase **************/

func TestRebase(t *testing.T) {
	dir := newTestDir(t)
	m := NewManager()

	base1, err := m.Create(CreateOptions{
		Dir:         dir,
		VirtualSize: 1 << 20,
		ClusterSize: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}

	base2, err := m.Create(CreateOptions{
		Dir:         dir,
		VirtualSize: 1 << 20,
		ClusterSize: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}

	child, err := m.CreateFromBacking(CreateFromBackingOptions{
		CreateOptions: CreateOptions{
			Dir:         dir,
			VirtualSize: 1 << 20,
			ClusterSize: 4096,
		},
		BackingGuid: base1.Guid,
	})
	if err != nil {
		t.Fatal(err)
	}

	childMeta, err := getMetaPath(child)
	if err != nil {
		t.Fatal(err)
	}
	img, err := m.Open(childMeta)
	if err != nil {
		t.Fatal(err)
	}

	data := []byte("rebase test")
	if err := (*img).WriteAt(data, 0); err != nil {
		t.Fatal(err)
	}

	base2Meta, err := getMetaPath(base2)
	if err != nil {
		t.Fatal(err)
	}
	if err := (*img).Rebase(base2Meta); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, len(data))
	if err := (*img).ReadAt(buf, 0); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(buf, data) {
		t.Fatal("rebase data lost")
	}
}

/************** 并发测试 **************/

func TestConcurrentAccess(t *testing.T) {
	dir := newTestDir(t)
	img, _ := newImage(t, dir)
	defer (*img).Close()

	wg := sync.WaitGroup{}

	for i := 0; i < 10; i++ {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			data := []byte{byte(i)}

			for j := 0; j < 100; j++ {
				offset := uint64(i * 4096)
				(*img).WriteAt(data, offset)

				buf := make([]byte, 1)
				(*img).ReadAt(buf, offset)
			}
		}(i)
	}

	wg.Wait()
}

/************** 顺序写压力 **************/

func TestSequentialWrite(t *testing.T) {
	dir := newTestDir(t)
	img, _ := newImage(t, dir)
	defer (*img).Close()

	data := make([]byte, 1024*1024) // 1MB
	for i := range data {
		data[i] = byte(i % 255)
	}

	if err := (*img).WriteAt(data, 0); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, len(data))
	if err := (*img).ReadAt(buf, 0); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(buf, data) {
		t.Fatal("sequential mismatch")
	}
}

/************** 链式一致性测试 (A->B->C) **************/

func TestChainConsistency(t *testing.T) {
	dir := newTestDir(t)
	m := NewManager()

	// 1. 创建 A (Base)
	vA, err := m.Create(CreateOptions{
		Dir:         dir,
		VirtualSize: 1 << 20,
		ClusterSize: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	pathA, _ := getMetaPath(vA)
	imgA, _ := m.Open(pathA)
	defer (*imgA).Close()

	// 2. 创建 B (Backing A)
	vB, err := m.CreateFromBacking(CreateFromBackingOptions{
		CreateOptions: CreateOptions{
			Dir:         dir,
			VirtualSize: 1 << 20,
			ClusterSize: 4096,
		},
		BackingGuid: vA.Guid,
	})
	if err != nil {
		t.Fatal(err)
	}
	pathB, _ := getMetaPath(vB)
	imgB, _ := m.Open(pathB)
	defer (*imgB).Close()

	// 3. 创建 C (Backing B)
	vC, err := m.CreateFromBacking(CreateFromBackingOptions{
		CreateOptions: CreateOptions{
			Dir:         dir,
			VirtualSize: 1 << 20,
			ClusterSize: 4096,
		},
		BackingGuid: vB.Guid,
	})
	if err != nil {
		t.Fatal(err)
	}
	pathC, _ := getMetaPath(vC)
	imgC, _ := m.Open(pathC)
	defer (*imgC).Close()

	// 4. 写入数据
	dataA := []byte("Data in A")
	dataB := []byte("Data in B")
	dataC := []byte("Data in C")

	(*imgA).WriteAt(dataA, 0)
	(*imgB).WriteAt(dataB, 4096)
	(*imgC).WriteAt(dataC, 8192)

	// 5. 初始检查
	check := func(img *Image, name string) {
		buf := make([]byte, 9)
		// 检查 A 的数据
		(*img).ReadAt(buf, 0)
		if !bytes.Equal(buf, dataA) {
			t.Fatalf("%s: dataA mismatch, got %s", name, string(buf))
		}
		// 检查 B 的数据
		(*img).ReadAt(buf, 4096)
		if !bytes.Equal(buf, dataB) {
			t.Fatalf("%s: dataB mismatch, got %s", name, string(buf))
		}
		// 检查 C 的数据
		(*img).ReadAt(buf, 8192)
		if !bytes.Equal(buf, dataC) {
			t.Fatalf("%s: dataC mismatch, got %s", name, string(buf))
		}
	}

	check(imgC, "Initial C")

	// 6. 将 B commit 到 A
	if err := (*imgB).Commit(); err != nil {
		t.Fatalf("Commit B to A failed: %v", err)
	}

	// 必须重新加载 imgA 的索引，因为文件被修改了
	if bi, ok := (*imgA).(*image); ok {
		bi.loadIndex()
	}

	// 7. 将 C rebase 到 A
	if err := (*imgC).Rebase(pathA); err != nil {
		t.Fatalf("Rebase C to A failed: %v", err)
	}

	// 8. 删除 B
	if err := m.Delete(pathB); err != nil {
		t.Fatalf("Delete B failed: %v", err)
	}

	// 9. 最终一致性检查
	check(imgC, "Final C after B deleted")

	fmt.Println("Chain consistency test passed!")
}
