package table

const (
	GPTPartitionEntryCount = 128
	GPTDefaultLBASize      = 512
)

const GPTSignature = "EFI PART"

type GPTPartitionType = string

// http://en.wikipedia.org/wiki/GUID_Partition_Table#Partition_type_GUIDs
const (
	BlankEmptyPart               GPTPartitionType = "00000000-0000-0000-0000-000000000000"
	MBRPartitionSchema           GPTPartitionType = "024DEE41-33E7-11D3-9D69-0008C781F39F"
	GEFISystemPartition          GPTPartitionType = "C12A7328-F81F-11D2-BA4B-00A0C93EC93B"
	BIOSBootPartition            GPTPartitionType = "21686148-6449-6E6F-744E-656564454649"
	IFFSPartition                GPTPartitionType = "D3BFE2DE-3DAF-11DF-BA40-E3A556D89593"
	SonyBootPartition            GPTPartitionType = "F4019732-066E-4E12-8273-346C5641494F"
	LenovoBootPartition          GPTPartitionType = "BFBFAFE7-A34F-448A-9A5B-6213EB736C22"
	MicroMSR                     GPTPartitionType = "E3C9E316-0B5C-4DB8-817D-F92DF00215AE"
	BasicDataPartition           GPTPartitionType = "EBD0A0A2-B9E5-4433-87C0-68B6B72699C7"
	LDMMetaDataPartition         GPTPartitionType = "5808C8AA-7E8F-42E0-85D2-E1E90434CFB3"
	LDMDataPartition             GPTPartitionType = "AF9B60A0-1431-4F62-BC68-3311714A69AD"
	MicroMRE                     GPTPartitionType = "DE94BBA4-06D1-4D40-A16A-BFD50179D6AC"
	IBMGPFSPartition             GPTPartitionType = "37AFFC90-EF7D-4E96-91C3-2D7AE055B174"
	DataPartition                GPTPartitionType = "75894C1E-3AEB-11D3-B7C1-7B03A0000000"
	ServicePartition             GPTPartitionType = "E2A1E728-32E3-11D6-A682-7B03A0000000"
	LinuxFSData                  GPTPartitionType = "0FC63DAF-8483-4772-8E79-3D69D8477DE4"
	RAIDPartition                GPTPartitionType = "A19D880F-05FC-4D3B-A006-743F0F84911E"
	SwapPartition                GPTPartitionType = "0657FD6D-A4AB-43C4-84E5-0933C84B4F4F"
	LVMPartition                 GPTPartitionType = "E6D6D379-F507-44C2-A23C-238F2A3DF928"
	HomePartition                GPTPartitionType = "933AC7E1-2EB4-4F13-B844-0E14E2AEF915"
	SrvPartition                 GPTPartitionType = "3B8F8425-20E0-4F3B-907F-1A25A76F98E8"
	PlainDmCryptPartition        GPTPartitionType = "7FFEC5C9-2D00-49B7-8941-3EA10A5586B7"
	LUKSPartition                GPTPartitionType = "CA7D7CCB-63ED-4C53-861C-1742536059CC"
	Reserved                     GPTPartitionType = "8DA63339-0007-60C0-C436-083AC8230908"
	BootPartition                GPTPartitionType = "83BD6B9D-7F41-11DC-BE0B-001560B84F0F"
	GDataPartition               GPTPartitionType = "516E7CB4-6ECF-11D6-8FF8-00022D09712B"
	SwapPartition2               GPTPartitionType = "516E7CB5-6ECF-11D6-8FF8-00022D09712B"
	UFSPartition                 GPTPartitionType = "516E7CB6-6ECF-11D6-8FF8-00022D09712B"
	VinumVolumeManagerPartition  GPTPartitionType = "516E7CB8-6ECF-11D6-8FF8-00022D09712B"
	ZFSPartition                 GPTPartitionType = "516E7CBA-6ECF-11D6-8FF8-00022D09712B"
	HFSPlusPartition             GPTPartitionType = "48465300-0000-11AA-AA11-00306543ECAC"
	AppleUFS                     GPTPartitionType = "55465300-0000-11AA-AA11-00306543ECAC"
	ZFS                          GPTPartitionType = "6A898CC3-1DD2-11B2-99A6-080020736631"
	AppleRAIDPartition           GPTPartitionType = "52414944-0000-11AA-AA11-00306543ECAC"
	AppleRAIDOfflinePartition    GPTPartitionType = "52414944-5F4F-11AA-AA11-00306543ECAC"
	AppleBootPartition           GPTPartitionType = "426F6F74-0000-11AA-AA11-00306543ECAC"
	AppleLabel                   GPTPartitionType = "4C616265-6C00-11AA-AA11-00306543ECAC"
	AppleTVRecoveryPartition     GPTPartitionType = "5265636F-7665-11AA-AA11-00306543ECAC"
	AppleCoreStoragePartition    GPTPartitionType = "53746F72-6167-11AA-AA11-00306543ECAC"
	BootPartition2               GPTPartitionType = "6A82CB45-1DD2-11B2-99A6-080020736631"
	RootPartition                GPTPartitionType = "6A85CF4D-1DD2-11B2-99A6-080020736631"
	SwapPartition3               GPTPartitionType = "6A87C46F-1DD2-11B2-99A6-080020736631"
	BackupPartition              GPTPartitionType = "6A8B642B-1DD2-11B2-99A6-080020736631"
	VarPartition                 GPTPartitionType = "6A8EF2E9-1DD2-11B2-99A6-080020736631"
	HomePartition2               GPTPartitionType = "6A90BA39-1DD2-11B2-99A6-080020736631"
	AlternatePartition           GPTPartitionType = "6A9283A5-1DD2-11B2-99A6-080020736631"
	ReservedPartition            GPTPartitionType = "6A945A3B-1DD2-11B2-99A6-080020736631"
	ReservedPartition2           GPTPartitionType = "6A9630D1-1DD2-11B2-99A6-080020736631"
	ReservedPartition3           GPTPartitionType = "6A980767-1DD2-11B2-99A6-080020736631"
	ReservedPartition4           GPTPartitionType = "6A96237F-1DD2-11B2-99A6-080020736631"
	ReservedPartition5           GPTPartitionType = "6A8D2AC7-1DD2-11B2-99A6-080020736631"
	SwapPartition4               GPTPartitionType = "49F48D32-B10E-11DC-B99B-0019D1879648"
	FFSPartition                 GPTPartitionType = "49F48D5A-B10E-11DC-B99B-0019D1879648"
	LFSPartition                 GPTPartitionType = "49F48D82-B10E-11DC-B99B-0019D1879648"
	RAIDPartition2               GPTPartitionType = "49F48DAA-B10E-11DC-B99B-0019D1879648"
	ConcatenatedPartition        GPTPartitionType = "2DB519C4-B10F-11DC-B99B-0019D1879648"
	EncryptedPartition           GPTPartitionType = "2DB519EC-B10F-11DC-B99B-0019D1879648"
	ChromeOSKernel               GPTPartitionType = "FE3A2A5D-4F32-41A7-B725-ACCC3285A309"
	ChromeOSRootFS               GPTPartitionType = "3CB8E202-3B7E-47DD-8A3C-7FF2A13CFCEC"
	ChromeOSFutureUse            GPTPartitionType = "2E0A753D-9E48-43B0-8337-B15192CB1B5E"
	HaikuBFS                     GPTPartitionType = "42465331-3BA3-10F1-802A-4861696B7521"
	BootPartition3               GPTPartitionType = "85D5E45E-237C-11E1-B4B3-E89A8F7FC3A7"
	DataPartition2               GPTPartitionType = "85D5E45A-237C-11E1-B4B3-E89A8F7FC3A7"
	SwapPartition5               GPTPartitionType = "85D5E45B-237C-11E1-B4B3-E89A8F7FC3A7"
	UFSPartition2                GPTPartitionType = "0394EF8B-237E-11E1-B4B3-E89A8F7FC3A7"
	VinumVolumeManagerPartition2 GPTPartitionType = "85D5E45C-237C-11E1-B4B3-E89A8F7FC3A7"
	ZFSPartition2                GPTPartitionType = "85D5E45D-237C-11E1-B4B3-E89A8F7FC3A7"
	CephDmCryptJournal           GPTPartitionType = "45B0969E-9B03-4F30-B4C6-5EC00CEFF106"
	CephOSD                      GPTPartitionType = "4FBD7E29-9D25-41B8-AFD0-062C0CEFF05D"
	CephDmCryptOSD               GPTPartitionType = "4FBD7E29-9D25-41B8-AFD0-5EC00CEFF05D"
	CephDiskInCreation           GPTPartitionType = "89C57F98-2FE5-4DC0-89C1-F3AD0CEFF2BE"
	CephDmCryptDiskInCreation    GPTPartitionType = "89C57F98-2FE5-4DC0-89C1-5EC00CEFF2BE"
)

var GPTPartitionTypeDesc = map[GPTPartitionType]string{
	BlankEmptyPart:               "Blank Or Empty",
	MBRPartitionSchema:           "MBR PartitionIndex Scheme",
	GEFISystemPartition:          "EFI System PartitionIndex",
	BIOSBootPartition:            "BIOS Boot PartitionIndex",
	IFFSPartition:                "Intel Fast Flash (iFFS) PartitionIndex (For Intel Rapid Start Technology)",
	SonyBootPartition:            "Sony Boot PartitionIndex",
	LenovoBootPartition:          "Lenovo Boot PartitionIndex", // or "Ceph Journal"
	MicroMSR:                     "Microsoft Reserved PartitionIndex (MSR)",
	BasicDataPartition:           "Basic Data PartitionIndex",
	LDMMetaDataPartition:         "Logical Disk Manager (LDM) Metadata PartitionIndex",
	LDMDataPartition:             "Logical Disk Manager Data PartitionIndex",
	MicroMRE:                     "Windows Recovery Environment",
	IBMGPFSPartition:             "IBM General Parallel File System (GPFS) PartitionIndex",
	DataPartition:                "Data PartitionIndex",
	ServicePartition:             "Service PartitionIndex",
	LinuxFSData:                  "Linux Filesystem Data",
	RAIDPartition:                "RAID PartitionIndex",
	SwapPartition:                "Swap PartitionIndex",
	LVMPartition:                 "Logical Volume Manager (LVM) PartitionIndex",
	HomePartition:                "/home PartitionIndex",
	SrvPartition:                 "/srv PartitionIndex",
	PlainDmCryptPartition:        "Plain Dm-crypt PartitionIndex",
	LUKSPartition:                "LUKS PartitionIndex",
	Reserved:                     "Reserved",
	BootPartition:                "Boot PartitionIndex",
	GDataPartition:               "Data PartitionIndex",
	SwapPartition2:               "Swap PartitionIndex 2",
	UFSPartition:                 "Unix File System (UFS) PartitionIndex",
	VinumVolumeManagerPartition:  "Vinum Volume Manager PartitionIndex",
	ZFSPartition:                 "ZFS PartitionIndex",
	HFSPlusPartition:             "Hierarchical File System Plus (HFS+) PartitionIndex",
	AppleUFS:                     "Apple UFS",
	ZFS:                          "ZFS", //  or "/usr partition"
	AppleRAIDPartition:           "Apple RAID PartitionIndex",
	AppleRAIDOfflinePartition:    "Apple RAID PartitionIndex, Offline",
	AppleBootPartition:           "Apple Boot PartitionIndex",
	AppleLabel:                   "Apple Label",
	AppleTVRecoveryPartition:     "Apple TV Recovery PartitionIndex",
	AppleCoreStoragePartition:    "Apple Core Storage (i.e. Lion FileVault) PartitionIndex",
	BootPartition2:               "Boot PartitionIndex 2",
	RootPartition:                "root PartitionIndex",
	SwapPartition3:               "Swap PartitionIndex 3",
	BackupPartition:              "Backup PartitionIndex",
	VarPartition:                 "/var PartitionIndex",
	HomePartition2:               "/home PartitionIndex",
	AlternatePartition:           "Alternate Sector",
	ReservedPartition:            "Reserved PartitionIndex 1",
	ReservedPartition2:           "Reserved PartitionIndex 2",
	ReservedPartition3:           "Reserved PartitionIndex 3",
	ReservedPartition4:           "Reserved PartitionIndex 4",
	ReservedPartition5:           "Reserved PartitionIndex 5",
	SwapPartition4:               "Swap PartitionIndex 4",
	FFSPartition:                 "FFS PartitionIndex",
	LFSPartition:                 "LFS PartitionIndex",
	RAIDPartition2:               "RAID PartitionIndex",
	ConcatenatedPartition:        "Concatenated PartitionIndex",
	EncryptedPartition:           "Encrypted PartitionIndex",
	ChromeOSKernel:               "ChromeOS Kernel",
	ChromeOSRootFS:               "ChromeOS Rootfs",
	ChromeOSFutureUse:            "ChromeOS Future Use",
	HaikuBFS:                     "Haiku BFS",
	BootPartition3:               "Boot PartitionIndex 3",
	DataPartition2:               "Data PartitionIndex",
	SwapPartition5:               "Swap PartitionIndex 5",
	UFSPartition2:                "Unix File System (UFS) PartitionIndex",
	VinumVolumeManagerPartition2: "Vinum Volume Manager PartitionIndex",
	ZFSPartition2:                "ZFS PartitionIndex",
	CephDmCryptJournal:           "Ceph Dm-crypt Encrypted Journal",
	CephOSD:                      "Ceph OSD",
	CephDmCryptOSD:               "Ceph Dm-crypt OSD",
	CephDiskInCreation:           "Ceph Disk In Creation",
	CephDmCryptDiskInCreation:    "Ceph Dm-crypt Disk In Creation",
}
