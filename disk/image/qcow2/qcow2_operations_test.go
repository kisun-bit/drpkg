// Copyright 2018-present Network Optix, Inc. Licensed under MPL 2.0: www.mozilla.org/MPL/2.0/
package qcow2

import (
	"bytes"
	"errors"
	"path"
	"testing"
)

// createParentAndChild creates a parent (backing) image of the given size,
// fills it with a deterministic pattern and closes it, then creates a child
// image backed by it and returns the open child. Both use the given factory.
func createParentAndChild(
	t *testing.T,
	factory *ImageFactory,
	parentName, childName string,
	virtualSize uint64,
	patternSeed byte,
) *ImageFile {
	parentPath := path.Join(testsDir(), parentName)
	childPath := path.Join(testsDir(), childName)
	prepareTestDir(testsDir(), t)
	deleteDiskIfExists(childPath, t)
	deleteDiskIfExists(parentPath, t)

	parent, err := factory.CreateImage(parentPath, virtualSize)
	if err != nil {
		t.Fatalf("while creating parent image %s", err)
	}
	parentData := makePattern(virtualSize, patternSeed)
	if err := parent.WriteAt(0, parentData); err != nil {
		t.Fatalf("while writing pattern to parent image %s", err)
	}
	if err := parent.Close(); err != nil {
		t.Fatalf("while closing parent image %s", err)
	}

	child, err := factory.CreateImageFromBacking(childPath, parentPath)
	if err != nil {
		t.Fatalf("while creating child image %s on top of %s", err, childPath)
	}
	return child
}

func TestMapEmptyImage(t *testing.T) {
	prepareTestDir(testsDir(), t)
	imagePath := path.Join(testsDir(), "map_empty.img")
	deleteDiskIfExists(imagePath, t)
	size := uint64(4 * 1024 * 1024)
	factory := NoCacheImageFactory()
	image, err := factory.CreateImage(imagePath, size)
	if err != nil {
		t.Fatalf("while creating image %s", err)
	}
	defer image.Close()

	extents, err := image.Map()
	if err != nil {
		t.Fatalf("while mapping image %s", err)
	}
	if len(extents) != 1 {
		t.Fatalf("expected a single merged unallocated extent, got %d extents", len(extents))
	}
	extent := extents[0]
	if extent.Allocated {
		t.Fatalf("expected the extent of an empty image to be unallocated")
	}
	if extent.Offset != 0 || extent.Length != size {
		t.Fatalf(
			"expected extent [0, %d), got [%d, %d)",
			size, extent.Offset, extent.Offset+extent.Length,
		)
	}
}

func TestMapReportsDepthAndHostOffsets(t *testing.T) {
	factory := NoCacheImageFactory()
	size := uint64(4 * 1024 * 1024)
	clusterSize := uint64(64 * 1024)
	child := createParentAndChild(t, factory, "map_parent.img", "map_child.img", size, 7)
	defer child.Close()

	// Overwrite only the second cluster of the child.
	childData := makePattern(clusterSize, 42)
	if err := child.WriteAt(clusterSize, childData); err != nil {
		t.Fatalf("while writing to child %s", err)
	}
	if err := child.Flush(); err != nil {
		t.Fatalf("while flushing child %s", err)
	}

	extents, err := child.Map()
	if err != nil {
		t.Fatalf("while mapping child %s", err)
	}
	if len(extents) != 3 {
		t.Fatalf("expected 3 extents (backing, child, backing), got %d: %+v", len(extents), extents)
	}
	// Extent 0: served by the backing file (depth 1).
	if extents[0].Depth != 1 || !extents[0].Allocated {
		t.Fatalf("extent 0 expected depth 1 allocated from backing, got %+v", extents[0])
	}
	if extents[0].Offset != 0 || extents[0].Length != clusterSize {
		t.Fatalf("extent 0 expected [0,%d), got [%d,%d)", clusterSize, extents[0].Offset, extents[0].Length)
	}
	// Extent 1: allocated in the child itself (depth 0).
	if extents[1].Depth != 0 || !extents[1].Allocated {
		t.Fatalf("extent 1 expected depth 0 allocated in child, got %+v", extents[1])
	}
	if extents[1].Offset != clusterSize || extents[1].Length != clusterSize {
		t.Fatalf("extent 1 expected [%d,%d), got [%d,%d)", clusterSize, 2*clusterSize, extents[1].Offset, extents[1].Length)
	}
	if extents[1].ImagePath != child.fullImagePath {
		t.Fatalf("extent 1 expected image path %s, got %s", child.fullImagePath, extents[1].ImagePath)
	}
	// Extent 2: served by the backing file again.
	if extents[2].Depth != 1 || !extents[2].Allocated {
		t.Fatalf("extent 2 expected depth 1 allocated from backing, got %+v", extents[2])
	}
}

func TestCommitMergesDataIntoBacking(t *testing.T) {
	for _, useCache := range []bool{true, false} {
		name := "nocache"
		if useCache {
			name = "cache"
		}
		t.Run(name, func(t *testing.T) {
			factory := NewImageFactory(useCache)
			size := uint64(4 * 1024 * 1024)
			clusterSize := uint64(64 * 1024)
			child := createParentAndChild(
				t, factory, "commit_parent.img", "commit_child.img", size, 11,
			)
			childPath := child.fullImagePath
			parentPath := ResolveBackingFilePath(
				*child.header.backingFilePath, childPath,
			)

			// Overwrite a couple of clusters in the child.
			childDataA := makePattern(clusterSize, 90)
			childDataB := makePattern(clusterSize, 91)
			if err := child.WriteAt(clusterSize, childDataA); err != nil {
				t.Fatalf("while writing cluster 1 to child %s", err)
			}
			if err := child.WriteAt(3*clusterSize, childDataB); err != nil {
				t.Fatalf("while writing cluster 3 to child %s", err)
			}
			if err := child.Flush(); err != nil {
				t.Fatalf("while flushing child %s", err)
			}

			// Snapshot what the child currently reads before committing.
			beforeCommit, err := child.ReadAt(0, size)
			if err != nil {
				t.Fatalf("while reading child before commit %s", err)
			}

			if err := child.Commit(); err != nil {
				t.Fatalf("while committing child %s", err)
			}
			if err := child.Close(); err != nil {
				t.Fatalf("while closing child after commit %s", err)
			}

			// Reopen the child and verify the data is unchanged and now fully
			// served by the backing file.
			reopenedChild, err := factory.OpenImage(childPath, backingFileMaxNestingDepth)
			if err != nil {
				t.Fatalf("while reopening child %s after commit", err)
			}
			defer reopenedChild.Close()
			afterCommit, err := reopenedChild.ReadAt(0, size)
			if err != nil {
				t.Fatalf("while reading child after commit %s", err)
			}
			if !bytes.Equal(beforeCommit, afterCommit) {
				t.Fatalf("data changed after commit")
			}
			if useCache {
				if err := reopenedChild.Flush(); err != nil {
					t.Fatalf("while flushing reopened child %s", err)
				}
			}
			extents, err := reopenedChild.Map()
			if err != nil {
				t.Fatalf("while mapping reopened child %s", err)
			}
			// After a commit the child holds no allocations: everything is
			// served by the backing file at depth 1.
			for _, extent := range extents {
				if extent.Allocated && extent.Depth == 0 {
					t.Fatalf(
						"expected no allocations in the child after commit, "+
							"found extent at offset %d with depth 0",
						extent.Offset,
					)
				}
				if extent.Allocated && extent.Depth != 1 {
					t.Fatalf("expected all data at depth 1 after commit, got depth %d", extent.Depth)
				}
			}

			// Verify the backing file itself now holds the merged data.
			reopenedParent, err := factory.OpenImage(parentPath, backingFileMaxNestingDepth)
			if err != nil {
				t.Fatalf("while reopening parent %s after commit", err)
			}
			defer reopenedParent.Close()
			parentData, err := reopenedParent.ReadAt(0, size)
			if err != nil {
				t.Fatalf("while reading parent after commit %s", err)
			}
			if !bytes.Equal(parentData, beforeCommit) {
				t.Fatalf("backing file does not contain the merged data after commit")
			}
		})
	}
}

func TestCommitWithoutBackingFails(t *testing.T) {
	prepareTestDir(testsDir(), t)
	imagePath := path.Join(testsDir(), "commit_nobacking.img")
	deleteDiskIfExists(imagePath, t)
	size := uint64(1 * 1024 * 1024)
	factory := NoCacheImageFactory()
	image, err := factory.CreateImage(imagePath, size)
	if err != nil {
		t.Fatalf("while creating image %s", err)
	}
	defer image.Close()
	err = image.Commit()
	if err == nil {
		t.Fatalf("expected committing an image without a backing file to fail")
	}
	var commitErr *ErrCommitImageHasNoBackingFile
	if !errors.As(err, &commitErr) {
		t.Fatalf("expected ErrCommitImageHasNoBackingFile, got %v", err)
	}
}

func TestRebaseToDifferentBackingPreservesData(t *testing.T) {
	prepareTestDir(testsDir(), t)
	factory := NoCacheImageFactory()
	size := uint64(4 * 1024 * 1024)
	clusterSize := uint64(64 * 1024)

	// Old backing, filled with one pattern.
	oldParentPath := path.Join(testsDir(), "rebase_old_parent.img")
	deleteDiskIfExists(oldParentPath, t)
	oldParent, err := factory.CreateImage(oldParentPath, size)
	if err != nil {
		t.Fatalf("while creating old parent %s", err)
	}
	if err := oldParent.WriteAt(0, makePattern(size, 20)); err != nil {
		t.Fatalf("while writing old parent %s", err)
	}
	if err := oldParent.Close(); err != nil {
		t.Fatalf("while closing old parent %s", err)
	}

	// New backing, filled with a DIFFERENT pattern.
	newParentPath := path.Join(testsDir(), "rebase_new_parent.img")
	deleteDiskIfExists(newParentPath, t)
	newParent, err := factory.CreateImage(newParentPath, size)
	if err != nil {
		t.Fatalf("while creating new parent %s", err)
	}
	if err := newParent.WriteAt(0, makePattern(size, 77)); err != nil {
		t.Fatalf("while writing new parent %s", err)
	}
	if err := newParent.Close(); err != nil {
		t.Fatalf("while closing new parent %s", err)
	}

	// Child on top of the old backing, with one cluster overwritten.
	childPath := path.Join(testsDir(), "rebase_child.img")
	deleteDiskIfExists(childPath, t)
	child, err := factory.CreateImageFromBacking(childPath, oldParentPath)
	if err != nil {
		t.Fatalf("while creating child %s", err)
	}
	if err := child.WriteAt(clusterSize, makePattern(clusterSize, 55)); err != nil {
		t.Fatalf("while writing child cluster %s", err)
	}
	if err := child.Flush(); err != nil {
		t.Fatalf("while flushing child %s", err)
	}
	beforeRebase, err := child.ReadAt(0, size)
	if err != nil {
		t.Fatalf("while reading child before rebase %s", err)
	}

	// Rebase onto the new backing. Data must be preserved even though the new
	// backing has different content for the unallocated clusters.
	if err := child.Rebase(newParentPath); err != nil {
		t.Fatalf("while rebasing child %s", err)
	}
	if err := child.Close(); err != nil {
		t.Fatalf("while closing child after rebase %s", err)
	}

	reopened, err := factory.OpenImage(childPath, backingFileMaxNestingDepth)
	if err != nil {
		t.Fatalf("while reopening child %s after rebase", err)
	}
	defer reopened.Close()
	afterRebase, err := reopened.ReadAt(0, size)
	if err != nil {
		t.Fatalf("while reading child after rebase %s", err)
	}
	if !bytes.Equal(beforeRebase, afterRebase) {
		t.Fatalf("data changed after rebase")
	}
	// The header must now point at the new backing file.
	if reopened.header.backingFilePath == nil ||
		*reopened.header.backingFilePath != newParentPath {
		t.Fatalf(
			"expected backing file path %s after rebase, got %v",
			newParentPath, reopened.header.backingFilePath,
		)
	}
}

func TestRebaseRemoveBacking(t *testing.T) {
	factory := NoCacheImageFactory()
	size := uint64(4 * 1024 * 1024)
	child := createParentAndChild(
		t, factory, "rebase_remove_parent.img", "rebase_remove_child.img", size, 33,
	)
	childPath := child.fullImagePath
	beforeRebase, err := child.ReadAt(0, size)
	if err != nil {
		t.Fatalf("while reading child before rebase %s", err)
	}

	// Rebase onto an empty backing path removes the backing file and copies
	// everything into the child.
	if err := child.Rebase(""); err != nil {
		t.Fatalf("while rebasing child %s to remove backing", err)
	}
	if err := child.Close(); err != nil {
		t.Fatalf("while closing child after rebase %s", err)
	}

	reopened, err := factory.OpenImage(childPath, backingFileMaxNestingDepth)
	if err != nil {
		t.Fatalf("while reopening child %s after rebase", err)
	}
	defer reopened.Close()
	afterRebase, err := reopened.ReadAt(0, size)
	if err != nil {
		t.Fatalf("while reading child after rebase %s", err)
	}
	if !bytes.Equal(beforeRebase, afterRebase) {
		t.Fatalf("data changed after removing the backing file")
	}
	if reopened.header.backingFilePath != nil {
		t.Fatalf("expected no backing file after removing it, got %s", *reopened.header.backingFilePath)
	}
	if err := reopened.Flush(); err != nil {
		t.Fatalf("while flushing reopened child %s", err)
	}
	extents, err := reopened.Map()
	if err != nil {
		t.Fatalf("while mapping reopened child %s", err)
	}
	// Every cluster must now be allocated in the child itself at depth 0.
	for _, extent := range extents {
		if !extent.Allocated || extent.Depth != 0 {
			t.Fatalf(
				"expected all data allocated at depth 0 after removing backing, "+
					"got extent %+v",
				extent,
			)
		}
	}
}

func TestRebaseBackingTooSmallFails(t *testing.T) {
	prepareTestDir(testsDir(), t)
	factory := NoCacheImageFactory()
	childSize := uint64(4 * 1024 * 1024)
	smallParentPath := path.Join(testsDir(), "rebase_small_parent.img")
	deleteDiskIfExists(smallParentPath, t)
	smallParent, err := factory.CreateImage(smallParentPath, childSize/2)
	if err != nil {
		t.Fatalf("while creating small parent %s", err)
	}
	if err := smallParent.Close(); err != nil {
		t.Fatalf("while closing small parent %s", err)
	}

	childPath := path.Join(testsDir(), "rebase_child_small.img")
	deleteDiskIfExists(childPath, t)
	child, err := factory.CreateImage(childPath, childSize)
	if err != nil {
		t.Fatalf("while creating child %s", err)
	}
	defer child.Close()
	err = child.Rebase(smallParentPath)
	if err == nil {
		t.Fatalf("expected rebasing onto a smaller backing image to fail")
	}
	var tooSmallErr *ErrRebaseBackingImageTooSmall
	if !errors.As(err, &tooSmallErr) {
		t.Fatalf("expected ErrRebaseBackingImageTooSmall, got %v", err)
	}
}

// findExtentAt returns the extent that covers the given virtual offset.
func findExtentAt(t *testing.T, extents []MapExtent, offset uint64) MapExtent {
	for _, extent := range extents {
		if offset >= extent.Offset && offset < extent.Offset+extent.Length {
			return extent
		}
	}
	t.Fatalf("no extent covers offset %d", offset)
	return MapExtent{}
}

// TestCommitAcrossMultipleBackingLayers verifies Commit on a three image
// chain (base <- parent <- child):
//   - committing the child merges its data into the parent only, the base is
//     left untouched and the visible data of the chain is unchanged;
//   - committing the parent afterwards moves everything into the base;
//   - after each commit the committed layer holds no allocations of its own
//     and Map reports the data at the correct depths.
func TestCommitAcrossMultipleBackingLayers(t *testing.T) {
	for _, useCache := range []bool{true, false} {
		name := "nocache"
		if useCache {
			name = "cache"
		}
		t.Run(name, func(t *testing.T) {
			runCommitAcrossMultipleBackingLayers(t, useCache)
		})
	}
}

func runCommitAcrossMultipleBackingLayers(t *testing.T, useCache bool) {
	factory := NewImageFactory(useCache)
	size := uint64(4 * 1024 * 1024)
	clusterSize := uint64(64 * 1024)
	basePattern := makePattern(size, 10)
	parentCluster := makePattern(clusterSize, 20)
	childCluster := makePattern(clusterSize, 30)

	basePath := path.Join(testsDir(), "multilayer_base.img")
	parentPath := path.Join(testsDir(), "multilayer_parent.img")
	childPath := path.Join(testsDir(), "multilayer_child.img")
	prepareTestDir(testsDir(), t)
	for _, imagePath := range []string{childPath, parentPath, basePath} {
		deleteDiskIfExists(imagePath, t)
	}

	// Base layer: pattern 10 everywhere.
	base, err := factory.CreateImage(basePath, size)
	if err != nil {
		t.Fatalf("while creating base image %s", err)
	}
	if err := base.WriteAt(0, basePattern); err != nil {
		t.Fatalf("while writing base image %s", err)
	}
	if err := base.Close(); err != nil {
		t.Fatalf("while closing base image %s", err)
	}

	// Parent layer on top of the base: pattern 20 in clusters 10 and 11.
	parent, err := factory.CreateImageFromBacking(parentPath, basePath)
	if err != nil {
		t.Fatalf("while creating parent image %s", err)
	}
	if err := parent.WriteAt(10*clusterSize, parentCluster); err != nil {
		t.Fatalf("while writing parent cluster 10 %s", err)
	}
	if err := parent.WriteAt(11*clusterSize, parentCluster); err != nil {
		t.Fatalf("while writing parent cluster 11 %s", err)
	}
	if err := parent.Close(); err != nil {
		t.Fatalf("while closing parent image %s", err)
	}

	// Child layer on top of the parent: pattern 30 in cluster 10 (overwriting
	// the parent's cluster) and in cluster 40 (never written by the parent).
	child, err := factory.CreateImageFromBacking(childPath, parentPath)
	if err != nil {
		t.Fatalf("while creating child image %s", err)
	}
	if err := child.WriteAt(10*clusterSize, childCluster); err != nil {
		t.Fatalf("while writing child cluster 10 %s", err)
	}
	if err := child.WriteAt(40*clusterSize, childCluster); err != nil {
		t.Fatalf("while writing child cluster 40 %s", err)
	}
	if err := child.Flush(); err != nil {
		t.Fatalf("while flushing child image %s", err)
	}

	// The expected view of the whole chain: base data, parent data in
	// clusters 10 and 11, child data in clusters 10 and 40.
	expectedView := make([]byte, size)
	copy(expectedView, basePattern)
	copy(expectedView[10*clusterSize:], parentCluster)
	copy(expectedView[11*clusterSize:], parentCluster)
	copy(expectedView[10*clusterSize:], childCluster)
	copy(expectedView[40*clusterSize:], childCluster)

	view, err := child.ReadAt(0, size)
	if err != nil {
		t.Fatalf("while reading the chain before commit %s", err)
	}
	if !bytes.Equal(view, expectedView) {
		t.Fatalf("chain view before commit is wrong")
	}

	// Commit the child: its data must move into the parent only.
	if err := child.Commit(); err != nil {
		t.Fatalf("while committing the child %s", err)
	}
	if err := child.Close(); err != nil {
		t.Fatalf("while closing the child after commit %s", err)
	}

	// The base must not have been touched by committing the child.
	baseAfterChildCommit, err := factory.OpenImage(basePath, backingFileMaxNestingDepth)
	if err != nil {
		t.Fatalf("while reopening the base image %s", err)
	}
	baseView, err := baseAfterChildCommit.ReadAt(0, size)
	if err != nil {
		t.Fatalf("while reading the base after the child commit %s", err)
	}
	if !bytes.Equal(baseView, basePattern) {
		t.Fatalf("the base image was modified by committing the child")
	}
	if err := baseAfterChildCommit.Close(); err != nil {
		t.Fatalf("while closing the base image %s", err)
	}

	// The child must now be empty and the whole chain must still read the
	// same data, served one layer deeper.
	childReopened, err := factory.OpenImage(childPath, backingFileMaxNestingDepth)
	if err != nil {
		t.Fatalf("while reopening the child after commit %s", err)
	}
	view, err = childReopened.ReadAt(0, size)
	if err != nil {
		t.Fatalf("while reading the chain after the child commit %s", err)
	}
	if !bytes.Equal(view, expectedView) {
		t.Fatalf("chain view changed after committing the child")
	}
	if useCache {
		if err := childReopened.Flush(); err != nil {
			t.Fatalf("while flushing the reopened child %s", err)
		}
	}
	extents, err := childReopened.Map()
	if err != nil {
		t.Fatalf("while mapping the reopened child %s", err)
	}
	for _, extent := range extents {
		if extent.Allocated && extent.Depth == 0 {
			t.Fatalf(
				"the child holds allocations at offset %d after commit",
				extent.Offset,
			)
		}
	}
	// Clusters written by the child are now served by the parent (depth 1),
	// everything else by the base (depth 2).
	for _, offset := range []uint64{10 * clusterSize, 11 * clusterSize, 40 * clusterSize} {
		extent := findExtentAt(t, extents, offset)
		if !extent.Allocated || extent.Depth != 1 || extent.ImagePath != parentPath {
			t.Fatalf(
				"expected cluster at offset %d at depth 1 in %s after the "+
					"child commit, got %+v",
				offset, parentPath, extent,
			)
		}
	}
	baseExtent := findExtentAt(t, extents, 0)
	if !baseExtent.Allocated || baseExtent.Depth != 2 || baseExtent.ImagePath != basePath {
		t.Fatalf(
			"expected unmodified clusters at depth 2 in %s after the child "+
				"commit, got %+v",
			basePath, baseExtent,
		)
	}
	if err := childReopened.Close(); err != nil {
		t.Fatalf("while closing the reopened child %s", err)
	}

	// Commit the parent: everything (including what it received from the
	// child) must move into the base.
	parentReopened, err := factory.OpenImage(parentPath, backingFileMaxNestingDepth)
	if err != nil {
		t.Fatalf("while reopening the parent %s", err)
	}
	parentView, err := parentReopened.ReadAt(0, size)
	if err != nil {
		t.Fatalf("while reading the parent before its commit %s", err)
	}
	if !bytes.Equal(parentView, expectedView) {
		t.Fatalf("parent view before its commit is wrong")
	}
	if err := parentReopened.Commit(); err != nil {
		t.Fatalf("while committing the parent %s", err)
	}
	if err := parentReopened.Close(); err != nil {
		t.Fatalf("while closing the parent after commit %s", err)
	}

	// The parent must now be empty and read everything from the base.
	parentFinal, err := factory.OpenImage(parentPath, backingFileMaxNestingDepth)
	if err != nil {
		t.Fatalf("while reopening the parent after its commit %s", err)
	}
	view, err = parentFinal.ReadAt(0, size)
	if err != nil {
		t.Fatalf("while reading the parent after its commit %s", err)
	}
	if !bytes.Equal(view, expectedView) {
		t.Fatalf("parent view changed after committing the parent")
	}
	if useCache {
		if err := parentFinal.Flush(); err != nil {
			t.Fatalf("while flushing the reopened parent %s", err)
		}
	}
	extents, err = parentFinal.Map()
	if err != nil {
		t.Fatalf("while mapping the reopened parent %s", err)
	}
	for _, extent := range extents {
		if !extent.Allocated || extent.Depth != 1 || extent.ImagePath != basePath {
			t.Fatalf(
				"expected all data at depth 1 in %s after committing the "+
					"parent, got %+v",
				basePath, extent,
			)
		}
	}
	if err := parentFinal.Close(); err != nil {
		t.Fatalf("while closing the reopened parent %s", err)
	}

	// The base must now hold the full composite view.
	baseFinal, err := factory.OpenImage(basePath, backingFileMaxNestingDepth)
	if err != nil {
		t.Fatalf("while reopening the base at the end %s", err)
	}
	defer baseFinal.Close()
	baseView, err = baseFinal.ReadAt(0, size)
	if err != nil {
		t.Fatalf("while reading the base at the end %s", err)
	}
	if !bytes.Equal(baseView, expectedView) {
		t.Fatalf("the base does not hold the merged data after committing the parent")
	}
}
