package ntfs

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

var (
	fileSignature = []byte{0x46, 0x49, 0x4c, 0x45}
)

const maxInt = int64(^uint(0) >> 1)

type MFTEntry struct {
	Signature             []byte
	FileReference         FileReference
	BaseRecordReference   FileReference
	LogFileSequenceNumber uint64
	HardLinkCount         int
	Flags                 RecordFlag
	ActualSize            uint32
	AllocatedSize         uint32
	NextAttributeId       int
	Attributes            []Attribute
}

func ParseMFTEntry(b []byte) (MFTEntry, error) {
	if len(b) < 42 {
		return MFTEntry{}, fmt.Errorf("record data length should be at least 42 but is %d", len(b))
	}
	sig := b[:4]
	if bytes.Compare(sig, fileSignature) != 0 {
		return MFTEntry{}, fmt.Errorf("unknown record signature: %# x", sig)
	}

	b = Duplicate(b)
	r := NewLittleEndianReader(b)
	baseRecordRef, err := ParseFileReference(r.Read(0x20, 8))
	if err != nil {
		return MFTEntry{}, fmt.Errorf("unable to parse base record reference: %v", err)
	}

	firstAttributeOffset := int(r.Uint16(0x14))
	if firstAttributeOffset < 0 || firstAttributeOffset >= len(b) {
		return MFTEntry{}, fmt.Errorf("invalid first attribute offset %d (data length: %d)", firstAttributeOffset, len(b))
	}

	updateSequenceOffset := int(r.Uint16(0x04))
	updateSequenceSize := int(r.Uint16(0x06))
	b, err = applyFixUp(b, updateSequenceOffset, updateSequenceSize)
	if err != nil {
		return MFTEntry{}, fmt.Errorf("unable to apply fixup: %v", err)
	}

	attributes, err := ParseAttributes(b[firstAttributeOffset:])
	if err != nil {
		return MFTEntry{}, err
	}
	return MFTEntry{
		Signature:             Duplicate(sig),
		FileReference:         FileReference{RecordNumber: uint64(r.Uint32(0x2C)), SequenceNumber: r.Uint16(0x10)},
		BaseRecordReference:   baseRecordRef,
		LogFileSequenceNumber: r.Uint64(0x08),
		HardLinkCount:         int(r.Uint16(0x12)),
		Flags:                 RecordFlag(r.Uint16(0x16)),
		ActualSize:            r.Uint32(0x18),
		AllocatedSize:         r.Uint32(0x1C),
		NextAttributeId:       int(r.Uint16(0x28)),
		Attributes:            attributes,
	}, nil
}

type FileReference struct {
	RecordNumber   uint64
	SequenceNumber uint16
}

func ParseFileReference(b []byte) (FileReference, error) {
	if len(b) != 8 {
		return FileReference{}, fmt.Errorf("expected 8 bytes but got %d", len(b))
	}

	return FileReference{
		RecordNumber:   binary.LittleEndian.Uint64(padTo(b[:6], 8)),
		SequenceNumber: binary.LittleEndian.Uint16(b[6:]),
	}, nil
}

type RecordFlag uint16

const (
	RecordFlagInUse       RecordFlag = 0x0001
	RecordFlagIsDirectory RecordFlag = 0x0002
	RecordFlagInExtend    RecordFlag = 0x0004
	RecordFlagIsIndex     RecordFlag = 0x0008
)

func (f *RecordFlag) Is(c RecordFlag) bool {
	return *f&c == c
}

func applyFixUp(b []byte, offset int, length int) ([]byte, error) {
	r := NewLittleEndianReader(b)

	updateSequence := r.Read(offset, length*2) // length is in pairs, not bytes
	updateSequenceNumber := updateSequence[:2]
	updateSequenceArray := updateSequence[2:]

	sectorCount := len(updateSequenceArray) / 2
	sectorSize := len(b) / sectorCount

	for i := 1; i <= sectorCount; i++ {
		offset := sectorSize*i - 2
		if bytes.Compare(updateSequenceNumber, b[offset:offset+2]) != 0 {
			return nil, fmt.Errorf("update sequence mismatch at pos %d", offset)
		}
	}

	for i := 0; i < sectorCount; i++ {
		offset := sectorSize*(i+1) - 2
		num := i * 2
		copy(b[offset:offset+2], updateSequenceArray[num:num+2])
	}

	return b, nil
}

// http://inform.pucp.edu.pe/~inf232/Ntfs/ntfs_doc_v0.5/concepts/fixup.html
func ApplyFixup(b []byte) ([]byte, error) {
	r := NewLittleEndianReader(b)
	updateSequenceOffset := int(r.Uint16(0x04))
	updateSequenceSize := int(r.Uint16(0x06))
	return applyFixUp(b, updateSequenceOffset, updateSequenceSize)
}

func (r *MFTEntry) FindAttributes(attrType AttributeType) []Attribute {
	ret := make([]Attribute, 0)
	for _, a := range r.Attributes {
		if a.Type == attrType {
			ret = append(ret, a)
		}
	}
	return ret
}

type Attribute struct {
	Type          AttributeType
	Resident      bool
	Name          string
	Flags         AttributeFlags
	AttributeId   int
	AllocatedSize uint64
	ActualSize    uint64
	Data          []byte
}

type AttributeType uint32

const (
	AttributeTypeStandardInformation AttributeType = 0x10       // $STANDARD_INFORMATION; always resident
	AttributeTypeAttributeList       AttributeType = 0x20       // $ATTRIBUTE_LIST; mixed residency
	AttributeTypeFileName            AttributeType = 0x30       // $FILE_NAME; always resident
	AttributeTypeObjectId            AttributeType = 0x40       // $OBJECT_ID; always resident
	AttributeTypeSecurityDescriptor  AttributeType = 0x50       // $SECURITY_DESCRIPTOR; always resident?
	AttributeTypeVolumeName          AttributeType = 0x60       // $VOLUME_NAME; always resident?
	AttributeTypeVolumeInformation   AttributeType = 0x70       // $VOLUME_INFORMATION; never resident?
	AttributeTypeData                AttributeType = 0x80       // $DATA; mixed residency
	AttributeTypeIndexRoot           AttributeType = 0x90       // $INDEX_ROOT; always resident
	AttributeTypeIndexAllocation     AttributeType = 0xa0       // $INDEX_ALLOCATION; never resident?
	AttributeTypeBitmap              AttributeType = 0xb0       // $BITMAP; nearly always resident?
	AttributeTypeReparsePoint        AttributeType = 0xc0       // $REPARSE_POINT; always resident?
	AttributeTypeEAInformation       AttributeType = 0xd0       // $EA_INFORMATION; always resident
	AttributeTypeEA                  AttributeType = 0xe0       // $EA; nearly always resident?
	AttributeTypePropertySet         AttributeType = 0xf0       // $PROPERTY_SET
	AttributeTypeLoggedUtilityStream AttributeType = 0x100      // $LOGGED_UTILITY_STREAM; always resident
	AttributeTypeTerminator          AttributeType = 0xFFFFFFFF // Indicates the last attribute in a list; will not actually be returned by ParseAttributes
)

type AttributeFlags uint16

// AttributeFlags比特位数据. 举例: 一个加密、压缩后的属性值为0x4001.
const (
	AttributeFlagsCompressed AttributeFlags = 0x0001
	AttributeFlagsEncrypted  AttributeFlags = 0x4000
	AttributeFlagsSparse     AttributeFlags = 0x8000
)

func (f *AttributeFlags) Is(c AttributeFlags) bool {
	return *f&c == c
}

func ParseAttributes(b []byte) ([]Attribute, error) {
	if len(b) == 0 {
		return []Attribute{}, nil
	}
	attributes := make([]Attribute, 0)
	for len(b) > 0 {
		if len(b) < 4 {
			return nil, fmt.Errorf("attribute header data should be at least 4 bytes but is %d", len(b))
		}

		r := NewLittleEndianReader(b)
		attrType := r.Uint32(0)
		if attrType == uint32(AttributeTypeTerminator) {
			break
		}

		if len(b) < 8 {
			return nil, fmt.Errorf("cannot read attribute header record length, data should be at least 8 bytes but is %d", len(b))
		}

		uRecordLength := r.Uint32(0x04)
		if int64(uRecordLength) > maxInt {
			return nil, fmt.Errorf("record length %d overflows maximum int value %d", uRecordLength, maxInt)
		}
		recordLength := int(uRecordLength)
		if recordLength <= 0 {
			return nil, fmt.Errorf("cannot handle attribute with zero or negative record length %d", recordLength)
		}

		if recordLength > len(b) {
			return nil, fmt.Errorf("attribute record length %d exceeds data length %d", recordLength, len(b))
		}

		recordData := r.Read(0, recordLength)
		attribute, err := ParseAttribute(recordData)
		if err != nil {
			return nil, err
		}
		attributes = append(attributes, attribute)
		b = r.ReadFrom(recordLength)
	}
	return attributes, nil
}

func ParseAttribute(b []byte) (Attribute, error) {
	if len(b) < 22 {
		return Attribute{}, fmt.Errorf("attribute data should be at least 22 bytes but is %d", len(b))
	}

	r := NewLittleEndianReader(b)

	nameLength := r.Byte(0x09)
	nameOffset := r.Uint16(0x0A)

	name := ""
	if nameLength != 0 {
		nameBytes := r.Read(int(nameOffset), int(nameLength)*2)
		name = DecodeString(nameBytes, binary.LittleEndian)
	}

	resident := r.Byte(0x08) == 0x00
	var attributeData []byte
	actualSize := uint64(0)
	allocatedSize := uint64(0)
	if resident {
		dataOffset := int(r.Uint16(0x14))
		uDataLength := r.Uint32(0x10)
		if int64(uDataLength) > maxInt {
			return Attribute{}, fmt.Errorf("attribute data length %d overflows maximum int value %d", uDataLength, maxInt)
		}
		dataLength := int(uDataLength)
		expectedDataLength := dataOffset + dataLength

		if len(b) < expectedDataLength {
			return Attribute{}, fmt.Errorf("expected attribute data length to be at least %d but is %d", expectedDataLength, len(b))
		}

		attributeData = r.Read(dataOffset, dataLength)
	} else {
		dataOffset := int(r.Uint16(0x20))
		if len(b) < dataOffset {
			return Attribute{}, fmt.Errorf("expected attribute data length to be at least %d but is %d", dataOffset, len(b))
		}
		allocatedSize = r.Uint64(0x28)
		actualSize = r.Uint64(0x30)
		attributeData = r.ReadFrom(int(dataOffset))
	}

	return Attribute{
		Type:          AttributeType(r.Uint32(0)),
		Resident:      resident,
		Name:          name,
		Flags:         AttributeFlags(r.Uint16(0x0C)),
		AttributeId:   int(r.Uint16(0x0E)),
		AllocatedSize: allocatedSize,
		ActualSize:    actualSize,
		Data:          Duplicate(attributeData),
	}, nil
}

type DataRun struct {
	OffsetCluster    int64
	LengthInClusters uint64
}

func ParseDataRuns(b []byte) ([]DataRun, error) {
	if len(b) == 0 {
		return []DataRun{}, nil
	}

	runs := make([]DataRun, 0)
	for len(b) > 0 {
		r := NewLittleEndianReader(b)
		header := r.Byte(0)
		if header == 0 {
			break
		}

		lengthLength := int(header &^ 0xF0)
		offsetLength := int(header >> 4)

		dataRunDataLength := offsetLength + lengthLength

		headerAndDataLength := dataRunDataLength + 1
		if len(b) < headerAndDataLength {
			return nil, fmt.Errorf("expected at least %d bytes of datarun data but is %d", headerAndDataLength, len(b))
		}

		dataRunData := r.Reader(1, dataRunDataLength)

		lengthBytes := dataRunData.Read(0, lengthLength)
		dataLength := binary.LittleEndian.Uint64(padTo(lengthBytes, 8))

		offsetBytes := dataRunData.Read(lengthLength, offsetLength)
		dataOffset := int64(binary.LittleEndian.Uint64(padTo(offsetBytes, 8)))

		runs = append(runs, DataRun{OffsetCluster: dataOffset, LengthInClusters: dataLength})

		b = r.ReadFrom(headerAndDataLength)
	}

	return runs, nil
}

func DataRunsToFragments(runs []DataRun, bytesPerCluster int) []Fragment {
	frags := make([]Fragment, len(runs))
	previousOffsetCluster := int64(0)
	for i, run := range runs {
		exactClusterOffset := previousOffsetCluster + run.OffsetCluster
		frags[i] = Fragment{
			Offset: exactClusterOffset * int64(bytesPerCluster),
			Length: int64(run.LengthInClusters) * int64(bytesPerCluster),
		}
		previousOffsetCluster = exactClusterOffset
	}
	return frags
}

func padTo(data []byte, length int) []byte {
	if len(data) > length {
		return data
	}
	if len(data) == length {
		return data
	}
	result := make([]byte, length)
	if len(data) == 0 {
		return result
	}
	copy(result, data)
	if data[len(data)-1]&0b10000000 == 0b10000000 {
		for i := len(data); i < length; i++ {
			result[i] = 0xFF
		}
	}
	return result
}

func (at AttributeType) Name() string {
	switch at {
	case AttributeTypeStandardInformation:
		return "$STANDARD_INFORMATION"
	case AttributeTypeAttributeList:
		return "$ATTRIBUTE_LIST"
	case AttributeTypeFileName:
		return "$FILE_NAME"
	case AttributeTypeObjectId:
		return "$OBJECT_ID"
	case AttributeTypeSecurityDescriptor:
		return "$SECURITY_DESCRIPTOR"
	case AttributeTypeVolumeName:
		return "$VOLUME_NAME"
	case AttributeTypeVolumeInformation:
		return "$VOLUME_INFORMATION"
	case AttributeTypeData:
		return "$DATA"
	case AttributeTypeIndexRoot:
		return "$INDEX_ROOT"
	case AttributeTypeIndexAllocation:
		return "$INDEX_ALLOCATION"
	case AttributeTypeBitmap:
		return "$BITMAP"
	case AttributeTypeReparsePoint:
		return "$REPARSE_POINT"
	case AttributeTypeEAInformation:
		return "$EA_INFORMATION"
	case AttributeTypeEA:
		return "$EA"
	case AttributeTypePropertySet:
		return "$PROPERTY_SET"
	case AttributeTypeLoggedUtilityStream:
		return "$LOGGED_UTILITY_STREAM"
	}
	return "unknown"
}
