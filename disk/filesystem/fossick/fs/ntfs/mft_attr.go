package ntfs

import (
	"encoding/binary"
	"fmt"
	"time"
)

// StandardInformation
// @Description: $STANDARD_INFORMATION属性(0x10)
//
//	属性区占72个字节,其中OwnerId、SecurityId、QuotaCharged、UpdateSequenceNumber为非必要数据,
//	因此,属性区至少占用48字节
//	见 http://inform.pucp.edu.pe/~inf232/Ntfs/ntfs_doc_v0.5/attributes/standard_information.html 定义
type StandardInformation struct {
	Creation                time.Time       // 0x00	8	 	C Time - File Creation
	FileLastModified        time.Time       // 0x08	8	 	A Time - File Altered
	MftLastModified         time.Time       // 0x10	8	 	M Time - MFT Changed
	LastAccess              time.Time       // 0x18	8	 	R Time - File Read
	FilePermissions         FilePermissions // 0x20	4	 	DOS File Permissions
	MaximumNumberOfVersions uint32          // 0x24	4	 	Maximum Number of Versions
	VersionNumber           uint32          // 0x28	4	 	Version Number
	ClassId                 uint32          // 0x2C	4	 	Class Id
	OwnerId                 uint32          // 0x30	4	2K	Owner Id
	SecurityId              uint32          // 0x34	4	2K	Security Id
	QuotaCharged            uint64          // 0x38	8	2K	Quota Charged
	UpdateSequenceNumber    uint64          // 0x40	8	2K	Update Sequence Number (USN)
}

// ParseStandardInformation
//
//	@Description: 将字节数据结构化为$STANDARD_INFORMATION属性
//	@param b 原始数据
//	@return si $STANDARD_INFORMATION属性
//	@return err --
func ParseStandardInformation(b []byte) (si StandardInformation, err error) {
	if len(b) < 48 {
		err = fmt.Errorf("expected a least %d bytes but got %d", 48, len(b))
		return
	}
	r := NewLittleEndianReader(b)

	ownerId := uint32(0)
	securityId := uint32(0)
	quotaCharged := uint64(0)
	updateSequenceNumber := uint64(0)

	if len(b) >= 0x30+4 {
		ownerId = r.Uint32(0x30)
	}
	if len(b) >= 0x34+4 {
		securityId = r.Uint32(0x34)
	}
	if len(b) >= 0x38+8 {
		quotaCharged = r.Uint64(0x38)
	}
	if len(b) >= 0x40+8 {
		updateSequenceNumber = r.Uint64(0x40)
	}

	return StandardInformation{
		Creation:                ConvertFileTime(r.Uint64(0x00)),
		FileLastModified:        ConvertFileTime(r.Uint64(0x08)),
		MftLastModified:         ConvertFileTime(r.Uint64(0x10)),
		LastAccess:              ConvertFileTime(r.Uint64(0x18)),
		FilePermissions:         FilePermissions(r.Uint32(0x20)),
		MaximumNumberOfVersions: r.Uint32(0x24),
		VersionNumber:           r.Uint32(0x28),
		ClassId:                 r.Uint32(0x2C),
		OwnerId:                 ownerId,
		SecurityId:              securityId,
		QuotaCharged:            quotaCharged,
		UpdateSequenceNumber:    updateSequenceNumber,
	}, nil
}

type FilePermissions uint32

// FilePermissions的比特位数据. 例如:一个普通文件或隐藏文件其值为0x0082.
const (
	FilePermissionsReadOnly          FilePermissions = 0x0001
	FilePermissionsHidden            FilePermissions = 0x0002
	FilePermissionsSystem            FilePermissions = 0x0004
	FilePermissionsArchive           FilePermissions = 0x0020
	FilePermissionsDevice            FilePermissions = 0x0040
	FilePermissionsNormal            FilePermissions = 0x0080
	FilePermissionsTemporary         FilePermissions = 0x0100
	FilePermissionsSparseFile        FilePermissions = 0x0200
	FilePermissionsReparsePoint      FilePermissions = 0x0400
	FilePermissionsCompressed        FilePermissions = 0x1000
	FilePermissionsOffline           FilePermissions = 0x1000
	FilePermissionsNotContentIndexed FilePermissions = 0x2000
	FilePermissionsEncrypted         FilePermissions = 0x4000
)

// Equal
//
//	@Description: 比较两个FilePermissions是否相等
//	@receiver a
//	@param c
//	@return bool
func (a *FilePermissions) Equal(c FilePermissions) bool {
	return *a&c == c
}

type FileNameNamespace byte

const (
	FileNameNamespacePosix    FileNameNamespace = 0
	FileNameNamespaceWin32    FileNameNamespace = 1
	FileNameNamespaceDos      FileNameNamespace = 2
	FileNameNamespaceWin32Dos FileNameNamespace = 3
)

// FileName
// @Description: $FILE_NAME属性(0x30)
//
//	http://inform.pucp.edu.pe/~inf232/Ntfs/ntfs_doc_v0.5/attributes/file_name.html
type FileName struct {
	ParentFileReference FileReference
	Creation            time.Time
	FileLastModified    time.Time
	MftLastModified     time.Time
	LastAccess          time.Time
	AllocatedSize       uint64
	ActualSize          uint64
	Flags               FilePermissions
	ExtendedData        uint32
	Namespace           FileNameNamespace
	Name                string
}

func ParseFileName(b []byte) (FileName, error) {
	if len(b) < 66 {
		return FileName{}, fmt.Errorf("expected at least %d bytes but got %d", 66, len(b))
	}

	fileNameLength := int(b[0x40 : 0x40+1][0]) * 2
	minExpectedSize := 66 + fileNameLength
	if len(b) < minExpectedSize {
		return FileName{}, fmt.Errorf("expected at least %d bytes but got %d", minExpectedSize, len(b))
	}

	r := NewLittleEndianReader(b)
	parentRef, err := ParseFileReference(r.Read(0x00, 8))
	if err != nil {
		return FileName{}, fmt.Errorf("unable to parse file reference: %v", err)
	}
	return FileName{
		ParentFileReference: parentRef,
		Creation:            ConvertFileTime(r.Uint64(0x08)),
		FileLastModified:    ConvertFileTime(r.Uint64(0x10)),
		MftLastModified:     ConvertFileTime(r.Uint64(0x18)),
		LastAccess:          ConvertFileTime(r.Uint64(0x20)),
		AllocatedSize:       r.Uint64(0x28),
		ActualSize:          r.Uint64(0x30),
		Flags:               FilePermissions(r.Uint32(0x38)),
		ExtendedData:        r.Uint32(0x3c),
		Namespace:           FileNameNamespace(r.Byte(0x41)),
		Name:                DecodeString(r.Read(0x42, fileNameLength), binary.LittleEndian),
	}, nil
}

// AttributeListEntry
// @Description: $20属性
type AttributeListEntry struct {
	Type                AttributeType
	Name                string
	StartingVCN         uint64
	BaseRecordReference FileReference
	AttributeId         uint16
}

func ParseAttributeList(b []byte) ([]AttributeListEntry, error) {
	if len(b) < 26 {
		return []AttributeListEntry{}, fmt.Errorf("expected at least %d bytes but got %d", 26, len(b))
	}

	entries := make([]AttributeListEntry, 0)

	for len(b) > 0 {
		r := NewLittleEndianReader(b)
		entryLength := int(r.Uint16(0x04))
		if len(b) < entryLength {
			return entries, fmt.Errorf("expected at least %d bytes remaining for AttributeList entry but is %d", entryLength, len(b))
		}
		nameLength := int(r.Byte(0x06))
		name := ""
		if nameLength != 0 {
			nameOffset := int(r.Byte(0x07))
			name = DecodeString(r.Read(nameOffset, nameLength*2), binary.LittleEndian)
		}
		baseRef, err := ParseFileReference(r.Read(0x10, 8))
		if err != nil {
			return entries, fmt.Errorf("unable to parse base record reference: %v", err)
		}
		entry := AttributeListEntry{
			Type:                AttributeType(r.Uint32(0)),
			Name:                name,
			StartingVCN:         r.Uint64(0x08),
			BaseRecordReference: baseRef,
			AttributeId:         r.Uint16(0x18),
		}
		entries = append(entries, entry)
		b = r.ReadFrom(entryLength)
	}
	return entries, nil
}

// CollationType indicates how the entries in an index should be ordered.
type CollationType uint32

const (
	CollationTypeBinary            CollationType = 0x00000000
	CollationTypeFileName          CollationType = 0x00000001
	CollationTypeUnicodeString     CollationType = 0x00000002
	CollationTypeNtofsULong        CollationType = 0x00000010
	CollationTypeNtofsSid          CollationType = 0x00000011
	CollationTypeNtofsSecurityHash CollationType = 0x00000012
	CollationTypeNtofsUlongs       CollationType = 0x00000013
)

// IndexRoot
// @Description: $90属性
type IndexRoot struct {
	AttributeType     AttributeType
	CollationType     CollationType
	BytesPerRecord    uint32
	ClustersPerRecord uint32
	Flags             uint32
	Entries           []IndexEntry
}

type IndexEntry struct {
	FileReference FileReference
	Flags         uint32
	FileName      FileName
	SubNodeVCN    uint64
}

// http://inform.pucp.edu.pe/~inf232/Ntfs/ntfs_doc_v0.5/concepts/index_header.html
type IndexBlock struct {
	Signature            string
	UpdateSequenceOffset uint16
	UpdateSequenceSize   uint16
	UpdateSequenceNumber uint16
	LSN                  uint64 // $LogFile Sequence Number
	EntryOffset          uint32
	TotalEntrySize       uint32
	AllocEntrySize       uint32
	NotLeaf              byte
}

func ParseIndexRoot(b []byte) (IndexRoot, error) {
	if len(b) < 32 {
		return IndexRoot{}, fmt.Errorf("expected at least %d bytes but got %d", 32, len(b))
	}
	r := NewLittleEndianReader(b)
	attributeType := AttributeType(r.Uint32(0x00))
	if attributeType != AttributeTypeFileName {
		return IndexRoot{}, fmt.Errorf("unable to handle attribute type %d (%s) in $INDEX_ROOT", attributeType, attributeType.Name())
	}

	uTotalSize := r.Uint32(0x14)
	if int64(uTotalSize) > maxInt {
		return IndexRoot{}, fmt.Errorf("index root size %d overflows maximum int value %d", uTotalSize, maxInt)
	}
	totalSize := int(uTotalSize)
	expectedSize := totalSize + 16
	if len(b) < expectedSize {
		return IndexRoot{}, fmt.Errorf("expected %d bytes in $INDEX_ROOT but is %d", expectedSize, len(b))
	}
	entries := []IndexEntry{}
	if totalSize >= 16 {
		parsed, err := ParseIndexEntries(r.Read(0x20, totalSize-16))
		if err != nil {
			return IndexRoot{}, fmt.Errorf("errors parsing index entries: %v", err)
		}
		entries = parsed
	}

	return IndexRoot{
		AttributeType:     attributeType,
		CollationType:     CollationType(r.Uint32(0x04)),
		BytesPerRecord:    r.Uint32(0x08),
		ClustersPerRecord: r.Uint32(0x0C),
		Flags:             r.Uint32(0x1C),
		Entries:           entries,
	}, nil
}

func ParseIndexBlock(b []byte) (IndexBlock, error) {
	if len(b) < 36 {
		return IndexBlock{}, fmt.Errorf("expected at least %d bytes but got %d", 36, len(b))
	}

	r := NewLittleEndianReader(b)
	signature := string(r.Read(0x00, 0x04))
	sequenceNumberOffset := r.Uint16(0x04)
	sequenceNumberSize := r.Uint16(0x06)
	updateSequenceNumber := r.Uint16(int(sequenceNumberOffset))
	lsn := r.Uint64(0x08)

	entryOffset := r.Uint32(0x18)
	totalEntrySize := r.Uint32(0x1C)
	allocEntrySize := r.Uint32(0x20)
	notLeaf := r.Read(0x24, 1)[0]

	return IndexBlock{Signature: signature,
		UpdateSequenceOffset: sequenceNumberOffset,
		UpdateSequenceSize:   sequenceNumberSize,
		UpdateSequenceNumber: updateSequenceNumber,
		LSN:                  lsn, // $LogFile Sequence Number
		EntryOffset:          entryOffset,
		TotalEntrySize:       totalEntrySize,
		AllocEntrySize:       allocEntrySize,
		NotLeaf:              notLeaf}, nil
}

func ParseIndexEntries(b []byte) ([]IndexEntry, error) {
	if len(b) < 13 {
		return []IndexEntry{}, fmt.Errorf("expected at least %d bytes but got %d", 13, len(b))
	}
	entries := make([]IndexEntry, 0)
	for len(b) > 0 {
		r := NewLittleEndianReader(b)
		entryLength := int(r.Uint16(0x08))

		if len(b) < entryLength {
			return entries, fmt.Errorf("index entry length indicates %d bytes but got %d", entryLength, len(b))
		}

		flags := r.Uint32(0x0C)
		pointsToSubNode := flags&0b1 != 0
		isLastEntryInNode := flags&0b10 != 0
		contentLength := int(r.Uint16(0x0A))

		fileName := FileName{}
		if contentLength != 0 && !isLastEntryInNode {
			parsedFileName, err := ParseFileName(r.Read(0x10, contentLength))
			if err != nil {
				return entries, fmt.Errorf("errors parsing $FILE_NAME record in index entry: %v", err)
			}
			fileName = parsedFileName
		}
		subNodeVcn := uint64(0)
		if pointsToSubNode {
			subNodeVcn = r.Uint64(entryLength - 8)
		}

		fileReference, err := ParseFileReference(r.Read(0x00, 8))
		if err != nil {
			return entries, fmt.Errorf("unable to file reference: %v", err)
		}
		entry := IndexEntry{
			FileReference: fileReference,
			Flags:         flags,
			FileName:      fileName,
			SubNodeVCN:    subNodeVcn,
		}
		entries = append(entries, entry)
		b = r.ReadFrom(entryLength)
		if isLastEntryInNode {
			break
		}
	}
	return entries, nil
}

// ConvertFileTime
//
//	@Description: 将NTFS中文件时间,转换为time.Time
//	              NTFS文件时间是一个64位值, 表示自 1601 年 1 月 1 日协调世界时 (UTC) 中午 12:00 起经过的 100 纳秒间隔数
//	              见 https://docs.microsoft.com/en-us/windows/win32/sysinfo/file-times
//	@param timeValue
//	@return time.Time
func ConvertFileTime(timeValue uint64) time.Time {
	dur := time.Duration(int64(timeValue))
	r := time.Date(1601, time.January, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 100; i++ {
		r = r.Add(dur)
	}
	return r
}
