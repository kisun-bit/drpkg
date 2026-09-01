// Copyright 2018-present Network Optix, Inc. Licensed under MPL 2.0: www.mozilla.org/MPL/2.0/
package qcow2

import (
	"fmt"

	"github.com/kisun-bit/drpkg/extend"
)

type ErrInvalidMagic struct {
	lineNumberString string
	magic            uint32
} // with value of a magic

func (err ErrInvalidMagic) Error() string {
	return fmt.Sprintf(
		"header magic must be equal to %x, actual value is %x",
		QcowMagic,
		err.magic,
	)
}

func (err ErrInvalidMagic) TraceInfo() string {
	return err.lineNumberString
}

func newErrInvalidMagic(magic uint32) error {
	return &ErrInvalidMagic{
		lineNumberString: extend.GetTraceInfo(),
		magic:            magic,
	}
}

type ErrBackingFileNameTooLong struct {
	lineNumberString    string
	backingFileNameSize uint32
} // with backing file size (backing file name size actually)

func (err ErrBackingFileNameTooLong) Error() string {
	return fmt.Sprintf(
		"backing file size is %d, valid value is < %d",
		err.backingFileNameSize,
		maxBackingFileNameSize,
	)
}

func (err ErrBackingFileNameTooLong) TraceInfo() string {
	return err.lineNumberString
}

func newErrBackingFileNameTooLong(backingFileNameSize uint32) error {
	return &ErrBackingFileNameTooLong{
		lineNumberString:    extend.GetTraceInfo(),
		backingFileNameSize: backingFileNameSize,
	}
}

type ErrBackingFileNameNonZeroOffsetZeroLength struct {
	lineNumberString  string
	backingFileOffset uint64
}

func (err ErrBackingFileNameNonZeroOffsetZeroLength) Error() string {
	return fmt.Sprintf(
		"header with backing file offset %d has zero backing file size",
		err.backingFileOffset,
	)
}

func (err ErrBackingFileNameNonZeroOffsetZeroLength) TraceInfo() string {
	return err.lineNumberString
}

func newErrBackingFileNameNonZeroOffsetZeroLength(backingFileOffset uint64) error {
	return &ErrBackingFileNameNonZeroOffsetZeroLength{
		lineNumberString:  extend.GetTraceInfo(),
		backingFileOffset: backingFileOffset,
	}
}

type ErrBackingFileZeroOffsetNameLengthNotZero struct {
	lineNumberString      string
	backingFileNameLength uint32
} //  with length

func (err ErrBackingFileZeroOffsetNameLengthNotZero) Error() string {
	return fmt.Sprintf(
		"header with backing file offset 0 has non zero backing size %d",
		err.backingFileNameLength,
	)
}

func (err ErrBackingFileZeroOffsetNameLengthNotZero) TraceInfo() string {
	return err.lineNumberString
}

func newErrBackingFileZeroOffsetNameLengthNotZero(backingFileNameLength uint32) error {
	return &ErrBackingFileZeroOffsetNameLengthNotZero{
		lineNumberString:      extend.GetTraceInfo(),
		backingFileNameLength: backingFileNameLength,
	}
}

type ErrInvalidClusterBits struct {
	lineNumberString string
	clusterBits      uint32
} // with cluster bits value

func (err ErrInvalidClusterBits) Error() string {
	return fmt.Sprintf(
		"cluster bits must be in (%d, %d), not %d",
		MinClusterBits,
		MaxClusterBits,
		err.clusterBits,
	)
}

func (err ErrInvalidClusterBits) TraceInfo() string {
	return err.lineNumberString
}

func newErrInvalidClusterBits(clusterBits uint32) error {
	return &ErrInvalidClusterBits{
		lineNumberString: extend.GetTraceInfo(),
		clusterBits:      clusterBits,
	}
}

type ErrUnsupportedCryptMethod struct {
	lineNumberString string
	cryptMethod      uint32
} // with crypt method value

func (err ErrUnsupportedCryptMethod) Error() string {
	switch err.cryptMethod {
	case 1:
		return "support of AES encryption is not yet implemented"
	case 2:
		return "support of LUKS encryption is not yet implemented"
	default:
		return fmt.Sprintf("unkown cryptMethod field %d", err.cryptMethod)
	}
}

func (err ErrUnsupportedCryptMethod) TraceInfo() string {
	return err.lineNumberString
}

func newErrUnsupportedCryptMethod(cryptMethod uint32) error {
	return &ErrUnsupportedCryptMethod{
		lineNumberString: extend.GetTraceInfo(),
		cryptMethod:      cryptMethod,
	}
}

type ErrL1TableTooLarge struct {
	lineNumberString string
	l1Size           uint32
} // w value

func (err ErrL1TableTooLarge) Error() string {
	return fmt.Sprintf(
		"l1 table size is %d but must not exceed %d",
		err.l1Size,
		L1TableMaxSize,
	)
}

func (err ErrL1TableTooLarge) TraceInfo() string {
	return err.lineNumberString
}

func newErrL1TableTooLarge(l1Size uint32) error {
	return &ErrL1TableTooLarge{
		lineNumberString: extend.GetTraceInfo(),
		l1Size:           l1Size,
	}
}

type ErrL1OffsetExceedsFileBoundaries struct {
	lineNumberString string
	l1TableOffset    uint64
	fileSize         uint64
}

func (err ErrL1OffsetExceedsFileBoundaries) Error() string {
	return fmt.Sprintf(
		"L1 table offset %d exceeds the image file size %d",
		err.l1TableOffset,
		err.fileSize,
	)
}

func (err ErrL1OffsetExceedsFileBoundaries) TraceInfo() string {
	return err.lineNumberString
}

func newErrL1OffsetExceedsFileBoundaries(l1TableOffset, fileSize uint64) error {
	return &ErrL1OffsetExceedsFileBoundaries{
		lineNumberString: extend.GetTraceInfo(),
		l1TableOffset:    l1TableOffset,
		fileSize:         fileSize,
	}
}

type ErrNoReferenceCountTableClusters struct {
	lineNumberString string
}

func (err ErrNoReferenceCountTableClusters) Error() string {
	return "no reference count table clusters"
}

func (err ErrNoReferenceCountTableClusters) TraceInfo() string {
	return err.lineNumberString
}

func newErrNoReferenceCountTableClusters() error {
	return &ErrNoReferenceCountTableClusters{
		lineNumberString: extend.GetTraceInfo(),
	}
}

type ErrSnapshotsUnsupported struct {
	lineNumberString string
	nbSnapshots      uint32
} // with number of snapshots

func (err ErrSnapshotsUnsupported) Error() string {
	return fmt.Sprintf(
		"snapshots are not yet supported, number of snapshots is %d != 0",
		err.nbSnapshots,
	)
}

func (err ErrSnapshotsUnsupported) TraceInfo() string {
	return err.lineNumberString
}

func newErrSnapshotsUnsupported(nbSnapshots uint32) error {
	return &ErrSnapshotsUnsupported{
		lineNumberString: extend.GetTraceInfo(),
		nbSnapshots:      nbSnapshots,
	}
}

type ErrCompressionNotSupported struct {
	lineNumberString string
}

func (err ErrCompressionNotSupported) Error() string {
	return "compression type bit has been set, but non default compression is not yet supported"
}

func (err ErrCompressionNotSupported) TraceInfo() string {
	return err.lineNumberString
}

func newErrCompressionNotSupported() error {
	return &ErrCompressionNotSupported{
		lineNumberString: extend.GetTraceInfo(),
	}
}

type ErrExternalDataFileNotSupported struct {
	lineNumberString string
}

func (err ErrExternalDataFileNotSupported) Error() string {
	return "external data files are not supported"
}

func (err ErrExternalDataFileNotSupported) TraceInfo() string {
	return err.lineNumberString
}

func newErrExternalDataFileNotSupported() error {
	return &ErrExternalDataFileNotSupported{
		lineNumberString: extend.GetTraceInfo(),
	}
}

type ErrExtendedL2EntriesNotSupported struct {
	lineNumberString string
}

func (err ErrExtendedL2EntriesNotSupported) Error() string {
	return "extended L2 entries are not yet supported"
}

func (err ErrExtendedL2EntriesNotSupported) TraceInfo() string {
	return err.lineNumberString
}

func newErrExtendedL2EntriesNotSupported() error {
	return &ErrExtendedL2EntriesNotSupported{
		lineNumberString: extend.GetTraceInfo(),
	}
}

type ErrRawExternalDataNotSupported struct {
	lineNumberString string
}

func (err ErrRawExternalDataNotSupported) Error() string {
	return "raw external data is not yet supported"
}

func (err ErrRawExternalDataNotSupported) TraceInfo() string {
	return err.lineNumberString
}

func newErrRawExternalDataNotSupported() error {
	return &ErrRawExternalDataNotSupported{
		lineNumberString: extend.GetTraceInfo(),
	}
}

type ErrBitmapExtensionsNotSupported struct {
	lineNumberString string
}

func (err ErrBitmapExtensionsNotSupported) Error() string {
	return "bitmap extensions are not yet supported"
}

func (err ErrBitmapExtensionsNotSupported) TraceInfo() string {
	return err.lineNumberString
}

func newErrBitmapExtensionsNotSupported() error {
	return &ErrBitmapExtensionsNotSupported{
		lineNumberString: extend.GetTraceInfo(),
	}
}

type ErrInvalidReferenceCountOrder struct {
	lineNumberString    string
	referenceCountOrder uint32
} //

func (err ErrInvalidReferenceCountOrder) Error() string {
	return fmt.Sprintf(
		"despite that spec supports valus < 6, "+
			"only refcount order 4 (two byte reference counts are supported), %d received",
		err.referenceCountOrder,
	)
}

func (err ErrInvalidReferenceCountOrder) TraceInfo() string {
	return err.lineNumberString
}

func newErrInvalidReferenceCountOrder(referenceCountOrder uint32) error {
	return &ErrInvalidReferenceCountOrder{
		lineNumberString:    extend.GetTraceInfo(),
		referenceCountOrder: referenceCountOrder,
	}
}

type ErrInvalidHeaderLength struct {
	lineNumberString string
	headerLength     uint32
} // headerSize w size

func (err ErrInvalidHeaderLength) Error() string {
	return fmt.Sprintf(
		"header_length field must be > %d for QCOW2 version 3 disks, %d received",
		V3BareHeaderSize,
		err.headerLength,
	)
}

func (err ErrInvalidHeaderLength) TraceInfo() string {
	return err.lineNumberString
}

func newErrInvalidHeaderLength(headerLength uint32) error {
	return &ErrInvalidHeaderLength{
		lineNumberString: extend.GetTraceInfo(),
		headerLength:     headerLength,
	}
}

type ErrOffsetIsNotAClusterBoundary struct {
	lineNumberString string
	offset           uint64
	clusterSize      uint64
} // offset, clusterSize

func (err ErrOffsetIsNotAClusterBoundary) Error() string {
	return fmt.Sprintf(
		"offset %d is not a cluster boundary for cluster size %d",
		err.offset,
		err.clusterSize,
	)
}

func (err ErrOffsetIsNotAClusterBoundary) TraceInfo() string {
	return err.lineNumberString
}

func newErrOffsetIsNotAClusterBoundary(offset, clusterSize uint64) error {
	return &ErrOffsetIsNotAClusterBoundary{
		lineNumberString: extend.GetTraceInfo(),
		offset:           offset,
		clusterSize:      clusterSize,
	}
}

type ErrReferenceCountTableTooLarge struct {
	lineNumberString string
} // (size)

func (err ErrReferenceCountTableTooLarge) Error() string {
	return ""
}

func (err ErrReferenceCountTableTooLarge) TraceInfo() string {
	return err.lineNumberString
}

type ErrTooManyReferenceCountClusters struct {
	lineNumberString string
}

func (err ErrTooManyReferenceCountClusters) Error() string {
	return ""
}

func (err ErrTooManyReferenceCountClusters) TraceInfo() string {
	return err.lineNumberString
}

type ErrHeaderWrite struct {
	lineNumberString string
}

func (err ErrHeaderWrite) Error() string {
	return "error while writing image header to a file"
}

func (err ErrHeaderWrite) TraceInfo() string {
	return err.lineNumberString
}

func newErrHeaderWrite() error {
	return &ErrHeaderWrite{
		lineNumberString: extend.GetTraceInfo(),
	}
}

type ErrInvalidVersion struct {
	versionNumber    uint32
	lineNumberString string
}

func (err ErrInvalidVersion) Error() string {
	return fmt.Sprintf("only QCOW2 disk version 3 is supported now, received %d", err.versionNumber)
}

func (err ErrInvalidVersion) TraceInfo() string {
	return err.lineNumberString
}

func newErrInvalidVersion(versionNumber uint32) error {
	return &ErrInvalidVersion{
		lineNumberString: extend.GetTraceInfo(),
		versionNumber:    versionNumber,
	}
}
