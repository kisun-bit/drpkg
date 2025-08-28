package info

type PSInfo struct {
	Generic
	DmiInfo

	// SystemManufacturer 主板制造商
	SystemManufacturer string
	// IsMemoryOS 是否是内存操作系统
	IsMemoryOS bool
	// IsVirtualHost 是否是虚拟机
	IsVirtualHost bool
	// BootType 启动类型(bios、uefi)
	BootType string

	//
	// Linux系统有效的信息
	//

	// LinuxKernels Linux系统内核
	LinuxKernels []LinuxKernel `json:"linux_kernels"`
	LinuxRelease LinuxRelease  `json:"linux_release"`
}
