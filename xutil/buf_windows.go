package xutil

// AlignedBlock 在 Windows 上无需特殊对齐，直接返回 make 分配的切片。
func AlignedBlock(size int) []byte {
	return make([]byte, size)
}
