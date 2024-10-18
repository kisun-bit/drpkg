package fossick

const (
	Unknown   Filesystem = "raw"
	NTFS      Filesystem = "ntfs"
	FAT       Filesystem = "fat"
	EXT       Filesystem = "ext2/3/4" // 没有较好地办法从superblock域中区分这三种文件系统, 参考:https://unix.stackexchange.com/questions/123009/reliable-way-to-detect-ext2-or-ext3-or-ext4
	XFS       Filesystem = "xfs"
	OracleASM Filesystem = "oracle-asm"
	BTRFS     Filesystem = "btrfs"
	ZFS       Filesystem = "zfs"
	JFS       Filesystem = "jfs"
	APFS      Filesystem = "apfs"
)

const (
	EXTMagic        = "\x53\xEF"
	FAT32Magic      = "\xEB\x3C\x90\x4D\x4B\x44\x4F\x53"
	NTFSMagic       = "\xEB\x52\x90\x4E\x54\x46\x53"
	XFSMagic        = "\x58\x46\x53\x42"
	BTRFSMagic      = "\x5F\xB7\xE1\x82"
	ZFSMagic        = "\x89\xc3\xd9\xd1\xf8\xa0\xe2\xe6"
	JFSMagic        = "\x01\xf5\xe1\xff"
	APFSMagic       = "\x45\xd2\xe1\xa9\xb7\xf6\xa8\xc6"
	OracleDiskMagic = "\x4f\x52\x43\x4c\x44\x49\x53\x4b"
)
