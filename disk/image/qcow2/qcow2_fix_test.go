// Copyright 2018-present Network Optix, Inc. Licensed under MPL 2.0: www.mozilla.org/MPL/2.0/
package qcow2

import (
	"bytes"
	"errors"
	"os"
	"path"
	"strings"
	"testing"
)

// makePattern returns a deterministic mostly non-zero byte pattern.
func makePattern(size uint64, seed byte) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = seed + byte(i%251)
	}
	return data
}

// Regression test: the cached mode writes L2 table entries with the COPIED
// flag (bit 63) set, while the no cache mode used to interpret that bit as
// part of the cluster address. After the fix both modes must be able to read
// images written by each other.
func TestCrossModeCacheWriteThenNoCacheRead(t *testing.T) {
	imagePath := path.Join(testsDir(), "cross_mode.img")
	prepareTestDir(testsDir(), t)
	deleteDiskIfExists(imagePath, t)
	size := uint64(4 * 1024 * 1024)
	offsets := []uint64{0, 64 * 1024, size - 64*1024}
	patterns := make([][]byte, len(offsets))

	image, err := CachedImageFactory().CreateImage(imagePath, size)
	if err != nil {
		t.Fatalf("Error while creating image %s", err)
	}
	for i, offset := range offsets {
		patterns[i] = makePattern(512, byte(i+1))
		if err := image.WriteAt(offset, patterns[i]); err != nil {
			t.Fatalf("Error while writing at offset %d: %s", offset, err)
		}
	}
	if err := image.Close(); err != nil {
		t.Fatalf("Error while closing image: %s", err)
	}

	// Reopen without cache; before the fix this failed with an invalid
	// (negative) file offset error.
	reopened, err := NoCacheImageFactory().OpenImage(imagePath, 1)
	if err != nil {
		t.Fatalf("Error while reopening image without cache: %s", err)
	}
	for i, offset := range offsets {
		data, err := reopened.ReadAt(offset, 512)
		if err != nil {
			t.Fatalf("Error while reading at offset %d in no cache mode: %s", offset, err)
		}
		if !bytes.Equal(data, patterns[i]) {
			t.Errorf("Data mismatch at offset %d after cross mode reopen", offset)
		}
	}
	// Write through the no cache path as well, close, then reopen with the
	// cache and verify that all data is still readable.
	newPattern := makePattern(512, 99)
	if err := reopened.WriteAt(2*64*1024, newPattern); err != nil {
		t.Fatalf("Error while writing in no cache mode: %s", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("Error while closing image: %s", err)
	}
	final, err := CachedImageFactory().OpenImage(imagePath, 1)
	if err != nil {
		t.Fatalf("Error while reopening image with cache: %s", err)
	}
	defer func() {
		if err := final.Close(); err != nil {
			t.Fatalf("Error while closing image: %s", err)
		}
	}()
	for i, offset := range offsets {
		data, err := final.ReadAt(offset, 512)
		if err != nil {
			t.Fatalf("Error while reading at offset %d in cached mode: %s", offset, err)
		}
		if !bytes.Equal(data, patterns[i]) {
			t.Errorf("Data mismatch at offset %d after final reopen", offset)
		}
	}
	data, err := final.ReadAt(2*64*1024, 512)
	if err != nil {
		t.Fatalf("Error while reading no cache mode written data: %s", err)
	}
	if !bytes.Equal(data, newPattern) {
		t.Errorf("Data written in no cache mode was corrupted")
	}
}

// Regression test: when the virtual disk size is not a multiple of the
// cluster size, the last cluster is partial. Writing to it used to fail with
// "initial data is too small" because the data read from the backing file
// was shorter than one cluster.
func TestWriteToTailOfUnalignedVirtualSize(t *testing.T) {
	parentPath := path.Join(testsDir(), "unaligned_parent.img")
	childPath := path.Join(testsDir(), "unaligned_child.img")
	prepareTestDir(testsDir(), t)
	deleteDiskIfExists(parentPath, t)
	deleteDiskIfExists(childPath, t)
	size := uint64(1024*1024 + 512) // not a multiple of the cluster size
	tailOffset := size - 512

	parentPattern := makePattern(512, 7)
	parent, err := CachedImageFactory().CreateImage(parentPath, size)
	if err != nil {
		t.Fatalf("Error while creating parent image with unaligned size: %s", err)
	}
	if err := parent.WriteAt(tailOffset, parentPattern); err != nil {
		t.Fatalf("Error while writing to the tail of the parent image: %s", err)
	}
	if err := parent.Close(); err != nil {
		t.Fatalf("Error while closing the parent image: %s", err)
	}

	child, err := CachedImageFactory().CreateImageFromBacking(childPath, parentPath)
	if err != nil {
		t.Fatalf("Error while creating child image: %s", err)
	}
	childPattern := makePattern(512, 21)
	if err := child.WriteAt(tailOffset, childPattern); err != nil {
		t.Fatalf("Error while writing to the tail of the child image: %s", err)
	}
	data, err := child.ReadAt(tailOffset, 512)
	if err != nil {
		t.Fatalf("Error while reading the tail of the child image: %s", err)
	}
	if !bytes.Equal(data, childPattern) {
		t.Errorf("Data mismatch at the tail of the child image")
	}
	if err := child.Close(); err != nil {
		t.Fatalf("Error while closing the child image: %s", err)
	}

	// Reopen the chain and verify that the written tail data is intact.
	reopened, err := NoCacheImageFactory().OpenImage(childPath, 2)
	if err != nil {
		t.Fatalf("Error while reopening child image: %s", err)
	}
	defer func() {
		if err := reopened.Close(); err != nil {
			t.Fatalf("Error while closing child image: %s", err)
		}
	}()
	data, err = reopened.ReadAt(tailOffset, 512)
	if err != nil {
		t.Fatalf("Error while reading the tail after reopen: %s", err)
	}
	if !bytes.Equal(data, childPattern) {
		t.Errorf("Data mismatch at the tail after reopen")
	}
}

// Regression test: reopening an image with a backing chain used to fail with
// a misleading "recursion depth 10 is exceeded" error regardless of the depth
// passed by the caller. The depth argument counts images in the chain
// including the image itself.
func TestReopenImageWithBackingChain(t *testing.T) {
	grandParentPath := path.Join(testsDir(), "chain_grandparent.img")
	parentPath := path.Join(testsDir(), "chain_parent.img")
	childPath := path.Join(testsDir(), "chain_child.img")
	prepareTestDir(testsDir(), t)
	for _, imagePath := range []string{grandParentPath, parentPath, childPath} {
		deleteDiskIfExists(imagePath, t)
	}
	size := uint64(2 * 1024 * 1024)
	offset := uint64(64 * 1024)
	pattern := makePattern(512, 3)

	grandParent, err := CachedImageFactory().CreateImage(grandParentPath, size)
	if err != nil {
		t.Fatalf("Error while creating the grandparent image: %s", err)
	}
	if err := grandParent.WriteAt(offset, pattern); err != nil {
		t.Fatalf("Error while writing to the grandparent image: %s", err)
	}
	if err := grandParent.Close(); err != nil {
		t.Fatalf("Error while closing the grandparent image: %s", err)
	}

	parent, err := CachedImageFactory().CreateImageFromBacking(parentPath, grandParentPath)
	if err != nil {
		t.Fatalf("Error while creating the parent image: %s", err)
	}
	if err := parent.Close(); err != nil {
		t.Fatalf("Error while closing the parent image: %s", err)
	}

	child, err := CachedImageFactory().CreateImageFromBacking(childPath, parentPath)
	if err != nil {
		t.Fatalf("Error while creating the child image: %s", err)
	}
	if err := child.Close(); err != nil {
		t.Fatalf("Error while closing the child image: %s", err)
	}

	// Depth 1 is not enough for an image that has a backing file.
	_, err = CachedImageFactory().OpenImage(childPath, 1)
	var depthErr *ErrRecursionDepthExceeded
	if !errors.As(err, &depthErr) {
		t.Fatalf("Expected ErrRecursionDepthExceeded for depth 1, got %v", err)
	}
	// The error message must report the depth requested by the caller.
	if !strings.Contains(err.Error(), "recursion depth 1") {
		t.Errorf("Error message must report the requested depth, got: %s", err)
	}

	// Depth 2 is not enough for a three image chain either.
	_, err = CachedImageFactory().OpenImage(childPath, 2)
	if !errors.As(err, &depthErr) {
		t.Fatalf("Expected ErrRecursionDepthExceeded for depth 2, got %v", err)
	}
	if !strings.Contains(err.Error(), "recursion depth 2") {
		t.Errorf("Error message must report the requested depth, got: %s", err)
	}

	// Depth 3 opens the whole chain; data must be readable through it.
	reopened, err := CachedImageFactory().OpenImage(childPath, 3)
	if err != nil {
		t.Fatalf("Error while reopening the chain with depth 3: %s", err)
	}
	defer func() {
		if err := reopened.Close(); err != nil {
			t.Fatalf("Error while closing the reopened chain: %s", err)
		}
	}()
	data, err := reopened.ReadAt(offset, 512)
	if err != nil {
		t.Fatalf("Error while reading through the backing chain: %s", err)
	}
	if !bytes.Equal(data, pattern) {
		t.Errorf("Data read through the backing chain is corrupted")
	}
}

// Regression test: writing through a write back cache with a tiny L2 table
// cache forces copy-on-write reallocation of L2 table clusters. All data
// written before and after the reallocation must survive.
func TestL2TableReallocationOnWriteBack(t *testing.T) {
	imagePath := path.Join(testsDir(), "l2_cow.img")
	prepareTestDir(testsDir(), t)
	deleteDiskIfExists(imagePath, t)
	// With the default 64KB cluster a single L2 table maps 512MB, so use a
	// 2GB virtual size to have several L2 tables. The host file stays small
	// because clusters are allocated lazily.
	size := uint64(2) * 1024 * 1024 * 1024
	l2Range := uint64(512 * 1024 * 1024)

	image, err := CachedImageFactory().CreateImage(imagePath, size)
	if err != nil {
		t.Fatalf("Error while creating image: %s", err)
	}
	pattern0 := makePattern(512, 11)
	pattern1 := makePattern(512, 12)
	if err := image.WriteAt(0, pattern0); err != nil {
		t.Fatalf("Error while writing the first cluster: %s", err)
	}
	if err := image.WriteAt(l2Range, pattern1); err != nil {
		t.Fatalf("Error while writing the second L2 range: %s", err)
	}
	if err := image.Close(); err != nil {
		t.Fatalf("Error while closing image: %s", err)
	}

	// Reopen with a tiny L2 table cache: allocating new clusters in already
	// existing L2 tables forces L2 table copy-on-write reallocation.
	tinyCacheFactory := ImageFactory{
		useCache:                     true,
		pointerTableCacheSize:        1,
		referenceCountTableCacheSize: 50,
	}
	image, err = tinyCacheFactory.OpenImage(imagePath, 1)
	if err != nil {
		t.Fatalf("Error while reopening image with a tiny cache: %s", err)
	}
	pattern2 := makePattern(512, 13)
	pattern3 := makePattern(512, 14)
	pattern4 := makePattern(512, 15)
	if err := image.WriteAt(64*1024, pattern2); err != nil {
		t.Fatalf("Error while writing a new cluster into the first L2 table: %s", err)
	}
	if err := image.WriteAt(l2Range+64*1024, pattern3); err != nil {
		t.Fatalf("Error while writing a new cluster into the second L2 table: %s", err)
	}
	// One more write into the first L2 table range exercises the
	// "previously read but L1 not synced" path.
	if err := image.WriteAt(2*64*1024, pattern4); err != nil {
		t.Fatalf("Error while writing another cluster into the first L2 table: %s", err)
	}
	if err := image.Close(); err != nil {
		t.Fatalf("Error while closing image: %s", err)
	}

	final, err := NoCacheImageFactory().OpenImage(imagePath, 1)
	if err != nil {
		t.Fatalf("Error while reopening image for verification: %s", err)
	}
	defer func() {
		if err := final.Close(); err != nil {
			t.Fatalf("Error while closing image: %s", err)
		}
	}()
	checks := []struct {
		offset uint64
		want   []byte
	}{
		{0, pattern0},
		{l2Range, pattern1},
		{64 * 1024, pattern2},
		{l2Range + 64*1024, pattern3},
		{2 * 64 * 1024, pattern4},
	}
	for _, check := range checks {
		data, err := final.ReadAt(check.offset, 512)
		if err != nil {
			t.Fatalf("Error while reading at offset %d: %s", check.offset, err)
		}
		if !bytes.Equal(data, check.want) {
			t.Errorf("Data mismatch at offset %d after L2 table reallocation", check.offset)
		}
	}
}

// Regression test: header validation used to compare the L1 table offset
// (a host file offset) against the virtual disk size, which rejected images
// smaller than a single cluster.
func TestCreateImageSmallerThanOneCluster(t *testing.T) {
	imagePath := path.Join(testsDir(), "tiny.img")
	prepareTestDir(testsDir(), t)
	deleteDiskIfExists(imagePath, t)
	image, err := CachedImageFactory().CreateImage(imagePath, 1024)
	if err != nil {
		t.Fatalf("Error while creating an image smaller than one cluster: %s", err)
	}
	pattern := makePattern(512, 5)
	if err := image.WriteAt(0, pattern); err != nil {
		t.Fatalf("Error while writing to the tiny image: %s", err)
	}
	if err := image.Close(); err != nil {
		t.Fatalf("Error while closing the tiny image: %s", err)
	}
	reopened, err := NoCacheImageFactory().OpenImage(imagePath, 1)
	if err != nil {
		t.Fatalf("Error while reopening the tiny image: %s", err)
	}
	defer func() {
		if err := reopened.Close(); err != nil {
			t.Fatalf("Error while closing the tiny image: %s", err)
		}
	}()
	data, err := reopened.ReadAt(0, 512)
	if err != nil {
		t.Fatalf("Error while reading from the tiny image: %s", err)
	}
	if !bytes.Equal(data, pattern) {
		t.Errorf("Data mismatch in the tiny image")
	}
}

// Close() must mark the image as closed even if flushing fails, and a second
// Close() must be a no-op instead of operating on the closed file handle.
func TestCloseIsIdempotent(t *testing.T) {
	imagePath := path.Join(testsDir(), "close_twice.img")
	prepareTestDir(testsDir(), t)
	deleteDiskIfExists(imagePath, t)
	image, err := CachedImageFactory().CreateImage(imagePath, uint64(1024*1024))
	if err != nil {
		t.Fatalf("Error while creating image: %s", err)
	}
	if err := image.Close(); err != nil {
		t.Fatalf("Error on the first close: %s", err)
	}
	if err := image.Close(); err != nil {
		t.Fatalf("Error on the second close: %s", err)
	}
}

// A cache based factory with a zero sized cache used to break on every write;
// now it must be rejected upfront with a descriptive error.
func TestZeroCacheSizeIsRejected(t *testing.T) {
	imagePath := path.Join(testsDir(), "zero_cache.img")
	prepareTestDir(testsDir(), t)
	deleteDiskIfExists(imagePath, t)
	image, err := NoCacheImageFactory().CreateImage(imagePath, uint64(1024*1024))
	if err != nil {
		t.Fatalf("Error while creating image: %s", err)
	}
	if err := image.Close(); err != nil {
		t.Fatalf("Error while closing image: %s", err)
	}
	brokenFactory := ImageFactory{
		useCache:                     true,
		pointerTableCacheSize:        0,
		referenceCountTableCacheSize: 0,
	}
	_, err = brokenFactory.OpenImage(imagePath, 1)
	if err == nil {
		t.Fatalf("Expected an error for a zero sized cache")
	}
	if !strings.Contains(err.Error(), "cache size") {
		t.Errorf("Unexpected error message: %s", err)
	}
}

// Header validation must reject structurally broken headers.
func TestHeaderValidationRejectsInvalidHeaders(t *testing.T) {
	mutate := func(mutations map[int]byte) []byte {
		header := make([]byte, len(validHeader))
		copy(header, validHeader)
		for offset, value := range mutations {
			header[offset] = value
		}
		return header
	}
	cases := []struct {
		name   string
		header []byte
		check  func(error) bool
	}{
		{
			name:   "invalid magic",
			header: mutate(map[int]byte{0: 0x00}),
			check: func(err error) bool {
				var target *ErrInvalidMagic
				return errors.As(err, &target)
			},
		},
		{
			name:   "unsupported version",
			header: mutate(map[int]byte{7: 0x02}),
			check: func(err error) bool {
				var target *ErrInvalidVersion
				return errors.As(err, &target)
			},
		},
		{
			name:   "invalid cluster bits",
			header: mutate(map[int]byte{23: 0x08}),
			check: func(err error) bool {
				var target *ErrInvalidClusterBits
				return errors.As(err, &target)
			},
		},
		{
			name: "non empty L1 table with zero offset",
			header: mutate(map[int]byte{
				39: 0x01, // l1Size = 1
				40: 0x00, 41: 0x00, 42: 0x00, 43: 0x00,
				44: 0x00, 45: 0x00, 46: 0x00, 47: 0x00, // l1TableOffset = 0
			}),
			check: func(err error) bool {
				return strings.Contains(err.Error(), "L1 table offset")
			},
		},
	}
	for _, testCase := range cases {
		_, err := imageHeaderFromFile(newFileBuffer(testCase.header))
		if err == nil {
			t.Errorf("%s: expected an error, got nil", testCase.name)
			continue
		}
		if !testCase.check(err) {
			t.Errorf("%s: unexpected error %s", testCase.name, err)
		}
	}
}

// An image whose L1 table offset points beyond the actual file must be
// rejected when opened.
func TestOpenImageRejectsL1OffsetBeyondFileSize(t *testing.T) {
	imagePath := path.Join(testsDir(), "bad_l1_offset.img")
	prepareTestDir(testsDir(), t)
	deleteDiskIfExists(imagePath, t)
	image, err := NoCacheImageFactory().CreateImage(imagePath, uint64(1024*1024))
	if err != nil {
		t.Fatalf("Error while creating image: %s", err)
	}
	if err := image.Close(); err != nil {
		t.Fatalf("Error while closing image: %s", err)
	}
	// Corrupt the L1 table offset field (header offset 40).
	file, err := os.OpenFile(imagePath, os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("Error while opening image for corruption: %s", err)
	}
	if err := writeUint64At(file, uint64(1)<<40, 40); err != nil {
		t.Fatalf("Error while corrupting the image header: %s", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Error while closing the corrupted image: %s", err)
	}
	_, err = NoCacheImageFactory().OpenImage(imagePath, 1)
	var boundsErr *ErrL1OffsetExceedsFileBoundaries
	if !errors.As(err, &boundsErr) {
		t.Fatalf("Expected ErrL1OffsetExceedsFileBoundaries, got %v", err)
	}
}

// TestBackingWriteConsistency verifies the core copy-on-write contract for an
// image that has a backing file, in both cached and no cache modes:
//   - a partial cluster write merges the new data with the backing data
//   - a full cluster write replaces the whole cluster without touching backing
//   - an untouched cluster still reads the backing data
//   - a write crossing a cluster boundary splits correctly
//   - the backing (parent) file is never modified by child writes
//   - the whole view survives a close/reopen of the chain
func TestBackingWriteConsistency(t *testing.T) {
	for _, useCache := range []bool{true, false} {
		useCache := useCache
		mode := "cached"
		if !useCache {
			mode = "nocache"
		}
		t.Run(mode, func(t *testing.T) {
			runBackingWriteConsistency(t, useCache)
		})
	}
}

func runBackingWriteConsistency(t *testing.T, useCache bool) {
	factory := NewImageFactory(useCache)
	parentPath := path.Join(testsDir(), "consistency_parent.img")
	childPath := path.Join(testsDir(), "consistency_child.img")
	prepareTestDir(testsDir(), t)
	deleteDiskIfExists(parentPath, t)
	deleteDiskIfExists(childPath, t)

	clusterSize := getClusterSize(DefaultClusterBits)
	cs := int(clusterSize)
	numClusters := uint64(5)
	size := clusterSize * numClusters

	// Build a parent image where every cluster carries a distinct pattern so
	// the origin of every byte in the child can be identified.
	parent, err := factory.CreateImage(parentPath, size)
	if err != nil {
		t.Fatalf("Error while creating the parent image: %s", err)
	}
	parentPatterns := make([][]byte, numClusters)
	for c := uint64(0); c < numClusters; c++ {
		parentPatterns[c] = makePattern(clusterSize, byte(100+c))
		if err := parent.WriteAt(c*clusterSize, parentPatterns[c]); err != nil {
			t.Fatalf("Error while writing parent cluster %d: %s", c, err)
		}
	}
	if useCache {
		if err := parent.Flush(); err != nil {
			t.Fatalf("Error while flushing the parent image: %s", err)
		}
	}
	if err := parent.Close(); err != nil {
		t.Fatalf("Error while closing the parent image: %s", err)
	}

	child, err := factory.CreateImageFromBacking(childPath, parentPath)
	if err != nil {
		t.Fatalf("Error while creating the child image: %s", err)
	}

	// Cluster 0: partial write, the rest must be copied from the backing file.
	partialWrite := makePattern(4*1024, 50)
	if err := child.WriteAt(0, partialWrite); err != nil {
		t.Fatalf("Error while writing partially to cluster 0: %s", err)
	}
	// Cluster 1: full cluster write, the whole cluster is replaced.
	fullWrite := makePattern(clusterSize, 60)
	if err := child.WriteAt(clusterSize, fullWrite); err != nil {
		t.Fatalf("Error while writing fully to cluster 1: %s", err)
	}
	// Cluster 2: never written, must read exactly as the backing cluster.
	// Clusters 3 and 4: a single write crossing the cluster boundary.
	crossWrite := makePattern(4*1024, 70)
	crossOffset := 4*clusterSize - 2*1024
	if err := child.WriteAt(crossOffset, crossWrite); err != nil {
		t.Fatalf("Error while writing across the cluster boundary: %s", err)
	}

	verifyChildView := func(source string, reader *ImageFile) {
		cluster0, err := reader.ReadAt(0, clusterSize)
		if err != nil {
			t.Fatalf("%s: error reading cluster 0: %s", source, err)
		}
		if !bytes.Equal(cluster0[:len(partialWrite)], partialWrite) {
			t.Errorf("%s: cluster 0 head does not hold the child write", source)
		}
		if !bytes.Equal(cluster0[len(partialWrite):], parentPatterns[0][len(partialWrite):]) {
			t.Errorf("%s: cluster 0 tail lost the backing data (bad copy on write)", source)
		}
		cluster1, err := reader.ReadAt(clusterSize, clusterSize)
		if err != nil {
			t.Fatalf("%s: error reading cluster 1: %s", source, err)
		}
		if !bytes.Equal(cluster1, fullWrite) {
			t.Errorf("%s: cluster 1 does not hold the full child write", source)
		}
		cluster2, err := reader.ReadAt(2*clusterSize, clusterSize)
		if err != nil {
			t.Fatalf("%s: error reading cluster 2: %s", source, err)
		}
		if !bytes.Equal(cluster2, parentPatterns[2]) {
			t.Errorf("%s: untouched cluster 2 does not match the backing data", source)
		}
		cluster3, err := reader.ReadAt(3*clusterSize, clusterSize)
		if err != nil {
			t.Fatalf("%s: error reading cluster 3: %s", source, err)
		}
		if !bytes.Equal(cluster3[:cs-2*1024], parentPatterns[3][:cs-2*1024]) {
			t.Errorf("%s: cluster 3 head lost the backing data", source)
		}
		if !bytes.Equal(cluster3[cs-2*1024:], crossWrite[:2*1024]) {
			t.Errorf("%s: cluster 3 tail does not hold the child write", source)
		}
		cluster4, err := reader.ReadAt(4*clusterSize, clusterSize)
		if err != nil {
			t.Fatalf("%s: error reading cluster 4: %s", source, err)
		}
		if !bytes.Equal(cluster4[:2*1024], crossWrite[2*1024:]) {
			t.Errorf("%s: cluster 4 head does not hold the child write", source)
		}
		if !bytes.Equal(cluster4[2*1024:], parentPatterns[4][2*1024:]) {
			t.Errorf("%s: cluster 4 tail lost the backing data", source)
		}
	}
	verifyChildView("before close", child)
	if err := child.Close(); err != nil {
		t.Fatalf("Error while closing the child image: %s", err)
	}

	// The backing file must remain byte-identical after all child writes.
	parentReopened, err := factory.OpenImage(parentPath, 1)
	if err != nil {
		t.Fatalf("Error while reopening the parent image: %s", err)
	}
	for c := uint64(0); c < numClusters; c++ {
		data, err := parentReopened.ReadAt(c*clusterSize, clusterSize)
		if err != nil {
			t.Fatalf("Error while reading parent cluster %d: %s", c, err)
		}
		if !bytes.Equal(data, parentPatterns[c]) {
			t.Errorf("Parent cluster %d was modified by child writes", c)
		}
	}
	if err := parentReopened.Close(); err != nil {
		t.Fatalf("Error while closing the reopened parent image: %s", err)
	}

	// Reopen the whole chain and confirm the view is durable.
	chain, err := factory.OpenImage(childPath, 2)
	if err != nil {
		t.Fatalf("Error while reopening the backing chain: %s", err)
	}
	defer func() {
		if err := chain.Close(); err != nil {
			t.Fatalf("Error while closing the reopened chain: %s", err)
		}
	}()
	verifyChildView("after reopen", chain)
}
