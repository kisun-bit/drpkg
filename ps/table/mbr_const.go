package table

const (
	MBRSignature510               = 0x55
	MBRSignature511               = 0xAA
	MBRLogicalPartitionEntryIndex = 0
	MBREBRPartitionEntryIndex     = 1
	MBRPartitionEntryCount        = 4
	MBRDefaultLBASize             = 1 << 9
	MBRPartitionBootable          = 0x80
)

// MBRPartitionType 表示MBR结构下的分区类型.
type MBRPartitionType = byte

const (
	Empty                                 MBRPartitionType = 0x00
	FAT12                                 MBRPartitionType = 0x01
	FAT16Range16MBTo32MB                  MBRPartitionType = 0x04
	ExtendCHS                             MBRPartitionType = 0x05
	FAT16Range32MBTo2GB                   MBRPartitionType = 0x06
	NTFS                                  MBRPartitionType = 0x07
	FAT32                                 MBRPartitionType = 0x0B
	FAT32X                                MBRPartitionType = 0x0C
	FAT16X                                MBRPartitionType = 0x0E
	ExtendLBA                             MBRPartitionType = 0x0F
	HiddenFAT12                           MBRPartitionType = 0x11
	HiddenFAT16Range16MBTo32MB            MBRPartitionType = 0x14
	HiddenExtendCHS                       MBRPartitionType = 0x15
	HiddenFAT16Range32MBTo2GB             MBRPartitionType = 0x16
	HiddenNTFS                            MBRPartitionType = 0x17
	HiddenFAT32                           MBRPartitionType = 0x1B
	HiddenFAT32X                          MBRPartitionType = 0x1C
	HiddenFAT16X                          MBRPartitionType = 0x1E
	HiddenExtendLBA                       MBRPartitionType = 0x1F
	WindowsRecoveryEnv                    MBRPartitionType = 0x27
	Plan9                                 MBRPartitionType = 0x39
	PartitionMagicRecoveryPartition       MBRPartitionType = 0x3C
	WindowsDynamicExtendedPartitionMarker MBRPartitionType = 0x42
	GoBackPartition                       MBRPartitionType = 0x44
	UnixSystemV                           MBRPartitionType = 0x63
	PCARMOURProtectedPartition            MBRPartitionType = 0x64
	Minix                                 MBRPartitionType = 0x81
	LinuxSwap                             MBRPartitionType = 0x82
	Linux                                 MBRPartitionType = 0x83
	Hibernation                           MBRPartitionType = 0x84
	LinuxExtend                           MBRPartitionType = 0x85
	FaultTolerantFAT16BVolSet             MBRPartitionType = 0x86
	FaultTolerantNTFSBVolSet              MBRPartitionType = 0x87
	LinuxPlaintext                        MBRPartitionType = 0x88
	LinuxLVM                              MBRPartitionType = 0x8E
	HiddenLinux                           MBRPartitionType = 0x93
	BSDOS                                 MBRPartitionType = 0x9F
	Hibernation01                         MBRPartitionType = 0xA0
	Hibernation02                         MBRPartitionType = 0xA1
	FreeBSD                               MBRPartitionType = 0xA5
	OpenBSD                               MBRPartitionType = 0xA6
	MacOSX                                MBRPartitionType = 0xA8
	NetBSD                                MBRPartitionType = 0xA9
	MacOSXBoot                            MBRPartitionType = 0xAB
	MacOSXHFS                             MBRPartitionType = 0xAF
	Solaris8BootPartition                 MBRPartitionType = 0xBE
	SolarisX86                            MBRPartitionType = 0xBF
	LinuxUnifiedKeySetup                  MBRPartitionType = 0xE8
	BFS                                   MBRPartitionType = 0xEB
	EFIGPTProtectiveMBR                   MBRPartitionType = 0xEE
	EFISystemPartition                    MBRPartitionType = 0xEF
	BochsX86Emulator                      MBRPartitionType = 0xFA
	VmwareFileSystem                      MBRPartitionType = 0xFB
	VmwareSwap                            MBRPartitionType = 0xFC
	LinuxRAID                             MBRPartitionType = 0xFD
)

// MBRPartitionTypeDesc MBR分区类型的描述字典.
var MBRPartitionTypeDesc = map[MBRPartitionType]string{
	Empty:                                 "Empty",
	FAT12:                                 "FAT12",
	FAT16Range16MBTo32MB:                  "FAT16 16-32MB",
	ExtendCHS:                             "Extended, CHS",
	FAT16Range32MBTo2GB:                   "FAT16 32MB-2GB",
	NTFS:                                  "NTFS",
	FAT32:                                 "FAT32",
	FAT32X:                                "FAT32X",
	FAT16X:                                "FAT16X",
	ExtendLBA:                             "Extended, LBA",
	HiddenFAT12:                           "Hidden FAT12",
	HiddenFAT16Range16MBTo32MB:            "Hidden FAT16,16-32MB",
	HiddenExtendCHS:                       "Hidden Extended, CHS",
	HiddenFAT16Range32MBTo2GB:             "Hidden FAT16,32MB-2GB",
	HiddenNTFS:                            "Hidden NTFS",
	HiddenFAT32:                           "Hidden FAT32",
	HiddenFAT32X:                          "Hidden FAT32X",
	HiddenFAT16X:                          "Hidden FAT16X",
	HiddenExtendLBA:                       "Hidden Extended, LBA",
	WindowsRecoveryEnv:                    "Windows recovery environment",
	Plan9:                                 "Plan 9",
	PartitionMagicRecoveryPartition:       "PartitionMagic recovery partition",
	WindowsDynamicExtendedPartitionMarker: "Windows dynamic extended partition marker",
	GoBackPartition:                       "GoBack partition",
	UnixSystemV:                           "Unix System V",
	PCARMOURProtectedPartition:            "PC-ARMOUR protected partition",
	Minix:                                 "Minix",
	LinuxSwap:                             "Linux Swap",
	Linux:                                 "Linux",
	Hibernation:                           "Hibernation",
	LinuxExtend:                           "Linux Extended",
	FaultTolerantFAT16BVolSet:             "Fault-tolerant FAT16B volume set",
	FaultTolerantNTFSBVolSet:              "Fault-tolerant NTFS volume set",
	LinuxPlaintext:                        "Linux plaintext",
	LinuxLVM:                              "Linux LVM",
	HiddenLinux:                           "Hidden Linux",
	BSDOS:                                 "BSD/OS",
	Hibernation01:                         "Hibernation01",
	Hibernation02:                         "Hibernation02",
	FreeBSD:                               "FreeBSD",
	OpenBSD:                               "OpenBSD",
	MacOSX:                                "Mac OS X",
	NetBSD:                                "NetBSD",
	MacOSXBoot:                            "Mac OS X Boot",
	MacOSXHFS:                             "Mac OS X HFS",
	Solaris8BootPartition:                 "Solaris 8 boot partition",
	SolarisX86:                            "Solaris x86",
	LinuxUnifiedKeySetup:                  "Linux Unified Key Setup",
	BFS:                                   "BFS",
	EFIGPTProtectiveMBR:                   "EFI GPT protective MBR",
	EFISystemPartition:                    "EFI system partition",
	BochsX86Emulator:                      "Bochs x86 emulator",
	VmwareFileSystem:                      "VMware File System",
	VmwareSwap:                            "VMware Swap",
	LinuxRAID:                             "Linux RAID",
}

// MBRExtendPartTypes MBR扩展分区类型标记集合.
var MBRExtendPartTypes = []MBRPartitionType{ExtendCHS, ExtendLBA, HiddenExtendCHS, HiddenExtendLBA, LinuxExtend}
