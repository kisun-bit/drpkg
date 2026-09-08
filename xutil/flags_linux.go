package xutil

import (
	"os"
	"syscall"
)

const (
	W_DSYNC_MODE = os.O_WRONLY | syscall.O_DSYNC
	R_DSYNC_MODE = os.O_RDONLY | syscall.O_DSYNC | syscall.O_DIRECT

	// MetaOpenFlags 是元数据文件打开标志（直写：同步数据完整性，不经过 OS buffer 延迟写入）。
	MetaOpenFlags = os.O_RDWR | syscall.O_DSYNC

	// DiskReadFlags 是源磁盘读取标志（直读：绕过 OS page cache）。
	// 要求读取缓冲区地址对齐到文件系统块大小（通常 512 字节），
	// 读取偏移和长度也必须对齐。
	DiskReadFlags = os.O_RDONLY | syscall.O_DIRECT
)
