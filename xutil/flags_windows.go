package xutil

import "os"

const (
	W_DSYNC_MODE = os.O_WRONLY | os.O_SYNC
	R_DSYNC_MODE = os.O_RDONLY | os.O_SYNC

	// MetaOpenFlags 是元数据文件打开标志。
	// 物理磁盘设备上 O_SYNC 会要求缓冲区扇区对齐（ProtectedDevice Record 71684 字节不满足），
	// 因此仅使用 O_RDWR。数据完整性由 Flush 中的 Sync 保证。
	MetaOpenFlags = os.O_RDWR

	// DiskReadFlags 是源磁盘读取标志。
	// Windows 下 os 包不直接暴露 FILE_FLAG_NO_BUFFERING，
	// 物理磁盘读取使用常规 open 即可。
	DiskReadFlags = os.O_RDONLY
)
