// Copyright 2018-present Network Optix, Inc. Licensed under MPL 2.0: www.mozilla.org/MPL/2.0/
package qcow2

import (
	"bytes"
	"fmt"
)

// MapExtent describes one contiguous extent of the virtual disk as reported
// by Map. Adjacent clusters that share the same allocation state and data
// source are merged into a single extent.
type MapExtent struct {
	// Offset is the virtual offset where the extent starts.
	Offset uint64
	// Length is the extent length in bytes (always a multiple of the cluster
	// size, except for the last extent of a disk whose virtual size is not a
	// multiple of the cluster size).
	Length uint64
	// Allocated tells whether the extent has data. When false, Depth,
	// HostOffset and ImagePath have no meaning.
	Allocated bool
	// Depth is the position of the image that holds the data in the backing
	// chain: 0 means the image itself, 1 its backing file, and so on.
	Depth uint32
	// HostOffset is the offset within the file at Depth where the extent data
	// lives.
	HostOffset uint64
	// ImagePath is the path of the image that holds the data.
	ImagePath string
}

// allocatedHostOffsetOnDisk reports whether the cluster containing
// virtualAddress is allocated according to the tables stored on disk, and
// returns the corresponding offset in the host file. The pointer table caches
// are bypassed so the result always reflects the file itself; call
// (*ImageFile).Flush before Map/Commit/Rebase if pending writes should be
// taken into account.
func allocatedHostOffsetOnDisk(imageFile *ImageFile, virtualAddress uint64) (uint64, bool, error) {
	header := imageFile.header
	l1Index := l1TableIndex(header, virtualAddress)
	if l1Index >= uint64(header.l1Size) {
		return 0, false, nil
	}
	l2ClusterAddress, err := imageFile.rawFile.readUint64At(header.l1TableOffset + 8*l1Index)
	if err != nil {
		return 0, false, err
	}
	l2ClusterAddress &= L1TableOffsetMask
	if l2ClusterAddress == 0 {
		return 0, false, nil
	}
	l2Entry, err := imageFile.rawFile.readUint64At(l2ClusterAddress + 8*l2TableIndex(header, virtualAddress))
	if err != nil {
		return 0, false, err
	}
	if l2Entry&CompressedFlag != 0 {
		return 0, false, fmt.Errorf("compressed clusters are not supported")
	}
	l2Entry &= L2TableOffsetMask
	if l2Entry == 0 {
		return 0, false, nil
	}
	return l2Entry + imageFile.rawFile.clusterOffset(virtualAddress), true, nil
}

// Map walks the image (and its backing chain) and reports the allocation
// state of every cluster of the virtual disk, merged into contiguous extents.
// The state is read from disk, so pending writes that were not flushed yet are
// not visible; call Flush beforehand to include them.
func (imageFile *ImageFile) Map() ([]MapExtent, error) {
	if imageFile.closed {
		return nil, fmt.Errorf("image %s is closed", imageFile.fullImagePath)
	}
	extents := make([]MapExtent, 0)
	clusterSize := imageFile.header.clusterSize
	virtualSize := imageFile.header.virtualDiskSizeBytes
	for virtualAddress := uint64(0); virtualAddress < virtualSize; virtualAddress += clusterSize {
		length := clusterSize
		if virtualAddress+clusterSize > virtualSize {
			length = virtualSize - virtualAddress
		}
		var extent MapExtent
		extent.Offset = virtualAddress
		extent.Length = length
		hostOffset, allocated, err := allocatedHostOffsetOnDisk(imageFile, virtualAddress)
		if err != nil {
			return nil, err
		}
		if allocated {
			extent.Allocated = true
			extent.Depth = 0
			extent.HostOffset = hostOffset
			extent.ImagePath = imageFile.fullImagePath
		} else {
			// Find the first backing file that has this cluster allocated.
			depth := uint32(0)
			for backing := imageFile.backingFile; backing != nil; backing = backing.backingFile {
				depth++
				backingHostOffset, backingAllocated, err := allocatedHostOffsetOnDisk(backing, virtualAddress)
				if err != nil {
					return nil, err
				}
				if backingAllocated {
					extent.Allocated = true
					extent.Depth = depth
					extent.HostOffset = backingHostOffset
					extent.ImagePath = backing.fullImagePath
					break
				}
			}
		}
		extents = appendMergedExtent(extents, extent)
	}
	return extents, nil
}

// appendMergedExtent appends extent to extents, merging it with the last
// extent when the two are contiguous and share the same allocation state,
// data source and (for allocated extents) host offsets.
func appendMergedExtent(extents []MapExtent, extent MapExtent) []MapExtent {
	if len(extents) > 0 {
		last := &extents[len(extents)-1]
		lastEnd := last.Offset + last.Length
		sameSource := last.Allocated == extent.Allocated &&
			last.Depth == extent.Depth &&
			last.ImagePath == extent.ImagePath
		hostContiguous := !extent.Allocated || extent.HostOffset == last.HostOffset+last.Length
		if lastEnd == extent.Offset && sameSource && hostContiguous {
			last.Length += extent.Length
			return extents
		}
	}
	return append(extents, extent)
}

// Commit merges the data allocated in this image into its backing file and
// then drops all allocations from this image, turning it into an empty layer
// that still reads everything from the same backing file. After a successful
// Commit the image returns exactly the same data as before, but the data now
// lives in the backing file. This is the equivalent of `qemu-img commit`.
//
// The image must have a backing file and must be opened read-write. Pending
// writes are flushed before the merge starts.
func (imageFile *ImageFile) Commit() error {
	if imageFile.closed {
		return fmt.Errorf("image %s is closed", imageFile.fullImagePath)
	}
	if imageFile.readOnly {
		return newErrWriteAttemptToReadOnlyDisk(0, 0)
	}
	if imageFile.backingFile == nil || imageFile.header.backingFilePath == nil {
		return newErrCommitImageHasNoBackingFile(imageFile.fullImagePath)
	}
	// Flush everything so that the on-disk tables are complete and consistent.
	if err := imageFile.syncCache(); err != nil {
		return err
	}

	backingPath := ResolveBackingFilePath(*imageFile.header.backingFilePath, imageFile.fullImagePath)
	backingImage, err := imageFile.factory.OpenImage(backingPath, backingFileMaxNestingDepth)
	if err != nil {
		return fmt.Errorf("while opening backing file %s for commit: %s", backingPath, err)
	}

	clusterSize := imageFile.header.clusterSize
	virtualSize := imageFile.header.virtualDiskSizeBytes
	buffer := make([]byte, clusterSize)
	l2ClustersToFree := make([]uint64, 0)
	dataClustersToFree := make([]uint64, 0)

	for virtualAddress := uint64(0); virtualAddress < virtualSize; virtualAddress += clusterSize {
		hostOffset, allocated, err := allocatedHostOffsetOnDisk(imageFile, virtualAddress)
		if err != nil {
			backingImage.Close()
			return err
		}
		if !allocated {
			continue
		}
		count := clusterSize
		if virtualAddress+clusterSize > virtualSize {
			count = virtualSize - virtualAddress
		}
		if err := imageFile.rawFile.ReadAt(buffer[:count], int64(hostOffset)); err != nil {
			backingImage.Close()
			return err
		}
		if err := backingImage.WriteAt(virtualAddress, buffer[:count]); err != nil {
			backingImage.Close()
			return err
		}
		dataClustersToFree = append(dataClustersToFree, hostOffset-imageFile.rawFile.clusterOffset(virtualAddress))
		l2Cluster, err := imageFile.l2ClusterAddressForDisk(virtualAddress)
		if err != nil {
			backingImage.Close()
			return err
		}
		l2ClustersToFree = append(l2ClustersToFree, l2Cluster)
	}
	// The data has been merged; make sure it is durably stored in the backing
	// file before this image's own allocations are dropped.
	if err := backingImage.syncCache(); err != nil {
		backingImage.Close()
		return err
	}
	if err := backingImage.rawFile.sync(); err != nil {
		backingImage.Close()
		return err
	}
	if err := backingImage.Close(); err != nil {
		return err
	}

	// The in-memory backing file handle was opened before the merge and its
	// cached tables are stale now. Close it and reopen the chain so that
	// subsequent reads see the merged data.
	if imageFile.backingFile != nil {
		if err := imageFile.backingFile.Close(); err != nil {
			return err
		}
		imageFile.backingFile = nil
	}
	reopenedBacking, err := imageFile.factory.getBackingFileImage(
		*imageFile.header.backingFilePath,
		imageFile.fullImagePath,
		backingFileMaxNestingDepth-1,
		backingFileMaxNestingDepth,
	)
	if err != nil {
		return err
	}
	imageFile.backingFile = reopenedBacking

	// Drop all allocations of this image. First clear the pointer tables and
	// persist them, so that the freed clusters are no longer referenced; only
	// afterwards release the reference counts of the data and L2 table
	// clusters. With this order an interruption between the two phases at
	// worst leaves unreferenced clusters marked as used (a minor leak that a
	// check can repair) and never a cluster that is both freed and still
	// referenced (corruption).
	if err := imageFile.pointerTable.clearTables(); err != nil {
		return err
	}
	if err := imageFile.syncCache(); err != nil {
		return err
	}
	for _, cluster := range dataClustersToFree {
		if err := imageFile.releaseCluster(cluster); err != nil {
			return err
		}
	}
	l2ClustersToFree = uniqueUint64s(l2ClustersToFree)
	for _, cluster := range l2ClustersToFree {
		if err := imageFile.releaseCluster(cluster); err != nil {
			return err
		}
	}
	if err := imageFile.syncCache(); err != nil {
		return err
	}
	return imageFile.Flush()
}

// l2ClusterAddressForDisk reads the L1 table from disk and returns the address
// of the L2 table cluster that maps virtualAddress.
func (imageFile *ImageFile) l2ClusterAddressForDisk(virtualAddress uint64) (uint64, error) {
	header := imageFile.header
	l1Index := l1TableIndex(header, virtualAddress)
	l2ClusterAddress, err := imageFile.rawFile.readUint64At(header.l1TableOffset + 8*l1Index)
	if err != nil {
		return 0, err
	}
	return l2ClusterAddress & L1TableOffsetMask, nil
}

// releaseCluster decrements the reference count of the cluster at address by
// one. When the reference count reaches zero the cluster is recorded so that
// it can be reused for future allocations.
func (imageFile *ImageFile) releaseCluster(address uint64) error {
	refcount, err := imageFile.referenceCounts.getClusterRefcount(address)
	if err != nil {
		return err
	}
	if refcount == 0 {
		return nil
	}
	newRefCount := refcount - 1
	droppedClusters, err := imageFile.setClusterRefcount(address, newRefCount)
	if err != nil {
		return err
	}
	imageFile.unrefClusters = append(imageFile.unrefClusters, droppedClusters...)
	if newRefCount == 0 {
		imageFile.unrefClusters = append(imageFile.unrefClusters, address)
	}
	return nil
}

func uniqueUint64s(values []uint64) []uint64 {
	seen := make(map[uint64]struct{}, len(values))
	result := make([]uint64, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

// Rebase changes the backing file of this image to newBackingFilePath while
// preserving the visible data of the image (safe rebase, the equivalent of
// `qemu-img rebase -b`). Every cluster of this image that is not allocated
// here is compared between the old and the new backing chain; when the two
// chains provide different data for such a cluster, the old data is copied
// into this image so nothing changes from the reader's point of view.
//
// Passing an empty newBackingFilePath removes the backing file entirely; all
// data that previously came from the backing chain is copied into this image.
//
// The new backing file's virtual size must be at least as large as this
// image's virtual size. Pending writes are flushed before the rebase starts.
func (imageFile *ImageFile) Rebase(newBackingFilePath string) error {
	if imageFile.closed {
		return fmt.Errorf("image %s is closed", imageFile.fullImagePath)
	}
	if imageFile.readOnly {
		return newErrWriteAttemptToReadOnlyDisk(0, 0)
	}
	if newBackingFilePath != "" {
		maxLength := uint32(imageFile.header.clusterSize) - V3BareHeaderSize - EmptyHeaderExtensionAreaSize
		if uint32(len(newBackingFilePath)) > maxLength {
			return newErrBackingFileNameTooLong(uint32(len(newBackingFilePath)))
		}
	}
	// Flush everything so that the on-disk tables are complete and consistent.
	if err := imageFile.syncCache(); err != nil {
		return err
	}

	var newBackingImage *ImageFile
	if newBackingFilePath != "" {
		var err error
		newBackingImage, err = imageFile.factory.getBackingFileImage(
			newBackingFilePath,
			imageFile.fullImagePath,
			backingFileMaxNestingDepth-1,
			backingFileMaxNestingDepth,
		)
		if err != nil {
			return err
		}
		defer newBackingImage.Close()
		if newBackingImage.header.virtualDiskSizeBytes < imageFile.header.virtualDiskSizeBytes {
			return newErrRebaseBackingImageTooSmall(
				newBackingFilePath,
				newBackingImage.header.virtualDiskSizeBytes,
				imageFile.header.virtualDiskSizeBytes,
			)
		}
	}

	clusterSize := imageFile.header.clusterSize
	virtualSize := imageFile.header.virtualDiskSizeBytes
	zeroCluster := make([]byte, clusterSize)
	for virtualAddress := uint64(0); virtualAddress < virtualSize; virtualAddress += clusterSize {
		_, allocatedHere, err := allocatedHostOffsetOnDisk(imageFile, virtualAddress)
		if err != nil {
			return err
		}
		if allocatedHere {
			continue
		}
		count := clusterSize
		if virtualAddress+clusterSize > virtualSize {
			count = virtualSize - virtualAddress
		}
		oldData, err := imageFile.ReadAt(virtualAddress, count)
		if err != nil {
			return err
		}
		var newData []byte
		if newBackingImage != nil {
			newData, err = newBackingImage.ReadAt(virtualAddress, count)
			if err != nil {
				return err
			}
		} else {
			newData = zeroCluster[:count]
		}
		if !bytes.Equal(oldData, newData) {
			// The new backing chain provides different data for this cluster,
			// so preserve the old data by allocating it in this image.
			if err := imageFile.WriteAt(virtualAddress, oldData); err != nil {
				return err
			}
		}
	}
	// Persist the copied data before the backing file reference is rewritten.
	if err := imageFile.syncCache(); err != nil {
		return err
	}

	// Update the header with the new backing file reference.
	header := imageFile.header
	if newBackingFilePath == "" {
		header.backingFilePath = nil
		header.backingFileOffset = 0
		header.backingFileSize = 0
	} else {
		backingPath := newBackingFilePath
		header.backingFilePath = &backingPath
		header.backingFileOffset = uint64(V3BareHeaderSize + EmptyHeaderExtensionAreaSize)
		header.backingFileSize = uint32(len(newBackingFilePath))
	}
	if err := imageFile.rawFile.writeHeaderOnly(header); err != nil {
		return err
	}
	if err := imageFile.rawFile.sync(); err != nil {
		return err
	}
	imageFile.header = header

	// Rewire the in-memory backing chain to match the new header.
	if imageFile.backingFile != nil {
		if err := imageFile.backingFile.Close(); err != nil {
			return err
		}
		imageFile.backingFile = nil
	}
	if newBackingFilePath != "" {
		reopened, err := imageFile.factory.getBackingFileImage(
			newBackingFilePath,
			imageFile.fullImagePath,
			backingFileMaxNestingDepth-1,
			backingFileMaxNestingDepth,
		)
		if err != nil {
			return err
		}
		imageFile.backingFile = reopened
	}
	return nil
}
