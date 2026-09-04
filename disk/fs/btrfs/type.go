package btrfs

// ---------------- 常量 ----------------

const (
	btrfsSuperInfoOffset = 0x10000
	btrfsSuperMagic      = "_BHRfS_M"

	sbOffMagic               = 64 // csum(32)+fsid(16)+bytenr(8)+flags(8)
	sbOffGeneration          = 72
	sbOffRoot                = 80
	sbOffChunkRoot           = 88
	sbOffLogRoot             = 96
	sbOffLogRootTransid      = 104 // <- 之前漏算的这个字段
	sbOffTotalBytes          = 112 // <- 修正后
	sbOffBytesUsed           = 120
	sbOffRootDirObjectid     = 128
	sbOffNumDevices          = 136
	sbOffSectorSize          = 144
	sbOffNodeSize            = 148
	sbOffUnusedLeafSize      = 152
	sbOffStripeSize          = 156
	sbOffSysChunkArraySize   = 160
	sbOffChunkRootGeneration = 164
	sbOffCompatFlags         = 172
	sbOffCompatRoFlags       = 180
	sbOffIncompatFlags       = 188
	sbOffCsumType            = 196
	sbOffRootLevel           = 198
	sbOffChunkRootLevel      = 199
	sbOffLogRootLevel        = 200
	sbOffDevItem             = 201
	sbOffDevItemSize         = 98                              // btrfs_dev_item 结构体大小，逐字段算过，确认无误
	sbOffLabel               = sbOffDevItem + sbOffDevItemSize // 299
	sbOffLabelSize           = 256
	sbOffCacheGeneration     = sbOffLabel + sbOffLabelSize // 555
	sbOffUuidTreeGeneration  = sbOffCacheGeneration + 8    // 563
	sbOffMetadataUuid        = sbOffUuidTreeGeneration + 8 // 571
	sbOffReserved            = sbOffMetadataUuid + 16      // 587
	sbOffSysChunkArray       = sbOffReserved + 28*8        // 811  <- 之前算成了很小的值

	btrfsSystemChunkArraySize = 2048
	btrfsHeaderSize           = 101
	btrfsItemSize             = 25
	btrfsKeyPtrSize           = 33
	btrfsDiskKeySize          = 17
	btrfsChunkHeaderSize      = 48
	btrfsStripeSize           = 32

	keyExtentCsum   = 128
	keyExtentData   = 108
	keyExtentItem   = 168
	keyMetadataItem = 169
	keyChunkItem    = 228
	keyRootItem     = 132

	firstChunkTreeObjectid = 256

	bgDup    = 1 << 5
	bgRaid1  = 1 << 4
	bgRaid10 = 1 << 6

	fileExtentInline = 0
)
