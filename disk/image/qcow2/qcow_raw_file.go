// Copyright 2018-present Network Optix, Inc. Licensed under MPL 2.0: www.mozilla.org/MPL/2.0/
package qcow2

import (
	"encoding/binary"
	"fmt"
	"math/bits"
	"os"
	"sync"
)

// QcowRawFile A qcow file. Allows reading/writing clusters and appending clusters.
type QcowRawFile struct {
	file        *os.File
	clusterSize uint64
	clusterMask uint64
	readOnly    bool
}

// Creates a `QcowRawFile` from the given `File`, `None` is returned if `cluster_size` is not
// a power of two.
func qcowRawFileFromFile(file *os.File, clusterSize uint64, readOnly bool) (*QcowRawFile, error) {
	if bits.OnesCount(uint(clusterSize)) != 1 {
		return nil, fmt.Errorf("invalid cluster size %d, must be power of two", clusterSize)
	}
	return &QcowRawFile{
		file:        file,
		clusterSize: clusterSize,
		clusterMask: clusterSize - 1,
		readOnly:    readOnly,
	}, nil
}

// read only methods

func (rawFile QcowRawFile) isReadOnly() bool {
	return rawFile.readOnly
}

func (rawFile QcowRawFile) size() (uint64, error) {
	stat, err := rawFile.file.Stat()
	if err != nil {
		return 0, err
	}
	return uint64(stat.Size()), nil
}

func (rawFile QcowRawFile) close() error {
	return rawFile.file.Close()
}

func (rawFile QcowRawFile) ReadAt(bytes []byte, offset int64) error {
	_, err := rawFile.file.ReadAt(bytes, offset)
	return err
}

func (rawFile QcowRawFile) readUint16At(offset uint64) (uint16, error) {
	return readUint16At(rawFile.file, offset)
}

func (rawFile QcowRawFile) readUint64At(offset uint64) (uint64, error) {
	return readUint64At(rawFile.file, offset)
}

// Reads `count` 64 bit offsets and returns them as an uint64 array.
// `mask` optionally ands out some of the bits on the file.
func (rawFile QcowRawFile) readPointerTable(
	offset uint64,
	count uint64,
	mask uint64,
) ([]uint64, error) {
	table := make([]uint64, count)
	buffer := make([]byte, count*8)
	if _, err := rawFile.file.ReadAt(buffer, int64(offset)); err != nil {
		return nil, err
	}
	if mask == 0 { // to avoid using optional, replace empty mask with 0
		// since mask can't be zero normally
		mask = ^uint64(0)
	}
	for index := range table {
		table[index] = binary.BigEndian.Uint64(buffer[index*8:]) & mask
	}
	return table, nil
}

// Read cluster containing pointers to other clusters
func (rawFile QcowRawFile) readPointerCluster(offset uint64, mask uint64) ([]uint64, error) {
	count := rawFile.clusterSize / uint64(8)
	value, err := rawFile.readPointerTable(offset, count, mask)
	return value, err
}

func (rawFile QcowRawFile) readRefCountBlock(offset uint64) ([]uint16, error) {
	uint16Size := uint64(2) // todo here reference count bits are used
	count := rawFile.clusterSize / uint16Size
	table := make([]uint16, count)
	buffer := make([]byte, rawFile.clusterSize)
	if _, err := rawFile.file.ReadAt(buffer, int64(offset)); err != nil {
		return nil, err
	}
	for index := range table {
		table[index] = binary.BigEndian.Uint16(buffer[index*2:])
	}
	return table, nil
}

func (rawFile QcowRawFile) clusterOffset(address uint64) uint64 {
	return address & rawFile.clusterMask
}

// Limits the range so that it doesn't overflow the end of a cluster.
func (rawFile QcowRawFile) limitRangeCluster(address uint64, count uint64) uint64 {
	offset := rawFile.clusterOffset(address)
	limit := rawFile.clusterSize - offset
	if count < limit {
		return count
	}
	return limit
}

// write methods need to check for read only
func (rawFile QcowRawFile) sync() error {
	if rawFile.readOnly {
		return newErrAttemptToSyncReadOnlyFile()
	}
	return rawFile.file.Sync()
}

func (rawFile QcowRawFile) WriteAt(bytes []byte, offset int64) error {
	if rawFile.readOnly {
		return newErrWriteAttemptToReadOnlyDisk(uint64(offset), uint64(len(bytes)))
	}
	_, err := rawFile.file.WriteAt(bytes, offset)
	return err
}

func (rawFile QcowRawFile) writeUint64At(value, offset uint64) error {
	if rawFile.readOnly {
		return newErrWriteAttemptToReadOnlyDisk(offset, 8)
	}
	return writeUint64At(rawFile.file, value, offset)
}

func (rawFile QcowRawFile) writeUint16At(value uint16, offset uint64) error {
	if rawFile.readOnly {
		return newErrWriteAttemptToReadOnlyDisk(offset, 2)
	}
	return writeUint16At(rawFile.file, value, offset)
}

func (rawFile QcowRawFile) writeHeader(header ImageHeader) error {
	if rawFile.readOnly {
		return newErrWriteAttemptToReadOnlyDisk(0, uint64(header.Length))
	}
	_, err := rawFile.file.Seek(0, 0)
	if err != nil {
		return err
	}
	return header.writeToFile(rawFile.file)
}

// writeHeaderOnly writes the header (including the backing file name) without
// extending the file, so it can be used on images that already contain data.
func (rawFile QcowRawFile) writeHeaderOnly(header ImageHeader) error {
	if rawFile.readOnly {
		return newErrWriteAttemptToReadOnlyDisk(0, uint64(header.Length))
	}
	return header.writeHeaderOnly(rawFile.file)
}

// Writes `table` of uint64 pointers to `offset` in the file.
// `non_zero_flags` will be ORed with all non-zero values in `table`.
// writing.
func (rawFile QcowRawFile) writePointerTable(
	offset uint64,
	table []uint64,
	nonZeroFlags uint64,
) error {
	if rawFile.readOnly {
		return newErrWriteAttemptToReadOnlyDisk(offset, uint64(len(table)*8))
	}
	toWrite := make([]byte, len(table)*8)
	for index, value := range table {
		if value != 0 {
			value |= nonZeroFlags
		}
		binary.BigEndian.PutUint64(toWrite[index*8:], value)
	}
	_, err := rawFile.file.WriteAt(toWrite, int64(offset))
	return err
}

func (rawFile QcowRawFile) writeRefcountBlock(offset uint64, table []uint16) error {
	if rawFile.readOnly {
		return newErrWriteAttemptToReadOnlyDisk(offset, uint64(len(table)*2))
	}
	toWrite := make([]byte, len(table)*2)
	for index, value := range table {
		binary.BigEndian.PutUint16(toWrite[index*2:], value)
	}
	_, err := rawFile.file.WriteAt(toWrite, int64(offset))
	return err
}

func (rawFile QcowRawFile) allocateClusterAtFileEnd(maxValidClusterOffset uint64) (uint64, error) {
	if rawFile.readOnly {
		return 0, newErrAttemptToTruncateReadOnlyFile()
	}
	fileEnd, err := rawFile.file.Seek(0, 2)
	if err != nil {
		return 0, err
	}
	newClusterAddress := (uint64(fileEnd) + rawFile.clusterSize - uint64(1)) & (^rawFile.clusterMask)
	if newClusterAddress > maxValidClusterOffset {
		return 0, fmt.Errorf("wrong new cluster address")
	}
	err = rawFile.file.Truncate(int64(newClusterAddress + rawFile.clusterSize))
	if err != nil {
		return 0, err
	}
	return newClusterAddress, nil
}

// zeroBuffer is a lazily grown, shared all-zero buffer used to zero out new
// clusters without allocating a fresh cluster sized buffer on every call.
// The buffer is only ever read from (never written to), so its content stays
// zero; the mutex only guards the grow operation.
var zeroBuffer struct {
	sync.Mutex
	data []byte
}

func zeroBytes(size uint64) []byte {
	zeroBuffer.Lock()
	defer zeroBuffer.Unlock()
	if uint64(cap(zeroBuffer.data)) < size {
		zeroBuffer.data = make([]byte, size)
	}
	return zeroBuffer.data[:size]
}

func (rawFile QcowRawFile) zeroCluster(address uint64) error {
	_, err := rawFile.file.WriteAt(zeroBytes(rawFile.clusterSize), int64(address))
	return err
}

func (rawFile QcowRawFile) writeCluster(address uint64, initialData []byte) error {
	if rawFile.readOnly {
		return newErrWriteAttemptToReadOnlyDisk(address, uint64(len(initialData)))
	}
	// crossvm uses write_volatile_at,
	// which is actually pwrite64 in a loop
	// (in order ot handle signal interrupt),
	// using also an unsafe pointer cast.
	// See: https://google.github.io/crosvm/doc/src/base/sys/unix/file_traits.rs.html
	// EINTR is being handled in FD.PWrite in golang which
	// is called in WriteAt API call.
	if uint64(len(initialData)) > rawFile.clusterSize {
		return fmt.Errorf(
			"initial data of size %d is larger than the cluster size %d",
			len(initialData),
			rawFile.clusterSize,
		)
	}
	// The last cluster of a virtual disk can be partial when the virtual
	// disk size is not a multiple of the cluster size. In that case the
	// initial data (for example read from a backing file) is shorter than
	// a cluster and must be zero padded up to the cluster size.
	if uint64(len(initialData)) < rawFile.clusterSize {
		paddedData := make([]byte, rawFile.clusterSize)
		copy(paddedData, initialData)
		initialData = paddedData
	}
	_, err := rawFile.file.WriteAt(initialData, int64(address))
	if err != nil {
		return err
	}
	return nil
}
