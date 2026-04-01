package sysrepair

// KernelCompat 内核兼容性
type KernelCompat struct {
	// Kernel 内核名
	Kernel string

	// PciCompats 各Pci硬件的兼容性
	PciCompats []PciCompat
}
