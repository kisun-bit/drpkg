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

/************** 加密读写 **************/

func TestEncryptedReadWrite(t *testing.T) {
	dir := newTestDir(t)
	m := NewManager()

	v, err := m.Create(CreateOptions{
		Dir:           dir,
		VirtualSize:   1 << 20,
		ClusterSize:   4096,
		Encryption:    EncryptionAES256,
		EncryptionKey: "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
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
	defer (*img).Close()

	data := []byte("this is encrypted cluster payload")
	if err := (*img).WriteAt(data, 123); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, len(data))
	if err := (*img).ReadAt(buf, 123); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(buf, data) {
		t.Fatalf("encrypted read mismatch: got=%q want=%q", string(buf), string(data))
	}

	dataPath := replaceExt(metaPath, ".DATA")
	raw, err := os.ReadFile(dataPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, data) {
		t.Fatal("raw DATA file unexpectedly contains plaintext payload")
	}
}

/************** rebase 保语义 **************/

func TestRebasePreservesBackingData(t *testing.T) {
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
	base1Meta, _ := getMetaPath(base1)
	base1Img, err := m.Open(base1Meta)
	if err != nil {
		t.Fatal(err)
	}
	defer (*base1Img).Close()

	baseData := []byte("base1-only-data")
	if err := (*base1Img).WriteAt(baseData, 0); err != nil {
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
	base2Meta, _ := getMetaPath(base2)

	child, err := m.CreateFromBacking(CreateFromBackingOptions{
		CreateOptions: CreateOptions{
			Dir:         dir,
			VirtualSize: 1 << 20,
			ClusterSize: 4096,
		},
		BackingMetaPath: base1Meta,
	})
	if err != nil {
		t.Fatal(err)
	}
	childMeta, _ := getMetaPath(child)
	childImg, err := m.Open(childMeta)
	if err != nil {
		t.Fatal(err)
	}
	defer (*childImg).Close()

	buf := make([]byte, len(baseData))
	if err := (*childImg).ReadAt(buf, 0); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf, baseData) {
		t.Fatalf("before rebase mismatch: got=%q want=%q", string(buf), string(baseData))
	}

	if err := (*childImg).Rebase(base2Meta); err != nil {
		t.Fatal(err)
	}

	after := make([]byte, len(baseData))
	if err := (*childImg).ReadAt(after, 0); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, baseData) {
		t.Fatalf("rebase should preserve visible data: got=%q want=%q", string(after), string(baseData))
	}
}

/************** backing meta path **************/

func TestCreateFromBackingWithMetaPath(t *testing.T) {
	dir := newTestDir(t)
	m := NewManager()

	base, err := m.Create(CreateOptions{
		Dir:         dir,
		VirtualSize: 1 << 20,
		ClusterSize: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	baseMeta, _ := getMetaPath(base)
	baseImg, err := m.Open(baseMeta)
	if err != nil {
		t.Fatal(err)
	}
	defer (*baseImg).Close()

	data := []byte("backing by meta path")
	if err := (*baseImg).WriteAt(data, 0); err != nil {
		t.Fatal(err)
	}

	child, err := m.CreateFromBacking(CreateFromBackingOptions{
		CreateOptions: CreateOptions{
			Dir:         dir,
			VirtualSize: 1 << 20,
			ClusterSize: 4096,
		},
		BackingMetaPath: baseMeta,
	})
	if err != nil {
		t.Fatal(err)
	}
	childMeta, _ := getMetaPath(child)
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
		t.Fatalf("child read mismatch: got=%q want=%q", string(buf), string(data))
	}
}

/************** 非对齐尾分块 + backing 补齐 **************/

func TestTailPartialClusterCopyOnWriteFromBacking(t *testing.T) {
	const (
		clusterSize  = uint32(512)
		backingSize  = uint64(1048576)
		childSize    = uint64(1048500)
		tailDataSize = uint64(436) // child 最后一个 cluster 的有效字节数
	)

	dir := newTestDir(t)
	m := NewManager()

	backingV, err := m.Create(CreateOptions{
		Dir:         dir,
		VirtualSize: backingSize,
		ClusterSize: clusterSize,
	})
	if err != nil {
		t.Fatal(err)
	}
	backingMeta, _ := getMetaPath(backingV)
	backingImg, err := m.Open(backingMeta)
	if err != nil {
		t.Fatal(err)
	}
	defer (*backingImg).Close()

	lastClusterStart := (childSize / uint64(clusterSize)) * uint64(clusterSize)
	pattern := make([]byte, clusterSize)
	for i := range pattern {
		pattern[i] = byte((i + 17) % 251)
	}
	if err := (*backingImg).WriteAt(pattern, lastClusterStart); err != nil {
		t.Fatal(err)
	}

	childV, err := m.CreateFromBacking(CreateFromBackingOptions{
		CreateOptions: CreateOptions{
			Dir:         dir,
			VirtualSize: childSize,
			ClusterSize: clusterSize,
		},
		BackingMetaPath: backingMeta,
	})
	if err != nil {
		t.Fatal(err)
	}
	childMeta, _ := getMetaPath(childV)
	childImg, err := m.Open(childMeta)
	if err != nil {
		t.Fatal(err)
	}
	defer (*childImg).Close()

	before := make([]byte, int(tailDataSize))
	if err := (*childImg).ReadAt(before, lastClusterStart); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, pattern[:int(tailDataSize)]) {
		t.Fatal("initial tail data should come from backing")
	}

	if err := (*childImg).WriteAt([]byte{0x01}, lastClusterStart); err != nil {
		t.Fatal(err)
	}

	after := make([]byte, int(tailDataSize))
	if err := (*childImg).ReadAt(after, lastClusterStart); err != nil {
		t.Fatal(err)
	}

	if after[0] != 0x01 {
		t.Fatalf("first byte mismatch, got=%d want=%d", after[0], byte(0x01))
	}
	if !bytes.Equal(after[1:], pattern[1:int(tailDataSize)]) {
		t.Fatal("remaining valid bytes should be copied from backing")
	}
	if err := (*childImg).WriteAt([]byte{0xFF}, childSize); err == nil {
		t.Fatal("write at virtual size boundary should fail")
	}

	backingCheck := make([]byte, clusterSize)
	if err := (*backingImg).ReadAt(backingCheck, lastClusterStart); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(backingCheck, pattern) {
		t.Fatal("backing image should remain unchanged after child write")
	}
}

/************** A > B 时，超出 backing 区域应为 0 **************/

func TestChildLargerThanBackingReadsZeroBeyondBacking(t *testing.T) {
	const clusterSize = uint32(512)

	dir := newTestDir(t)
	m := NewManager()

	backingV, err := m.Create(CreateOptions{
		Dir:         dir,
		VirtualSize: 1024,
		ClusterSize: clusterSize,
	})
	if err != nil {
		t.Fatal(err)
	}
	backingMeta, _ := getMetaPath(backingV)

	childV, err := m.CreateFromBacking(CreateFromBackingOptions{
		CreateOptions: CreateOptions{
			Dir:         dir,
			VirtualSize: 2048,
			ClusterSize: clusterSize,
		},
		BackingMetaPath: backingMeta,
	})
	if err != nil {
		t.Fatal(err)
	}
	childMeta, _ := getMetaPath(childV)
	childImg, err := m.Open(childMeta)
	if err != nil {
		t.Fatal(err)
	}
	defer (*childImg).Close()

	zeroBuf := make([]byte, 128)
	if err := (*childImg).ReadAt(zeroBuf, 1536); err != nil {
		t.Fatal(err)
	}
	for i, b := range zeroBuf {
		if b != 0 {
			t.Fatalf("expected zero at %d, got %d", i, b)
		}
	}

	payload := []byte("data in area beyond backing size")
	if err := (*childImg).WriteAt(payload, 1536); err != nil {
		t.Fatal(err)
	}
	verify := make([]byte, len(payload))
	if err := (*childImg).ReadAt(verify, 1536); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(verify, payload) {
		t.Fatal("write/read mismatch in child-only extended region")
	}
}

/************** rebase 支持不同大小的 backing **************/

func TestRebaseWithDifferentBackingSizes(t *testing.T) {
	const clusterSize = uint32(512)

	dir := newTestDir(t)
	m := NewManager()

	baseLarge, err := m.Create(CreateOptions{
		Dir:         dir,
		VirtualSize: 2048,
		ClusterSize: clusterSize,
	})
	if err != nil {
		t.Fatal(err)
	}
	baseLargeMeta, _ := getMetaPath(baseLarge)
	baseLargeImg, err := m.Open(baseLargeMeta)
	if err != nil {
		t.Fatal(err)
	}
	defer (*baseLargeImg).Close()

	baseSmall, err := m.Create(CreateOptions{
		Dir:         dir,
		VirtualSize: 1024,
		ClusterSize: clusterSize,
	})
	if err != nil {
		t.Fatal(err)
	}
	baseSmallMeta, _ := getMetaPath(baseSmall)

	child, err := m.CreateFromBacking(CreateFromBackingOptions{
		CreateOptions: CreateOptions{
			Dir:         dir,
			VirtualSize: 1500,
			ClusterSize: clusterSize,
		},
		BackingMetaPath: baseLargeMeta,
	})
	if err != nil {
		t.Fatal(err)
	}
	childMeta, _ := getMetaPath(child)
	childImg, err := m.Open(childMeta)
	if err != nil {
		t.Fatal(err)
	}
	defer (*childImg).Close()

	// 这个范围位于 child 的最后一个部分 cluster，同时超出 baseSmall 的可见范围。
	keep := []byte("keep-me-after-rebase")
	if err := (*baseLargeImg).WriteAt(keep, 1024); err != nil {
		t.Fatal(err)
	}

	before := make([]byte, len(keep))
	if err := (*childImg).ReadAt(before, 1024); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, keep) {
		t.Fatal("child should read data from large backing before rebase")
	}

	if err := (*childImg).Rebase(baseSmallMeta); err != nil {
		t.Fatal(err)
	}

	after := make([]byte, len(keep))
	if err := (*childImg).ReadAt(after, 1024); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, keep) {
		t.Fatal("rebase with different backing sizes should preserve visible child data")
	}
}

/************** map + backing 查询 **************/

func TestMapWithBackingAndQueryBacking(t *testing.T) {
	const clusterSize = uint32(512)

	dir := newTestDir(t)
	m := NewManager()

	base, err := m.Create(CreateOptions{
		Dir:         dir,
		VirtualSize: 3 * 512,
		ClusterSize: clusterSize,
	})
	if err != nil {
		t.Fatal(err)
	}
	baseMeta, _ := getMetaPath(base)
	baseImg, err := m.Open(baseMeta)
	if err != nil {
		t.Fatal(err)
	}
	defer (*baseImg).Close()

	basePayload := bytes.Repeat([]byte{0xAB}, int(clusterSize))
	if err := (*baseImg).WriteAt(basePayload, 0); err != nil {
		t.Fatal(err)
	}

	child, err := m.CreateFromBacking(CreateFromBackingOptions{
		CreateOptions: CreateOptions{
			Dir:         dir,
			VirtualSize: 3 * 512,
			ClusterSize: clusterSize,
		},
		BackingMetaPath: baseMeta,
	})
	if err != nil {
		t.Fatal(err)
	}
	childMeta, _ := getMetaPath(child)
	childImg, err := m.Open(childMeta)
	if err != nil {
		t.Fatal(err)
	}
	defer (*childImg).Close()

	childPayload := bytes.Repeat([]byte{0xCD}, int(clusterSize))
	if err := (*childImg).WriteAt(childPayload, 512); err != nil {
		t.Fatal(err)
	}

	backingRef, err := (*childImg).Backing()
	if err != nil {
		t.Fatal(err)
	}
	if backingRef == nil {
		t.Fatal("expected backing ref for child image")
	}
	if backingRef.Guid != base.Guid {
		t.Fatalf("backing guid mismatch: got=%s want=%s", backingRef.Guid, base.Guid)
	}
	if backingRef.MetaPath != baseMeta {
		t.Fatalf("backing meta path mismatch: got=%s want=%s", backingRef.MetaPath, baseMeta)
	}

	segs, err := (*childImg).Map(0, 3*512)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 3 {
		t.Fatalf("expected 3 map segments, got %d", len(segs))
	}

	if segs[0].Offset != 0 || segs[0].Length != 512 || segs[0].Source != MapSourceBacking || segs[0].OwnerGuid != base.Guid {
		t.Fatalf("segment0 mismatch: %+v", segs[0])
	}
	if segs[1].Offset != 512 || segs[1].Length != 512 || segs[1].Source != MapSourceData || segs[1].OwnerGuid != child.Guid {
		t.Fatalf("segment1 mismatch: %+v", segs[1])
	}
	if segs[2].Offset != 1024 || segs[2].Length != 512 || segs[2].Source != MapSourceZero || segs[2].OwnerGuid != "" {
		t.Fatalf("segment2 mismatch: %+v", segs[2])
	}
}

/************** backing 链超出中间层大小时不应透传 **************/

func TestNoLeakBeyondIntermediateBackingSize(t *testing.T) {
	const clusterSize = uint32(512)

	dir := newTestDir(t)
	m := NewManager()

	grand, err := m.Create(CreateOptions{
		Dir:         dir,
		VirtualSize: 2048,
		ClusterSize: clusterSize,
	})
	if err != nil {
		t.Fatal(err)
	}
	grandMeta, _ := getMetaPath(grand)
	grandImg, err := m.Open(grandMeta)
	if err != nil {
		t.Fatal(err)
	}
	defer (*grandImg).Close()

	secret := []byte("grand-secret")
	if err := (*grandImg).WriteAt(secret, 1536); err != nil {
		t.Fatal(err)
	}

	parent, err := m.CreateFromBacking(CreateFromBackingOptions{
		CreateOptions: CreateOptions{
			Dir:         dir,
			VirtualSize: 1024,
			ClusterSize: clusterSize,
		},
		BackingMetaPath: grandMeta,
	})
	if err != nil {
		t.Fatal(err)
	}
	parentMeta, _ := getMetaPath(parent)

	child, err := m.CreateFromBacking(CreateFromBackingOptions{
		CreateOptions: CreateOptions{
			Dir:         dir,
			VirtualSize: 2048,
			ClusterSize: clusterSize,
		},
		BackingMetaPath: parentMeta,
	})
	if err != nil {
		t.Fatal(err)
	}
	childMeta, _ := getMetaPath(child)
	childImg, err := m.Open(childMeta)
	if err != nil {
		t.Fatal(err)
	}
	defer (*childImg).Close()

	buf := make([]byte, len(secret))
	if err := (*childImg).ReadAt(buf, 1536); err != nil {
		t.Fatal(err)
	}
	for i, b := range buf {
		if b != 0 {
			t.Fatalf("expected zero at %d, got %d", i, b)
		}
	}

	segs, err := (*childImg).Map(1536, 512)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 1 || segs[0].Source != MapSourceZero || segs[0].Length != 512 {
		t.Fatalf("unexpected map result: %+v", segs)
	}
}
