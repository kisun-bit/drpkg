package types

type FsType string

const (
	FsTypeUnknown FsType = "unknown"
	FsTypeExt234  FsType = "ext2/3/4"
	FsTypeNtfs    FsType = "ntfs"
	FsTypeXfs     FsType = "xfs"
	FsTypeBtrfs   FsType = "btrfs"
	FsTypeExFat   FsType = "exfat"
	FsTypeApfs    FsType = "apfs"
	FsTypeFat     FsType = "fat"
)

func (f *FsType) String() string {
	return string(*f)
}
