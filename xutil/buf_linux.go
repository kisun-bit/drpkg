package xutil

import "unsafe"

// AlignedBlock 分配一个起始地址对齐到 4096 字节的内存块。
// O_DIRECT 要求缓冲区地址对齐到文件系统块大小（通常 512 或 4096 字节），
// 但 Go 的 make 仅保证 8 字节对齐。这里通过过度分配 + 偏移实现 4096 对齐。
func AlignedBlock(size int) []byte {
	const align = 4096
	buf := make([]byte, size+align)
	p := uintptr(unsafe.Pointer(&buf[0]))
	off := (align - p%align) % align
	return buf[off : off+size]
}
