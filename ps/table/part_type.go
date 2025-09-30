package table

type PartType string

// MBR磁盘相关分区表项类型的常量定义
// 参考：https://thestarman.pcministry.com/asm/mbr/PartTypes.htm
const (
	MBR_EMPTY_PARTITION                PartType = "00"
	MBR_FAT12_PARTITION                PartType = "01"
	MBR_XENIX_ROOT_PARTITION           PartType = "02"
	MBR_XENIX_USR_PARTITION            PartType = "03"
	MBR_FAT16_LESS32M_PARTITION        PartType = "04"
	MBR_DOS_EXTENDED_PARTITION         PartType = "05"
	MBR_FAT16_PARTITION                PartType = "06" /* DOS 16-bit >=32M */
	MBR_HPFS_NTFS_PARTITION            PartType = "07" /* OS/2 IFS e.g. HPFS or NTFS or QNX or exFAT */
	MBR_AIX_PARTITION                  PartType = "08" /* AIX boot (AIX -- PS/2 port) or SplitDrive */
	MBR_AIX_BOOTABLE_PARTITION         PartType = "09" /* AIX data or Coherent */
	MBR_OS2_BOOTMNGR_PARTITION         PartType = "0a" /* OS/2 Boot Manager */
	MBR_W95_FAT32_PARTITION            PartType = "0b"
	MBR_W95_FAT32_LBA_PARTITION        PartType = "0c" /* LBA really is `Extended Int 13h' */
	MBR_W95_FAT16_LBA_PARTITION        PartType = "0e"
	MBR_W95_EXTENDED_PARTITION         PartType = "0f"
	MBR_OPUS_PARTITION                 PartType = "10"
	MBR_HIDDEN_FAT12_PARTITION         PartType = "11"
	MBR_COMPAQ_DIAGNOSTICS_PARTITION   PartType = "12"
	MBR_HIDDEN_FAT16_L32M_PARTITION    PartType = "14"
	MBR_HIDDEN_FAT16_PARTITION         PartType = "16"
	MBR_HIDDEN_HPFS_NTFS_PARTITION     PartType = "17"
	MBR_AST_SMARTSLEEP_PARTITION       PartType = "18"
	MBR_HIDDEN_W95_FAT32_PARTITION     PartType = "1b"
	MBR_HIDDEN_W95_FAT32LBA_PARTITION  PartType = "1c"
	MBR_HIDDEN_W95_FAT16LBA_PARTITION  PartType = "1e"
	MBR_NEC_DOS_PARTITION              PartType = "24"
	MBR_PLAN9_PARTITION                PartType = "39"
	MBR_PARTITIONMAGIC_PARTITION       PartType = "3c"
	MBR_VENIX80286_PARTITION           PartType = "40"
	MBR_PPC_PREP_BOOT_PARTITION        PartType = "41"
	MBR_SFS_PARTITION                  PartType = "42"
	MBR_QNX_4X_PARTITION               PartType = "4d"
	MBR_QNX_4X_2ND_PARTITION           PartType = "4e"
	MBR_QNX_4X_3RD_PARTITION           PartType = "4f"
	MBR_DM_PARTITION                   PartType = "50"
	MBR_DM6_AUX1_PARTITION             PartType = "51" /* (or Novell) */
	MBR_CPM_PARTITION                  PartType = "52" /* CP/M or Microport SysV/AT */
	MBR_DM6_AUX3_PARTITION             PartType = "53"
	MBR_DM6_PARTITION                  PartType = "54"
	MBR_EZ_DRIVE_PARTITION             PartType = "55"
	MBR_GOLDEN_BOW_PARTITION           PartType = "56"
	MBR_PRIAM_EDISK_PARTITION          PartType = "5c"
	MBR_SPEEDSTOR_PARTITION            PartType = "61"
	MBR_GNU_HURD_PARTITION             PartType = "63" /* GNU HURD or Mach or Sys V/386 (such as ISC UNIX) */
	MBR_UNIXWARE_PARTITION                      = MBR_GNU_HURD_PARTITION
	MBR_NETWARE_286_PARTITION          PartType = "64"
	MBR_NETWARE_386_PARTITION          PartType = "65"
	MBR_DISKSECURE_MULTIBOOT_PARTITION PartType = "70"
	MBR_PC_IX_PARTITION                PartType = "75"
	MBR_OLD_MINIX_PARTITION            PartType = "80" /* Minix 1.4a and earlier */
	MBR_MINIX_PARTITION                PartType = "81" /* Minix 1.4b and later */
	MBR_LINUX_SWAP_PARTITION           PartType = "82"
	MBR_SOLARIS_X86_PARTITION                   = MBR_LINUX_SWAP_PARTITION
	MBR_LINUX_DATA_PARTITION           PartType = "83"
	MBR_OS2_HIDDEN_DRIVE_PARTITION     PartType = "84" /* also hibernation MS APM Intel Rapid Start */
	MBR_INTEL_HIBERNATION_PARTITION             = MBR_OS2_HIDDEN_DRIVE_PARTITION
	MBR_LINUX_EXTENDED_PARTITION       PartType = "85"
	MBR_NTFS_VOL_SET1_PARTITION        PartType = "86"
	MBR_NTFS_VOL_SET2_PARTITION        PartType = "87"
	MBR_LINUX_PLAINTEXT_PARTITION      PartType = "88"
	MBR_LINUX_LVM_PARTITION            PartType = "8e"
	MBR_AMOEBA_PARTITION               PartType = "93"
	MBR_AMOEBA_BBT_PARTITION           PartType = "94" /* (bad block table) */
	MBR_BSD_OS_PARTITION               PartType = "9f" /* BSDI */
	MBR_THINKPAD_HIBERNATION_PARTITION PartType = "a0"
	MBR_FREEBSD_PARTITION              PartType = "a5" /* various BSD flavours */
	MBR_OPENBSD_PARTITION              PartType = "a6"
	MBR_NEXTSTEP_PARTITION             PartType = "a7"
	MBR_DARWIN_UFS_PARTITION           PartType = "a8"
	MBR_NETBSD_PARTITION               PartType = "a9"
	MBR_DARWIN_BOOT_PARTITION          PartType = "ab"
	MBR_HFS_HFS_PARTITION              PartType = "af"
	MBR_BSDI_FS_PARTITION              PartType = "b7"
	MBR_BSDI_SWAP_PARTITION            PartType = "b8"
	MBR_BOOTWIZARD_HIDDEN_PARTITION    PartType = "bb"
	MBR_ACRONIS_FAT32LBA_PARTITION     PartType = "bc" /* Acronis Secure Zone with ipl for loader F11.SYS */
	MBR_SOLARIS_BOOT_PARTITION         PartType = "be"
	MBR_SOLARIS_PARTITION              PartType = "bf"
	MBR_DRDOS_FAT12_PARTITION          PartType = "c1"
	MBR_DRDOS_FAT16_L32M_PARTITION     PartType = "c4"
	MBR_DRDOS_FAT16_PARTITION          PartType = "c6"
	MBR_SYRINX_PARTITION               PartType = "c7"
	MBR_NONFS_DATA_PARTITION           PartType = "da"
	MBR_CPM_CTOS_PARTITION             PartType = "db" /* CP/M or Concurrent CP/M or Concurrent DOS or CTOS */
	MBR_DELL_UTILITY_PARTITION         PartType = "de" /* Dell PowerEdge Server utilities */
	MBR_BOOTIT_PARTITION               PartType = "df" /* BootIt EMBRM */
	MBR_DOS_ACCESS_PARTITION           PartType = "e1" /* DOS access or SpeedStor 12-bit FAT extended partition */
	MBR_DOS_RO_PARTITION               PartType = "e3" /* DOS R/O or SpeedStor */
	MBR_SPEEDSTOR_EXTENDED_PARTITION   PartType = "e4" /* SpeedStor 16-bit FAT extended partition < 1024 cyl. */
	MBR_RUFUS_EXTRA_PARTITION          PartType = "ea" /* Rufus extra partition for alignment */
	MBR_BEOS_FS_PARTITION              PartType = "eb"
	MBR_GPT_PARTITION                  PartType = "ee" /* Intel EFI GUID Partition Table */
	MBR_EFI_SYSTEM_PARTITION           PartType = "ef" /* Intel EFI System Partition */
	MBR_LINUX_PARISC_BOOT_PARTITION    PartType = "f0" /* Linux/PA-RISC boot loader */
	MBR_SPEEDSTOR1_PARTITION           PartType = "f1"
	MBR_SPEEDSTOR2_PARTITION           PartType = "f4" /* SpeedStor large partition */
	MBR_DOS_SECONDARY_PARTITION        PartType = "f2" /* DOS 3.3+ secondary */
	MBR_EBBR_PROTECTIVE_PARTITION      PartType = "f8" /* Arm EBBR firmware protective partition */
	MBR_VMWARE_VMFS_PARTITION          PartType = "fb"
	MBR_VMWARE_VMKCORE_PARTITION       PartType = "fc" /* VMware kernel dump partition */
	MBR_LINUX_RAID_PARTITION           PartType = "fd" /* Linux raid partition with autodetect using persistent superblock */
	MBR_LANSTEP_PARTITION              PartType = "fe" /* SpeedStor >1024 cyl. or LANstep */
	MBR_XENIX_BBT_PARTITION            PartType = "ff" /* Xenix Bad Block Table */
)

// GPT磁盘相关分区表项类型的常量定义
// 参考自维基百科
const (
	// Generic / Unused / Reserved
	GPT_UNUSED_ENTRY     PartType = "00000000-0000-0000-0000-000000000000" // Unused entry
	GPT_MBR              PartType = "024DEE41-33E7-11D3-9D69-0008C781F39F" // MBR protective
	GPT_EFI              PartType = "C12A7328-F81F-11D2-BA4B-00A0C93EC93B" // EFI System Partition
	GPT_BIOS_BOOT        PartType = "21686148-6449-6E6F-744E-656564454649" // BIOS boot partition
	GPT_INTEL_FAST_FLASH PartType = "D3BFE2DE-3DAF-11DF-BA40-E3A556D89593" // Intel Fast Flash
	GPT_SONY_BOOT        PartType = "F4019732-066E-4E12-8273-346C5641494F" // Sony boot
	GPT_LENOVO_BOOT      PartType = "BFBFAFE7-A34F-448A-9A5B-6213EB736C22" // Lenovo boot

	// Windows / Microsoft
	GPT_MSFT_RESERVED        PartType = "E3C9E316-0B5C-4DB8-817D-F92DF00215AE" // Microsoft Reserved
	GPT_MSFT_BASIC_DATA      PartType = "EBD0A0A2-B9E5-4433-87C0-68B6B72699C7" // Microsoft basic data
	GPT_MSFT_LDM_METADATA    PartType = "5808C8AA-7E8F-42E0-85D2-E1E90434CFB3" // Logical Disk Manager metadata
	GPT_MSFT_LDM_DATA        PartType = "AF9B60A0-1431-4F62-BC68-3311714A69AD" // Logical Disk Manager data
	GPT_MSFT_RECOVERY        PartType = "DE94BBA4-06D1-4D40-A16A-BFD50179D6AC" // Windows Recovery
	GPT_MSFT_GPFS            PartType = "37AFFC90-EF7D-4E96-91C3-2D7AE055B174" // IBM GPFS
	GPT_MSFT_STORAGE_SPACES  PartType = "E75CAF8F-F680-4CEE-AFA3-B001E56EFC2D" // Storage Spaces
	GPT_MSFT_STORAGE_REPLICA PartType = "558D43C5-A1AC-43C0-AAC8-D1472B2923D1" // Storage Replica

	// HP-UX
	GPT_HPUX_DATA    PartType = "75894C1E-3AEB-11D3-B7C1-7B03A0000000" // HP-UX Data
	GPT_HPUX_SERVICE PartType = "E2A1E728-32E3-11D6-A682-7B03A0000000" // HP-UX Service

	// Linux / freedesktop etc.
	GPT_LINUX_FS_DATA  PartType = "0FC63DAF-8483-4772-8E79-3D69D8477DE4" // Linux filesystem data
	GPT_LINUX_RAID     PartType = "A19D880F-05FC-4D3B-A006-743F0F84911E" // RAID
	GPT_ROOT_X86       PartType = "44479540-F297-41B2-9AF7-D131D5F0458A" // Root (x86)
	GPT_ROOT_X86_64    PartType = "4F68BCE3-E8CD-4DB1-96E7-FBCAF984B709" // Root (x86-64)
	GPT_ROOT_ARM32     PartType = "69DAD710-2CE4-4E3C-B16C-21A1D49ABED3" // Root (32-bit ARM)
	GPT_ROOT_ARM64     PartType = "B921B045-1DF0-41C3-AF44-4C6F280D3FAE" // Root (64-bit ARM / AArch64)
	GPT_LINUX_BOOT     PartType = "BC13C2FF-59E6-4262-A352-B275FD6F7172" // /boot (shared boot loader)
	GPT_LINUX_SWAP     PartType = "0657FD6D-A4AB-43C4-84E5-0933C84B4F4F" // Swap
	GPT_LINUX_LVM      PartType = "E6D6D379-F507-44C2-A23C-238F2A3DF928" // Logical Volume Manager
	GPT_LINUX_HOME     PartType = "933AC7E1-2EB4-4F13-B844-0E14E2AEF915" // /home
	GPT_LINUX_SRV      PartType = "3B8F8425-20E0-4F3B-907F-1A25A76F98E8" // /srv
	GPT_LINUX_DM_CRYPT PartType = "7FFEC5C9-2D00-49B7-8941-3EA10A5586B7" // Plain dm-crypt
	GPT_LINUX_LUKS     PartType = "CA7D7CCB-63ED-4C53-861C-1742536059CC" // LUKS encrypted
	GPT_LINUX_RESERVED PartType = "8DA63339-0007-60C0-C436-083AC8230908" // Reserved

	// FreeBSD
	GPT_FREEBSD_BOOT      PartType = "83BD6B9D-7F41-11DC-BE0B-001560B84F0F" // Boot
	GPT_FREEBSD_DISKLABEL PartType = "516E7CB4-6ECF-11D6-8FF8-00022D09712B" // BSD disklabel
	GPT_FREEBSD_SWAP      PartType = "516E7CB5-6ECF-11D6-8FF8-00022D09712B" // Swap
	GPT_FREEBSD_UFS       PartType = "516E7CB6-6ECF-11D6-8FF8-00022D09712B" // Unix File System
	GPT_FREEBSD_VINUM     PartType = "516E7CB8-6ECF-11D6-8FF8-00022D09712B" // Vinum volume manager
	GPT_FREEBSD_ZFS       PartType = "516E7CBA-6ECF-11D6-8FF8-00022D09712B" // ZFS
	GPT_FREEBSD_NANDFS    PartType = "74BA7DD9-A689-11E1-BD04-00E081286ACF" // nandfs

	// macOS / Darwin
	GPT_APPLE_HFS_PLUS       PartType = "48465300-0000-11AA-AA11-00306543ECAC" // Apple HFS+
	GPT_APPLE_APFS_CONTAINER PartType = "7C3457EF-0000-11AA-AA11-00306543ECAC" // Apple APFS container
	GPT_APPLE_UFS_CONTAINER  PartType = "55465300-0000-11AA-AA11-00306543ECAC" // Apple UFS container
	GPT_APPLE_ZFS            PartType = "6A898CC3-1DD2-11B2-99A6-080020736631" // ZFS
	GPT_APPLE_RAID           PartType = "52414944-0000-11AA-AA11-00306543ECAC" // Apple RAID
	GPT_APPLE_RAID_OFFLINE   PartType = "52414944-5F4F-11AA-AA11-00306543ECAC" // Apple RAID offline
	GPT_APPLE_BOOT           PartType = "426F6F74-0000-11AA-AA11-00306543ECAC" // Apple Boot
	GPT_APPLE_LABEL          PartType = "4C616265-6C00-11AA-AA11-00306543ECAC" // Apple Label
	GPT_APPLE_CORE_STORAGE   PartType = "53746F72-6167-11AA-AA11-00306543ECAC" // Apple Core Storage
	GPT_APPLE_PREBOOT        PartType = "69646961-6700-11AA-AA11-00306543ECAC" // APFS Preboot
	GPT_APPLE_RECOVERY       PartType = "52637672-7900-11AA-AA11-00306543ECAC" // APFS Recovery

	// Solaris / illumos
	GPT_SOLARIS_BOOT      PartType = "6A82CB45-1DD2-11B2-99A6-080020736631" // Boot
	GPT_SOLARIS_ROOT      PartType = "6A85CF4D-1DD2-11B2-99A6-080020736631" // Root
	GPT_SOLARIS_SWAP      PartType = "6A87C46F-1DD2-11B2-99A6-080020736631" // Swap
	GPT_SOLARIS_BACKUP    PartType = "6A8B642B-1DD2-11B2-99A6-080020736631" // Backup
	GPT_SOLARIS_USR       PartType = "6A898CC3-1DD2-11B2-99A6-080020736631" // /usr
	GPT_SOLARIS_VAR       PartType = "6A8EF2E9-1DD2-11B2-99A6-080020736631" // /var
	GPT_SOLARIS_HOME      PartType = "6A90BA39-1DD2-11B2-99A6-080020736631" // /home
	GPT_SOLARIS_ALTSECTOR PartType = "6A9283A5-1DD2-11B2-99A6-080020736631" // Alternate sector
	GPT_SOLARIS_RESERVED1 PartType = "6A945A3B-1DD2-11B2-99A6-080020736631" // Reserved
	GPT_SOLARIS_RESERVED2 PartType = "6A9630D1-1DD2-11B2-99A6-080020736631" // Reserved
	GPT_SOLARIS_RESERVED3 PartType = "6A980767-1DD2-11B2-99A6-080020736631" // Reserved
	GPT_SOLARIS_RESERVED4 PartType = "6A96237F-1DD2-11B2-99A6-080020736631" // Reserved
	GPT_SOLARIS_RESERVED5 PartType = "6A8D2AC7-1DD2-11B2-99A6-080020736631" // Reserved

	// NetBSD
	GPT_NETBSD_SWAP      PartType = "49F48D32-B10E-11DC-B99B-0019D1879648" // Swap
	GPT_NETBSD_FFS       PartType = "49F48D5A-B10E-11DC-B99B-0019D1879648" // FFS
	GPT_NETBSD_LFS       PartType = "49F48D82-B10E-11DC-B99B-0019D1879648" // LFS
	GPT_NETBSD_RAID      PartType = "49F48DAA-B10E-11DC-B99B-0019D1879648" // RAID
	GPT_NETBSD_CONCAT    PartType = "2DB519C4-B10F-11DC-B99B-0019D1879648" // Concatenated
	GPT_NETBSD_ENCRYPTED PartType = "2DB519EC-B10F-11DC-B99B-0019D1879648" // Encrypted

	// ChromeOS
	GPT_CHROMEOS_KERNEL    PartType = "FE3A2A5D-4F32-41A7-B725-ACCC3285A309"   // ChromeOS kernel
	GPT_CHROMEOS_ROOTFS    PartType = "3CB8C8E202-3B7E-47DD-8A3C-7FF2A13CFCEC" // ChromeOS rootfs
	GPT_CHROMEOS_FIRMWARE  PartType = "CAB6E88E-ABF3-4102-A07A-D4BB9BE3C1D3"   // ChromeOS firmware
	GPT_CHROMEOS_FUTURE    PartType = "2E0A753D-9E48-43B0-8337-B15192CB1B5E"   // ChromeOS future use
	GPT_CHROMEOS_MINIOS    PartType = "09845860-705F-4BB5-B16C-8A8A099CAF52"   // ChromeOS miniOS
	GPT_CHROMEOS_HIBERNATE PartType = "3F0F8318-F146-4E6B-8222-C28C8F02E0D5"   // ChromeOS hibernate

	// Container Linux (CoreOS)
	GPT_COREOS_USR              PartType = "5DFBF5F4-2848-4BAC-AA5E-0D9A20B745A6" // /usr
	GPT_COREOS_RESIZABLE_ROOTFS PartType = "3884DD41-8582-4404-B9A8-E9B84F2DF50E" // Resizable rootfs
	GPT_COREOS_OEM_CUSTOM       PartType = "C95DC21A-DF0E-4340-8D7B-26CBFA9A03E0" // OEM customizations
	GPT_COREOS_ROOT_RAID        PartType = "BE9067B9-EA49-4F15-B4F6-F36F8C9E1818" // Root filesystem on RAID

	// Haiku
	GPT_HAIKU_BFS PartType = "42465331-3BA3-10F1-802A-4861696B7521" // Haiku BFS

	// MidnightBSD
	GPT_MIDNIGHTBSD_BOOT  PartType = "85D5E45E-237C-11E1-B4B3-E89A8F7FC3A7" // Boot
	GPT_MIDNIGHTBSD_DATA  PartType = "85D5E45A-237C-11E1-B4B3-E89A8F7FC3A7" // Data
	GPT_MIDNIGHTBSD_SWAP  PartType = "85D5E45B-237C-11E1-B4B3-E89A8F7FC3A7" // Swap
	GPT_MIDNIGHTBSD_UFS   PartType = "0394EF8B-237E-11E1-B4B3-E89A8F7FC3A7" // Unix File System
	GPT_MIDNIGHTBSD_VINUM PartType = "85D5E45C-237C-11E1-B4B3-E89A8F7FC3A7" // Vinum volume manager
	GPT_MIDNIGHTBSD_ZFS   PartType = "85D5E45D-237C-11E1-B4B3-E89A8F7FC3A7" // ZFS

	// OpenBSD
	GPT_OPENBSD_DATA PartType = "824CC7A0-36A8-11E3-890A-952519AD3F61" // Data (OpenBSD)

	// QNX
	GPT_QNX_POWERSAFE PartType = "CEF5A9AD-73BC-4601-89F3-CDEEEEE321A1" // Power-safe (QNX6)

	// Plan9
	GPT_PLAN9 PartType = "C91818F9-8025-47AF-89D2-F030D7000C2C" // Plan 9

	// VMware ESX
	GPT_VMKCORE         PartType = "9D275380-40AD-11DB-BF97-000C2911D1B8" // ESX vmkcore
	GPT_VMFS            PartType = "AA31E02A-400F-11DB-9590-000C2911D1B8" // VMFS filesystem
	GPT_VMWARE_RESERVED PartType = "9198EFFC-31C0-11DB-8F78-000C2911D1B8" // VMware Reserved

	// Android-IA
	GPT_ANDROID_BOOTLOADER  PartType = "2568845D-2332-4675-BC39-8FA5A4748D15" // Bootloader
	GPT_ANDROID_BOOTLOADER2 PartType = "114EAFFE-1552-4022-B26E-9B053604CF84" // Bootloader2
	GPT_ANDROID_BOOT        PartType = "49A4D17F-93A3-45C1-A0DE-F50B2EBE2599" // Boot
	GPT_ANDROID_RECOVERY    PartType = "4177C722-9E92-4AAB-8644-43502BFD5506" // Recovery
	GPT_ANDROID_MISC        PartType = "EF32A33B-A409-486C-9141-9FFB711F6266" // Misc
	GPT_ANDROID_METADATA    PartType = "20AC26BE-20B7-11E3-84C5-6CFDB94711E9" // Metadata
	GPT_ANDROID_SYSTEM      PartType = "38F428E6-D326-425D-9140-6E0EA133647C" // System
	GPT_ANDROID_CACHE       PartType = "A893EF21-E428-470A-9E55-0668FD91A2D9" // Cache
	GPT_ANDROID_DATA        PartType = "DC76DDA9-5AC1-491C-AF42-A82591580C0D" // Data
	GPT_ANDROID_PERSISTENT  PartType = "EBC597D0-2053-4B15-8B64-E0AAC75F4DB1" // Persistent
	GPT_ANDROID_VENDOR      PartType = "C5A0AEEC-13EA-11E5-A1B1-001E67CA0C3C" // Vendor
	GPT_ANDROID_CONFIG      PartType = "BD59408B-4514-490D-BF12-9878D963F378" // Config
	GPT_ANDROID_FACTORY     PartType = "8F68CC74-C5E5-48DA-BE91-A0C8C15E9C80" // Factory
	GPT_ANDROID_FACTORY2    PartType = "9FDAA6EF-4B3F-40D2-BA8D-BFF16BFB887B" // Factory (alternate)
	GPT_ANDROID_FASTBOOT    PartType = "767941D0-2085-11E3-AD3B-6CFDB94711E9" // Fastboot / Tertiary
	GPT_ANDROID_OEM         PartType = "AC6D7924-EB71-4DF8-B48D-E267B27148FF" // OEM

	// Fuchsia standard partitions
	GPT_FUCHSIA_BOOTLOADER          PartType = "FE8A2634-5E2E-46BA-99E3-3A192091A350" // Bootloader
	GPT_FUCHSIA_MUTABLE_ENCRYPTED   PartType = "D9FD4535-106C-4CEC-8D37-DFC020CA87CB" // Durable mutable encrypted system data
	GPT_FUCHSIA_MUTABLE_BL          PartType = "A409E16B-78AA-4ACC-995C-302352621A41" // Bootloader data
	GPT_FUCHSIA_FACT_SYS            PartType = "F95D940E-CABA-4578-9B93-BB6C90F29D3E" // Factory-provisioned system data
	GPT_FUCHSIA_FACT_BL             PartType = "10B8DBAA-D2BF-42A9-98C6-A7C5DB3701E7" // Factory-provisioned bootloader data
	GPT_FUCHSIA_FVM                 PartType = "49FD7CB8-DF15-4E73-B9D9-992070127F0F" // Fuchsia Volume Manager
	GPT_FUCHSIA_VERIFIED_BOOT       PartType = "421A8BFC-85D9-4D85-ACDA-B64EEC0133E9" // Verified boot metadata
	GPT_FUCHSIA_LEGACY_ESP          PartType = "C12A7328-F81F-11D2-BA4B-00A0C93EC93B" // fuchsia-esp (same as EFI)
	GPT_FUCHSIA_LEGACY_SYSTEM       PartType = "606B000B-B7C7-4653-A7D5-B737332C899D" // fuchsia-system
	GPT_FUCHSIA_LEGACY_DATA         PartType = "08185F0C-892D-428A-A789-DBEEC8F55E6A" // fuchsia-data
	GPT_FUCHSIA_LEGACY_INSTALL      PartType = "48435546-4953-2041-494E-5354414C4C52" // fuchsia-install
	GPT_FUCHSIA_LEGACY_BLOB         PartType = "2967380E-134C-4CBB-B6DA-17E7CE1CA45D" // fuchsia-blob
	GPT_FUCHSIA_LEGACY_FVM          PartType = "41D0E340-57E3-954E-8C1E-17ECAC44CFF7" // fuchsia-fvm
	GPT_FUCHSIA_LEGACY_ZIRCON_BOOT1 PartType = "DE30CC86-1F4A-4A31-93C4-66F147D33E05" // Zircon boot image
	GPT_FUCHSIA_LEGACY_ZIRCON_BOOT2 PartType = "23CC04DF-C278-4CE7-8471-897D1A4BCDF7" // Zircon boot image
	GPT_FUCHSIA_LEGACY_ZIRCON_BOOT3 PartType = "A0E5CF57-2DEF-46BE-A80C-A2067C37CD49" // Zircon boot image
	GPT_FUCHSIA_LEGACY_ZIRCON_BOOT4 PartType = "4E5E989E-4C86-11E8-A15B-480FCF35F8E6" // Zircon boot image
	GPT_FUCHSIA_LEGACY_SYS_CONFIG   PartType = "23CC04DF-C278-4CE7-8471-897D1A4BCDF7" // sys-config (alternate)
	GPT_FUCHSIA_LEGACY_MISC         PartType = "1D75395D-F2C6-476B-A8B7-45CC1C97B476" // misc
	GPT_FUCHSIA_LEGACY_BOOT1        PartType = "900B0FC5-90CD-4D4F-84F9-9F8ED579DB88" // emmc-boot1
	GPT_FUCHSIA_LEGACY_BOOT2        PartType = "B2B2E8D1-7C10-4EBC-A2D0-4614568260AD" // emmc-boot2

	// SoftRAID
	GPT_SOFTRAID_STATUS  PartType = "B6FA30DA-92D2-4A9A-96F1-871EC6486200" // SoftRAID_Status
	GPT_SOFTRAID_SCRATCH PartType = "2E313465-19B9-463F-8126-8A7993773801" // SoftRAID_Scratch
	GPT_SOFTRAID_VOLUME  PartType = "FA709C7E-65B1-4593-BFD5-E71D61DE9B02" // SoftRAID_Volume
	GPT_SOFTRAID_CACHE   PartType = "BBBA6DF5-F46F-4A89-8F59-8765B2727503" // SoftRAID_Cache

	// VeraCrypt (encrypted)
	GPT_VERACRYPT_ENCRYPTED PartType = "8C8F8EFF-AC95-4770-814A-21994F2DBC8F" // VeraCrypt encrypted data

	// ArcaOS / OS/2
	GPT_ARCAOS_TYPE1 PartType = "90B6FF38-B98F-4358-A21F-48F35B4A8AD3" // ArcaOS Type 1

	// SPDK
	GPT_SPDK_BLOCK_DEVICE PartType = "7C5222BD-8F5D-4087-9C00-BF9843C7B58C" // SPDK block device

	// barebox
	GPT_BAREBOX_STATE PartType = "4778ED65-BF42-45FA-9C5B-287A1DC4AAB1" // barebox-state

	// U-Boot
	GPT_UBOOT PartType = "3DE21764-95BD-54BD-A5C3-4ABE786F38A8" // U-Boot

	// SoftRAID (redundant group, already covered)
	// (已在上述 SoftRAID_* 常量里定义)

	// freedesktop.org shared boot loader config
	GPT_FREEDESKTOP_SHARED_BOOT PartType = "BC13C2FF-59E6-4262-A352-B275FD6F7172" // Shared boot loader configuration (Linux etc.)

	// Atari TOS
	GPT_ATARI_BASIC PartType = "734E5AFE-F61A-11E6-BC64-92361F002671" // Atari TOS basic data
)

var TypeDescMapping = map[PartType]string{
	//
	// -------------------- MBR磁盘 --------------------
	//

	MBR_EMPTY_PARTITION:               "Empty",
	MBR_FAT12_PARTITION:               "FAT12",
	MBR_XENIX_ROOT_PARTITION:          "XENIX root",
	MBR_XENIX_USR_PARTITION:           "XENIX usr",
	MBR_FAT16_LESS32M_PARTITION:       "FAT16 <32M",
	MBR_DOS_EXTENDED_PARTITION:        "DOS Extended",
	MBR_FAT16_PARTITION:               "FAT16 >=32M",
	MBR_HPFS_NTFS_PARTITION:           "HPFS/NTFS/QNX/exFAT",
	MBR_AIX_PARTITION:                 "AIX boot or SplitDrive",
	MBR_AIX_BOOTABLE_PARTITION:        "AIX data or Coherent",
	MBR_OS2_BOOTMNGR_PARTITION:        "OS/2 Boot Manager",
	MBR_W95_FAT32_PARTITION:           "W95 FAT32",
	MBR_W95_FAT32_LBA_PARTITION:       "W95 FAT32 LBA",
	MBR_W95_FAT16_LBA_PARTITION:       "W95 FAT16 LBA",
	MBR_W95_EXTENDED_PARTITION:        "W95 Extended",
	MBR_OPUS_PARTITION:                "OPUS",
	MBR_HIDDEN_FAT12_PARTITION:        "Hidden FAT12",
	MBR_COMPAQ_DIAGNOSTICS_PARTITION:  "Compaq Diagnostics",
	MBR_HIDDEN_FAT16_L32M_PARTITION:   "Hidden FAT16 <32M",
	MBR_HIDDEN_FAT16_PARTITION:        "Hidden FAT16 >=32M",
	MBR_HIDDEN_HPFS_NTFS_PARTITION:    "Hidden HPFS/NTFS",
	MBR_AST_SMARTSLEEP_PARTITION:      "AST SmartSleep",
	MBR_HIDDEN_W95_FAT32_PARTITION:    "Hidden W95 FAT32",
	MBR_HIDDEN_W95_FAT32LBA_PARTITION: "Hidden W95 FAT32 LBA",
	MBR_HIDDEN_W95_FAT16LBA_PARTITION: "Hidden W95 FAT16 LBA",
	MBR_NEC_DOS_PARTITION:             "NEC DOS",
	MBR_PLAN9_PARTITION:               "Plan 9",
	MBR_PARTITIONMAGIC_PARTITION:      "PartitionMagic",
	MBR_VENIX80286_PARTITION:          "Venix 80286",
	MBR_PPC_PREP_BOOT_PARTITION:       "PPC PReP Boot",
	MBR_SFS_PARTITION:                 "SFS",
	MBR_QNX_4X_PARTITION:              "QNX 4.x primary",
	MBR_QNX_4X_2ND_PARTITION:          "QNX 4.x secondary",
	MBR_QNX_4X_3RD_PARTITION:          "QNX 4.x third",
	MBR_DM_PARTITION:                  "DM",
	MBR_DM6_AUX1_PARTITION:            "DM6 Aux1 or Novell",
	MBR_CPM_PARTITION:                 "CP/M or Microport SysV/AT",
	MBR_DM6_AUX3_PARTITION:            "DM6 Aux3",
	MBR_DM6_PARTITION:                 "DM6",
	MBR_EZ_DRIVE_PARTITION:            "EZ-Drive",
	MBR_GOLDEN_BOW_PARTITION:          "Golden Bow",
	MBR_PRIAM_EDISK_PARTITION:         "Priam Edisk",
	MBR_SPEEDSTOR_PARTITION:           "SpeedStor",
	MBR_GNU_HURD_PARTITION:            "GNU HURD/Mach/Sys V/386",
	//MBR_UNIXWARE_PARTITION:             "UNIXWARE (alias GNU HURD)",
	MBR_NETWARE_286_PARTITION:          "NetWare 286",
	MBR_NETWARE_386_PARTITION:          "NetWare 386",
	MBR_DISKSECURE_MULTIBOOT_PARTITION: "DiskSecure Multiboot",
	MBR_PC_IX_PARTITION:                "PC/IX",
	MBR_OLD_MINIX_PARTITION:            "Old Minix",
	MBR_MINIX_PARTITION:                "Minix",
	MBR_LINUX_SWAP_PARTITION:           "Linux Swap",
	//MBR_SOLARIS_X86_PARTITION:          "Solaris x86 (alias Linux Swap)",
	MBR_LINUX_DATA_PARTITION:       "Linux",
	MBR_OS2_HIDDEN_DRIVE_PARTITION: "OS/2 Hidden Drive",
	//MBR_INTEL_HIBERNATION_PARTITION:    "Intel Hibernation",
	MBR_LINUX_EXTENDED_PARTITION:       "Linux Extended",
	MBR_NTFS_VOL_SET1_PARTITION:        "NTFS Volume Set 1",
	MBR_NTFS_VOL_SET2_PARTITION:        "NTFS Volume Set 2",
	MBR_LINUX_PLAINTEXT_PARTITION:      "Linux Plaintext",
	MBR_LINUX_LVM_PARTITION:            "Linux LVM",
	MBR_AMOEBA_PARTITION:               "Amoeba",
	MBR_AMOEBA_BBT_PARTITION:           "Amoeba BBT",
	MBR_BSD_OS_PARTITION:               "BSD OS",
	MBR_THINKPAD_HIBERNATION_PARTITION: "ThinkPad Hibernation",
	MBR_FREEBSD_PARTITION:              "FreeBSD",
	MBR_OPENBSD_PARTITION:              "OpenBSD",
	MBR_NEXTSTEP_PARTITION:             "NeXTSTEP",
	MBR_DARWIN_UFS_PARTITION:           "Darwin UFS",
	MBR_NETBSD_PARTITION:               "NetBSD",
	MBR_DARWIN_BOOT_PARTITION:          "Darwin Boot",
	MBR_HFS_HFS_PARTITION:              "HFS/HFS+",
	MBR_BSDI_FS_PARTITION:              "BSDI FS",
	MBR_BSDI_SWAP_PARTITION:            "BSDI Swap",
	MBR_BOOTWIZARD_HIDDEN_PARTITION:    "BootWizard Hidden",
	MBR_ACRONIS_FAT32LBA_PARTITION:     "Acronis FAT32 LBA",
	MBR_SOLARIS_BOOT_PARTITION:         "Solaris Boot",
	MBR_SOLARIS_PARTITION:              "Solaris",
	MBR_DRDOS_FAT12_PARTITION:          "DRDOS FAT12",
	MBR_DRDOS_FAT16_L32M_PARTITION:     "DRDOS FAT16 <32M",
	MBR_DRDOS_FAT16_PARTITION:          "DRDOS FAT16",
	MBR_SYRINX_PARTITION:               "Syrinx",
	MBR_NONFS_DATA_PARTITION:           "Non-FS Data",
	MBR_CPM_CTOS_PARTITION:             "CP/M, Concurrent DOS or CTOS",
	MBR_DELL_UTILITY_PARTITION:         "Dell Utility",
	MBR_BOOTIT_PARTITION:               "BootIt",
	MBR_DOS_ACCESS_PARTITION:           "DOS Access / SpeedStor 12-bit",
	MBR_DOS_RO_PARTITION:               "DOS R/O or SpeedStor",
	MBR_SPEEDSTOR_EXTENDED_PARTITION:   "SpeedStor 16-bit Extended",
	MBR_RUFUS_EXTRA_PARTITION:          "Rufus Extra",
	MBR_BEOS_FS_PARTITION:              "BeOS FS",
	MBR_GPT_PARTITION:                  "GPT Protective",
	MBR_EFI_SYSTEM_PARTITION:           "EFI System",
	MBR_LINUX_PARISC_BOOT_PARTITION:    "Linux/PA-RISC Boot",
	MBR_SPEEDSTOR1_PARTITION:           "SpeedStor 1",
	MBR_SPEEDSTOR2_PARTITION:           "SpeedStor 2",
	MBR_DOS_SECONDARY_PARTITION:        "DOS Secondary",
	MBR_EBBR_PROTECTIVE_PARTITION:      "EBBR Protective",
	MBR_VMWARE_VMFS_PARTITION:          "VMware VMFS",
	MBR_VMWARE_VMKCORE_PARTITION:       "VMware VMKCore",
	MBR_LINUX_RAID_PARTITION:           "Linux RAID",
	MBR_LANSTEP_PARTITION:              "LANstep / SpeedStor >1024 cyl.",
	MBR_XENIX_BBT_PARTITION:            "Xenix BBT",

	//
	// -------------------- GPT磁盘 --------------------
	//

	// Generic / Unused / Reserved
	GPT_UNUSED_ENTRY:     "Unused entry",
	GPT_MBR:              "MBR protective",
	GPT_EFI:              "EFI System Partition",
	GPT_BIOS_BOOT:        "BIOS boot partition",
	GPT_INTEL_FAST_FLASH: "Intel Fast Flash",
	GPT_SONY_BOOT:        "Sony boot",
	GPT_LENOVO_BOOT:      "Lenovo boot",

	// Windows / Microsoft
	GPT_MSFT_RESERVED:        "Microsoft Reserved",
	GPT_MSFT_BASIC_DATA:      "Microsoft basic data",
	GPT_MSFT_LDM_METADATA:    "Logical Disk Manager metadata",
	GPT_MSFT_LDM_DATA:        "Logical Disk Manager data",
	GPT_MSFT_RECOVERY:        "Windows Recovery",
	GPT_MSFT_GPFS:            "IBM GPFS",
	GPT_MSFT_STORAGE_SPACES:  "Storage Spaces",
	GPT_MSFT_STORAGE_REPLICA: "Storage Replica",

	// HP-UX
	GPT_HPUX_DATA:    "HP-UX Data",
	GPT_HPUX_SERVICE: "HP-UX Service",

	// Linux
	GPT_LINUX_FS_DATA:  "Linux filesystem data",
	GPT_LINUX_RAID:     "Linux RAID",
	GPT_ROOT_X86:       "Linux Root (x86)",
	GPT_ROOT_X86_64:    "Linux Root (x86-64)",
	GPT_ROOT_ARM32:     "Linux Root (32-bit ARM)",
	GPT_ROOT_ARM64:     "Linux Root (64-bit ARM / AArch64)",
	GPT_LINUX_BOOT:     "Linux /boot (shared boot loader)",
	GPT_LINUX_SWAP:     "Linux Swap",
	GPT_LINUX_LVM:      "Linux LVM",
	GPT_LINUX_HOME:     "Linux /home",
	GPT_LINUX_SRV:      "Linux /srv",
	GPT_LINUX_DM_CRYPT: "Linux dm-crypt",
	GPT_LINUX_LUKS:     "Linux LUKS encrypted",
	GPT_LINUX_RESERVED: "Linux Reserved",

	// FreeBSD
	GPT_FREEBSD_BOOT:      "FreeBSD Boot",
	GPT_FREEBSD_DISKLABEL: "FreeBSD Disklabel",
	GPT_FREEBSD_SWAP:      "FreeBSD Swap",
	GPT_FREEBSD_UFS:       "FreeBSD UFS",
	GPT_FREEBSD_VINUM:     "FreeBSD Vinum",
	GPT_FREEBSD_ZFS:       "FreeBSD ZFS",
	GPT_FREEBSD_NANDFS:    "FreeBSD nandfs",

	// macOS
	GPT_APPLE_HFS_PLUS:       "Apple HFS+",
	GPT_APPLE_APFS_CONTAINER: "Apple APFS container",
	GPT_APPLE_UFS_CONTAINER:  "Apple UFS container",
	GPT_APPLE_ZFS:            "Apple ZFS",
	GPT_APPLE_RAID:           "Apple RAID",
	GPT_APPLE_RAID_OFFLINE:   "Apple RAID offline",
	GPT_APPLE_BOOT:           "Apple Boot",
	GPT_APPLE_LABEL:          "Apple Label",
	GPT_APPLE_CORE_STORAGE:   "Apple Core Storage",
	GPT_APPLE_PREBOOT:        "APFS Preboot",
	GPT_APPLE_RECOVERY:       "APFS Recovery",

	// Solaris / illumos
	GPT_SOLARIS_BOOT:   "Solaris Boot",
	GPT_SOLARIS_ROOT:   "Solaris Root",
	GPT_SOLARIS_SWAP:   "Solaris Swap",
	GPT_SOLARIS_BACKUP: "Solaris Backup",
	//GPT_SOLARIS_USR:       "Solaris /usr",
	GPT_SOLARIS_VAR:       "Solaris /var",
	GPT_SOLARIS_HOME:      "Solaris /home",
	GPT_SOLARIS_ALTSECTOR: "Solaris Alternate sector",
	GPT_SOLARIS_RESERVED1: "Solaris Reserved1",
	GPT_SOLARIS_RESERVED2: "Solaris Reserved2",
	GPT_SOLARIS_RESERVED3: "Solaris Reserved3",
	GPT_SOLARIS_RESERVED4: "Solaris Reserved4",
	GPT_SOLARIS_RESERVED5: "Solaris Reserved5",

	// NetBSD
	GPT_NETBSD_SWAP:      "NetBSD Swap",
	GPT_NETBSD_FFS:       "NetBSD FFS",
	GPT_NETBSD_LFS:       "NetBSD LFS",
	GPT_NETBSD_RAID:      "NetBSD RAID",
	GPT_NETBSD_CONCAT:    "NetBSD Concatenated",
	GPT_NETBSD_ENCRYPTED: "NetBSD Encrypted",

	// ChromeOS
	GPT_CHROMEOS_KERNEL:    "ChromeOS kernel",
	GPT_CHROMEOS_ROOTFS:    "ChromeOS rootfs",
	GPT_CHROMEOS_FIRMWARE:  "ChromeOS firmware",
	GPT_CHROMEOS_FUTURE:    "ChromeOS future use",
	GPT_CHROMEOS_MINIOS:    "ChromeOS miniOS",
	GPT_CHROMEOS_HIBERNATE: "ChromeOS hibernate",

	// CoreOS
	GPT_COREOS_USR:              "CoreOS /usr",
	GPT_COREOS_RESIZABLE_ROOTFS: "CoreOS Resizable rootfs",
	GPT_COREOS_OEM_CUSTOM:       "CoreOS OEM customizations",
	GPT_COREOS_ROOT_RAID:        "CoreOS Root RAID",

	// Haiku
	GPT_HAIKU_BFS: "Haiku BFS",

	// MidnightBSD
	GPT_MIDNIGHTBSD_BOOT:  "MidnightBSD Boot",
	GPT_MIDNIGHTBSD_DATA:  "MidnightBSD Data",
	GPT_MIDNIGHTBSD_SWAP:  "MidnightBSD Swap",
	GPT_MIDNIGHTBSD_UFS:   "MidnightBSD UFS",
	GPT_MIDNIGHTBSD_VINUM: "MidnightBSD Vinum",
	GPT_MIDNIGHTBSD_ZFS:   "MidnightBSD ZFS",

	// OpenBSD
	GPT_OPENBSD_DATA: "OpenBSD Data",

	// QNX
	GPT_QNX_POWERSAFE: "QNX Power-safe",

	// Plan9
	GPT_PLAN9: "Plan 9",

	// VMware
	GPT_VMKCORE:         "VMware ESX vmkcore",
	GPT_VMFS:            "VMware VMFS",
	GPT_VMWARE_RESERVED: "VMware Reserved",

	// Android
	GPT_ANDROID_BOOTLOADER:  "Android Bootloader",
	GPT_ANDROID_BOOTLOADER2: "Android Bootloader2",
	GPT_ANDROID_BOOT:        "Android Boot",
	GPT_ANDROID_RECOVERY:    "Android Recovery",
	GPT_ANDROID_MISC:        "Android Misc",
	GPT_ANDROID_METADATA:    "Android Metadata",
	GPT_ANDROID_SYSTEM:      "Android System",
	GPT_ANDROID_CACHE:       "Android Cache",
	GPT_ANDROID_DATA:        "Android Data",
	GPT_ANDROID_PERSISTENT:  "Android Persistent",
	GPT_ANDROID_VENDOR:      "Android Vendor",
	GPT_ANDROID_CONFIG:      "Android Config",
	GPT_ANDROID_FACTORY:     "Android Factory",
	GPT_ANDROID_FACTORY2:    "Android Factory2",
	GPT_ANDROID_FASTBOOT:    "Android Fastboot",
	GPT_ANDROID_OEM:         "Android OEM",

	// Fuchsia
	GPT_FUCHSIA_BOOTLOADER:        "Fuchsia Bootloader",
	GPT_FUCHSIA_MUTABLE_ENCRYPTED: "Fuchsia Durable mutable encrypted",
	GPT_FUCHSIA_MUTABLE_BL:        "Fuchsia Bootloader data",
	GPT_FUCHSIA_FACT_SYS:          "Fuchsia Factory system data",
	GPT_FUCHSIA_FACT_BL:           "Fuchsia Factory bootloader data",
	GPT_FUCHSIA_FVM:               "Fuchsia Volume Manager",
	GPT_FUCHSIA_VERIFIED_BOOT:     "Fuchsia Verified boot metadata",
	//GPT_FUCHSIA_LEGACY_ESP:          "Fuchsia Legacy ESP",
	GPT_FUCHSIA_LEGACY_SYSTEM:       "Fuchsia Legacy System",
	GPT_FUCHSIA_LEGACY_DATA:         "Fuchsia Legacy Data",
	GPT_FUCHSIA_LEGACY_INSTALL:      "Fuchsia Legacy Install",
	GPT_FUCHSIA_LEGACY_BLOB:         "Fuchsia Legacy Blob",
	GPT_FUCHSIA_LEGACY_FVM:          "Fuchsia Legacy FVM",
	GPT_FUCHSIA_LEGACY_ZIRCON_BOOT1: "Fuchsia Zircon boot1",
	GPT_FUCHSIA_LEGACY_ZIRCON_BOOT2: "Fuchsia Zircon boot2",
	GPT_FUCHSIA_LEGACY_ZIRCON_BOOT3: "Fuchsia Zircon boot3",
	GPT_FUCHSIA_LEGACY_ZIRCON_BOOT4: "Fuchsia Zircon boot4",
	//GPT_FUCHSIA_LEGACY_SYS_CONFIG:   "Fuchsia sys-config",
	GPT_FUCHSIA_LEGACY_MISC:  "Fuchsia misc",
	GPT_FUCHSIA_LEGACY_BOOT1: "Fuchsia emmc-boot1",
	GPT_FUCHSIA_LEGACY_BOOT2: "Fuchsia emmc-boot2",

	// SoftRAID
	GPT_SOFTRAID_STATUS:  "SoftRAID Status",
	GPT_SOFTRAID_SCRATCH: "SoftRAID Scratch",
	GPT_SOFTRAID_VOLUME:  "SoftRAID Volume",
	GPT_SOFTRAID_CACHE:   "SoftRAID Cache",

	// VeraCrypt
	GPT_VERACRYPT_ENCRYPTED: "VeraCrypt Encrypted",

	// ArcaOS
	GPT_ARCAOS_TYPE1: "ArcaOS Type 1",

	// SPDK
	GPT_SPDK_BLOCK_DEVICE: "SPDK Block Device",

	// barebox
	GPT_BAREBOX_STATE: "barebox-state",

	// U-Boot
	GPT_UBOOT: "U-Boot",

	// freedesktop shared boot loader
	//GPT_FREEDESKTOP_SHARED_BOOT: "Shared boot loader config (freedesktop)",

	// Atari TOS
	GPT_ATARI_BASIC: "Atari TOS Basic",
}
