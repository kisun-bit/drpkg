package table

const (
	GPTPartitionEntryCount = 128
	GPTDefaultLBASize      = 512
)

const GPTSignature = "EFI PART"

type GPTPartitionType = string

// http://en.wikipedia.org/wiki/GUID_Partition_Table#Partition_type_GUIDs
// https://github.com/corpnewt/MountEFI/blob/a5856d0dd9198e639c40fdafaedbb010fa21675e/Scripts/disk.py#L13
const (
	// -
	BlankEmptyPart      GPTPartitionType = "00000000-0000-0000-0000-000000000000"
	MBRPartitionSchema  GPTPartitionType = "024DEE41-33E7-11D3-9D69-0008C781F39F"
	GEFISystemPartition GPTPartitionType = "C12A7328-F81F-11D2-BA4B-00A0C93EC93B"
	BIOSBootPartition   GPTPartitionType = "21686148-6449-6E6F-744E-656564454649"
	IFFSPartition       GPTPartitionType = "D3BFE2DE-3DAF-11DF-BA40-E3A556D89593"
	SonyBootPartition   GPTPartitionType = "F4019732-066E-4E12-8273-346C5641494F"
	LenovoBootPartition GPTPartitionType = "BFBFAFE7-A34F-448A-9A5B-6213EB736C22"
	// Windows
	MicroMSR             GPTPartitionType = "E3C9E316-0B5C-4DB8-817D-F92DF00215AE"
	BasicDataPartition   GPTPartitionType = "EBD0A0A2-B9E5-4433-87C0-68B6B72699C7"
	LDMMetaDataPartition GPTPartitionType = "5808C8AA-7E8F-42E0-85D2-E1E90434CFB3"
	LDMDataPartition     GPTPartitionType = "AF9B60A0-1431-4F62-BC68-3311714A69AD"
	MicroMRE             GPTPartitionType = "DE94BBA4-06D1-4D40-A16A-BFD50179D6AC"
	IBMGPFSPartition     GPTPartitionType = "37AFFC90-EF7D-4E96-91C3-2D7AE055B174"
	StorageSpaces        GPTPartitionType = "E75CAF8F-F680-4CEE-AFA3-B001E56EFC2D"
	StorageReplica       GPTPartitionType = "558D43C5-A1AC-43C0-AAC8-D1472B2923D1"
	// HP-UX
	DataPartition    GPTPartitionType = "75894C1E-3AEB-11D3-B7C1-7B03A0000000"
	ServicePartition GPTPartitionType = "E2A1E728-32E3-11D6-A682-7B03A0000000"
	// Linux
	LinuxFSData           GPTPartitionType = "0FC63DAF-8483-4772-8E79-3D69D8477DE4"
	RAIDPartition         GPTPartitionType = "A19D880F-05FC-4D3B-A006-743F0F84911E"
	RootX86               GPTPartitionType = "44479540-F297-41B2-9AF7-D131D5F0458A"
	RootX8664             GPTPartitionType = "4F68BCE3-E8CD-4DB1-96E7-FBCAF984B709"
	Root32bitArm          GPTPartitionType = "69DAD710-2CE4-4E3C-B16C-21A1D49ABED3"
	Root64BitArm          GPTPartitionType = "B921B045-1DF0-41C3-AF44-4C6F280D3FAE"
	BootDir               GPTPartitionType = "BC13C2FF-59E6-4262-A352-B275FD6F7172"
	SwapPartition         GPTPartitionType = "0657FD6D-A4AB-43C4-84E5-0933C84B4F4F"
	LVMPartition          GPTPartitionType = "E6D6D379-F507-44C2-A23C-238F2A3DF928"
	HomePartition         GPTPartitionType = "933AC7E1-2EB4-4F13-B844-0E14E2AEF915"
	SrvPartition          GPTPartitionType = "3B8F8425-20E0-4F3B-907F-1A25A76F98E8"
	PlainDmCryptPartition GPTPartitionType = "7FFEC5C9-2D00-49B7-8941-3EA10A5586B7"
	LUKSPartition         GPTPartitionType = "CA7D7CCB-63ED-4C53-861C-1742536059CC"
	Reserved              GPTPartitionType = "8DA63339-0007-60C0-C436-083AC8230908"
	// FreeBSD
	BootPartition               GPTPartitionType = "83BD6B9D-7F41-11DC-BE0B-001560B84F0F"
	GDataPartition              GPTPartitionType = "516E7CB4-6ECF-11D6-8FF8-00022D09712B"
	SwapPartition2              GPTPartitionType = "516E7CB5-6ECF-11D6-8FF8-00022D09712B"
	UFSPartition                GPTPartitionType = "516E7CB6-6ECF-11D6-8FF8-00022D09712B"
	VinumVolumeManagerPartition GPTPartitionType = "516E7CB8-6ECF-11D6-8FF8-00022D09712B"
	ZFSPartition                GPTPartitionType = "516E7CBA-6ECF-11D6-8FF8-00022D09712B"
	Nandfs                      GPTPartitionType = "74BA7DD9-A689-11E1-BD04-00E081286ACF"
	// MacOS Darwin
	HFSPlusPartition          GPTPartitionType = "48465300-0000-11AA-AA11-00306543ECAC"
	APFSPartition             GPTPartitionType = "7C3457EF-0000-11AA-AA11-00306543ECAC"
	AppleUFS                  GPTPartitionType = "55465300-0000-11AA-AA11-00306543ECAC"
	ZFS                       GPTPartitionType = "6A898CC3-1DD2-11B2-99A6-080020736631"
	AppleRAIDPartition        GPTPartitionType = "52414944-0000-11AA-AA11-00306543ECAC"
	AppleRAIDOfflinePartition GPTPartitionType = "52414944-5F4F-11AA-AA11-00306543ECAC"
	AppleBootPartition        GPTPartitionType = "426F6F74-0000-11AA-AA11-00306543ECAC"
	AppleLabel                GPTPartitionType = "4C616265-6C00-11AA-AA11-00306543ECAC"
	AppleTVRecoveryPartition  GPTPartitionType = "5265636F-7665-11AA-AA11-00306543ECAC"
	AppleCoreStoragePartition GPTPartitionType = "53746F72-6167-11AA-AA11-00306543ECAC"
	AppleAPFSPreBoot          GPTPartitionType = "69646961-6700-11AA-AA11-00306543ECAC"
	AppleAPFSRecovery         GPTPartitionType = "52637672-7900-11AA-AA11-00306543ECAC"
	// Solaris illumos
	BootPartition2     GPTPartitionType = "6A82CB45-1DD2-11B2-99A6-080020736631"
	RootPartition      GPTPartitionType = "6A85CF4D-1DD2-11B2-99A6-080020736631"
	SwapPartition3     GPTPartitionType = "6A87C46F-1DD2-11B2-99A6-080020736631"
	BackupPartition    GPTPartitionType = "6A8B642B-1DD2-11B2-99A6-080020736631"
	VarPartition       GPTPartitionType = "6A8EF2E9-1DD2-11B2-99A6-080020736631"
	HomePartition2     GPTPartitionType = "6A90BA39-1DD2-11B2-99A6-080020736631"
	AlternatePartition GPTPartitionType = "6A9283A5-1DD2-11B2-99A6-080020736631"
	ReservedPartition  GPTPartitionType = "6A945A3B-1DD2-11B2-99A6-080020736631"
	ReservedPartition2 GPTPartitionType = "6A9630D1-1DD2-11B2-99A6-080020736631"
	ReservedPartition3 GPTPartitionType = "6A980767-1DD2-11B2-99A6-080020736631"
	ReservedPartition4 GPTPartitionType = "6A96237F-1DD2-11B2-99A6-080020736631"
	ReservedPartition5 GPTPartitionType = "6A8D2AC7-1DD2-11B2-99A6-080020736631"
	// NetBSD
	SwapPartition4        GPTPartitionType = "49F48D32-B10E-11DC-B99B-0019D1879648"
	FFSPartition          GPTPartitionType = "49F48D5A-B10E-11DC-B99B-0019D1879648"
	LFSPartition          GPTPartitionType = "49F48D82-B10E-11DC-B99B-0019D1879648"
	RAIDPartition2        GPTPartitionType = "49F48DAA-B10E-11DC-B99B-0019D1879648"
	ConcatenatedPartition GPTPartitionType = "2DB519C4-B10F-11DC-B99B-0019D1879648"
	EncryptedPartition    GPTPartitionType = "2DB519EC-B10F-11DC-B99B-0019D1879648"
	// ChromeOS
	ChromeOSKernel    GPTPartitionType = "FE3A2A5D-4F32-41A7-B725-ACCC3285A309"
	ChromeOSRootFS    GPTPartitionType = "3CB8E202-3B7E-47DD-8A3C-7FF2A13CFCEC"
	ChromeOSFirmware  GPTPartitionType = "CAB6E88E-ABF3-4102-A07A-D4BB9BE3C1D3"
	ChromeOSFutureUse GPTPartitionType = "2E0A753D-9E48-43B0-8337-B15192CB1B5E"
	ChromeOSMiniOS    GPTPartitionType = "09845860-705F-4BB5-B16C-8A8A099CAF52"
	ChromeOSHibernate GPTPartitionType = "3F0F8318-F146-4E6B-8222-C28C8F02E0D5"
	// Container Linux by CoreOS
	CoreOSUsr      GPTPartitionType = "5DFBF5F4-2848-4BAC-AA5E-0D9A20B745A6"
	CoreOSRootFS   GPTPartitionType = "3884DD41-8582-4404-B9A8-E9B84F2DF50E"
	CoreOSOEM      GPTPartitionType = "C95DC21A-DF0E-4340-8D7B-26CBFA9A03E0"
	CoreOSRaidRoot GPTPartitionType = "BE9067B9-EA49-4F15-B4F6-F36F8C9E1818"
	// Haiku
	HaikuBFS GPTPartitionType = "42465331-3BA3-10F1-802A-4861696B7521"
	// MidnightBSD
	BootPartition3               GPTPartitionType = "85D5E45E-237C-11E1-B4B3-E89A8F7FC3A7"
	DataPartition2               GPTPartitionType = "85D5E45A-237C-11E1-B4B3-E89A8F7FC3A7"
	SwapPartition5               GPTPartitionType = "85D5E45B-237C-11E1-B4B3-E89A8F7FC3A7"
	UFSPartition2                GPTPartitionType = "0394EF8B-237E-11E1-B4B3-E89A8F7FC3A7"
	VinumVolumeManagerPartition2 GPTPartitionType = "85D5E45C-237C-11E1-B4B3-E89A8F7FC3A7"
	ZFSPartition2                GPTPartitionType = "85D5E45D-237C-11E1-B4B3-E89A8F7FC3A7"
	// Ceph
	CephJournal                        GPTPartitionType = "45B0969E-9B03-4F30-B4C6-B4B80CEFF106"
	CephDmCryptJournal                 GPTPartitionType = "45B0969E-9B03-4F30-B4C6-5EC00CEFF106"
	CephOSD                            GPTPartitionType = "4FBD7E29-9D25-41B8-AFD0-062C0CEFF05D"
	CephDmCryptOSD                     GPTPartitionType = "4FBD7E29-9D25-41B8-AFD0-5EC00CEFF05D"
	CephDiskInCreation                 GPTPartitionType = "89C57F98-2FE5-4DC0-89C1-F3AD0CEFF2BE"
	CephDmCryptDiskInCreation          GPTPartitionType = "89C57F98-2FE5-4DC0-89C1-5EC00CEFF2BE"
	CephBlock                          GPTPartitionType = "CAFECAFE-9B03-4F30-B4C6-B4B80CEFF106"
	CephBlockDB                        GPTPartitionType = "30CD0809-C2B2-499C-8879-2D6B78529876"
	CephBlockWriteAheadLog             GPTPartitionType = "5CE17FCE-4087-4169-B7FF-056CC58473F9"
	CephLockBoxForDmCryptKeys          GPTPartitionType = "FB3AABF9-D25F-47CC-BF5E-721D1816496B"
	CephMultipathOSD                   GPTPartitionType = "4FBD7E29-8AE0-4982-BF9D-5A8D867AF560"
	CephMultipathJournal               GPTPartitionType = "45B0969E-8AE0-4982-BF9D-5A8D867AF560"
	CephMultipathBlock1                GPTPartitionType = "CAFECAFE-8AE0-4982-BF9D-5A8D867AF560"
	CephMultipathBlock2                GPTPartitionType = "7F4A666A-16F3-47A2-8445-152EF4D03F6C"
	CephMultipathBlockDB               GPTPartitionType = "EC6D6385-E346-45DC-BE91-DA2A7C8B3261"
	CephMultipathBlockWriteAheadLog    GPTPartitionType = "01B41E1B-002A-453C-9F17-88793989FF8F"
	CephDmCryptBlock                   GPTPartitionType = "CAFECAFE-9B03-4F30-B4C6-5EC00CEFF106"
	CephDmCryptBlockDB                 GPTPartitionType = "93B0052D-02D9-4D8A-A43B-33A3EE4DFBC3"
	CephDmCryptBlockWriteAheadLog      GPTPartitionType = "306E8683-4FE2-4330-B7C0-00A917C16966"
	CephDmCryptLUNKsJournal            GPTPartitionType = "45B0969E-9B03-4F30-B4C6-35865CEFF106"
	CephDmCryptLUNKsBlock              GPTPartitionType = "CAFECAFE-9B03-4F30-B4C6-35865CEFF106"
	CephDmCryptLUNKsBlockDB            GPTPartitionType = "166418DA-C469-4022-ADF4-B30AFD37F176"
	CephDmCryptLUNKsBlockWriteAheadLog GPTPartitionType = "86A32090-3647-40B9-BBBD-38D8C573AA86"
	CephDmCryptLUNKsOSD                GPTPartitionType = "4FBD7E29-9D25-41B8-AFD0-35865CEFF05D"
	// OpenBSD
	OpenBSDData GPTPartitionType = "824CC7A0-36A8-11E3-890A-952519AD3F61"
	// QNX
	QNXPowerSafeFs GPTPartitionType = "CEF5A9AD-73BC-4601-89F3-CDEEEEE321A1"
	// Plan 9
	GPTPlan9 GPTPartitionType = "C91818F9-8025-47AF-89D2-F030D7000C2C"
	// Vmware ESX
	VmwareVMFS      GPTPartitionType = "AA31E02A-400F-11DB-9590-000C2911D1B8"
	VmwareDiagostic GPTPartitionType = "9D275380-40AD-11DB-BF97-000C2911D1B8"
	VmwareReversed  GPTPartitionType = "9198EFFC-31C0-11DB-8F78-000C2911D1B8"
	VmwareVSAN      GPTPartitionType = "381cfccc-7288-11e0-92ee-000c2911d0b2"
	// TODO more...
)

var GPTPartitionTypeDesc = map[GPTPartitionType]string{
	BlankEmptyPart:                     "Unused",
	MBRPartitionSchema:                 "MBR",
	GEFISystemPartition:                "EFI System",
	BIOSBootPartition:                  "BIOS Boot",
	IFFSPartition:                      "Intel Fast Flash",
	SonyBootPartition:                  "Sony Boot",
	LenovoBootPartition:                "Lenovo Boot",
	MicroMSR:                           "Microsoft Reserved",
	BasicDataPartition:                 "Microsoft basic data",
	LDMMetaDataPartition:               "Logical Disk Manager metadata",
	LDMDataPartition:                   "Logical Disk Manager Data",
	MicroMRE:                           "Windows Recovery",
	IBMGPFSPartition:                   "IBM General Parallel File System",
	StorageSpaces:                      "Storage Spaces",
	StorageReplica:                     "Storage Replica",
	DataPartition:                      "Data",
	ServicePartition:                   "Service",
	LinuxFSData:                        "Linux Filesystem Data",
	RAIDPartition:                      "RAID",
	RootX86:                            "Root (x86)",
	RootX8664:                          "Root (x86-64)",
	Root32bitArm:                       "Root (32-bit ARM)",
	Root64BitArm:                       "Root (64-bit ARM/AArch64)",
	BootDir:                            "/boot",
	SwapPartition:                      "Swap",
	LVMPartition:                       "Logical Volume Manager",
	HomePartition:                      "/home",
	SrvPartition:                       "/srv",
	PlainDmCryptPartition:              "Plain dm-crypt",
	LUKSPartition:                      "LUKS",
	Reserved:                           "Reserved",
	BootPartition:                      "Boot PartitionIndex",
	GDataPartition:                     "Data PartitionIndex",
	SwapPartition2:                     "Swap PartitionIndex 2",
	UFSPartition:                       "Unix File System (UFS)",
	VinumVolumeManagerPartition:        "Vinum Volume Manager",
	ZFSPartition:                       "ZFS PartitionIndex",
	Nandfs:                             "nandfs",
	HFSPlusPartition:                   "Hierarchical File System Plus (HFS+)",
	APFSPartition:                      "Apple APFS container",
	AppleUFS:                           "Apple UFS",
	ZFS:                                "ZFS",
	AppleRAIDPartition:                 "Apple RAID",
	AppleRAIDOfflinePartition:          "Apple RAID, Offline",
	AppleBootPartition:                 "Apple Boot",
	AppleLabel:                         "Apple Label",
	AppleTVRecoveryPartition:           "Apple TV Recovery",
	AppleCoreStoragePartition:          "Apple Core Storage (i.e. Lion FileVault)",
	AppleAPFSPreBoot:                   "Apple APFS Preboot",
	AppleAPFSRecovery:                  "Apple APFS Recovery",
	BootPartition2:                     "Boot",
	RootPartition:                      "root",
	SwapPartition3:                     "Swap",
	BackupPartition:                    "Backup",
	VarPartition:                       "/var",
	HomePartition2:                     "/home",
	AlternatePartition:                 "Alternate Sector",
	ReservedPartition:                  "Reserved",
	ReservedPartition2:                 "Reserved",
	ReservedPartition3:                 "Reserved",
	ReservedPartition4:                 "Reserved",
	ReservedPartition5:                 "Reserved",
	SwapPartition4:                     "Swap",
	FFSPartition:                       "FFS",
	LFSPartition:                       "LFS",
	RAIDPartition2:                     "RAID",
	ConcatenatedPartition:              "Concatenated",
	EncryptedPartition:                 "Encrypted",
	ChromeOSKernel:                     "ChromeOS Kernel",
	ChromeOSRootFS:                     "ChromeOS Rootfs",
	ChromeOSFirmware:                   "ChromeOS firmware",
	ChromeOSFutureUse:                  "ChromeOS Future Use",
	ChromeOSMiniOS:                     "ChromeOS MiniOS",
	ChromeOSHibernate:                  "ChromeOS hibernate",
	CoreOSUsr:                          "/usr",
	CoreOSRootFS:                       "Resizable rootfs",
	CoreOSOEM:                          "OEM customizations",
	CoreOSRaidRoot:                     "Root filesystem on RAID",
	HaikuBFS:                           "Haiku BFS",
	BootPartition3:                     "Boot",
	DataPartition2:                     "Data",
	SwapPartition5:                     "Swap",
	UFSPartition2:                      "Unix File System (UFS)",
	VinumVolumeManagerPartition2:       "Vinum Volume Manager",
	ZFSPartition2:                      "ZFS PartitionIndex",
	CephJournal:                        "Journal",
	CephDmCryptJournal:                 "Ceph Dm-crypt Encrypted Journal",
	CephOSD:                            "Ceph OSD",
	CephDmCryptOSD:                     "Ceph Dm-crypt OSD",
	CephDiskInCreation:                 "Ceph Disk In Creation",
	CephDmCryptDiskInCreation:          "Ceph Dm-crypt Disk In Creation",
	CephBlock:                          "Block",
	CephBlockDB:                        "Block DB",
	CephBlockWriteAheadLog:             "Block write-ahead log",
	CephLockBoxForDmCryptKeys:          "Lockbox for dm-crypt keys",
	CephMultipathOSD:                   "Multipath OSD",
	CephMultipathJournal:               "Multipath journal",
	CephMultipathBlock1:                "Multipath block",
	CephMultipathBlock2:                "Multipath block",
	CephMultipathBlockDB:               "Multipath block DB",
	CephMultipathBlockWriteAheadLog:    "Multipath block write-ahead log",
	CephDmCryptBlock:                   "dm-crypt bloc",
	CephDmCryptBlockDB:                 "dm-crypt block DB",
	CephDmCryptBlockWriteAheadLog:      "dm-crypt block write-ahead log",
	CephDmCryptLUNKsJournal:            "dm-crypt LUKS journal",
	CephDmCryptLUNKsBlock:              "dm-crypt LUKS block",
	CephDmCryptLUNKsBlockDB:            "dm-crypt LUKS block DB",
	CephDmCryptLUNKsBlockWriteAheadLog: "dm-crypt LUKS block write-ahead log",
	CephDmCryptLUNKsOSD:                "dm-crypt LUKS OSD",
	OpenBSDData:                        "Data",
	QNXPowerSafeFs:                     "Power-safe (QNX6) file syste",
	GPTPlan9:                           "Plan 9",
	VmwareVMFS:                         "VMWare VMFS",
	VmwareDiagostic:                    "VMWare Diagostic",
	VmwareReversed:                     "VMWare Reserved",
	VmwareVSAN:                         "VMWare VSAN",
}
