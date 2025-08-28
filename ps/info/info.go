package info

type PSInfo struct {
	Generic
	DmiInfo

	// IsMemoryOS 是否是内存操作系统
	IsMemoryOS bool `json:"is_memory_os"`
	// IsVirtualHost 是否是虚拟机
	IsVirtualHost bool `json:"is_virtual_host"`
	// BootType 启动类型(bios、uefi)
	BootType string `json:"boot_type"`

	//
	// Linux系统有效的信息
	//

	// LinuxKernels Linux系统内核
	LinuxKernels []LinuxKernel `json:"linux_kernels"`
	LinuxRelease LinuxRelease  `json:"linux_release"`
}

// QueryPSInfo 查询系统信息
func QueryPSInfo() (pi *PSInfo, err error) {
	pi = new(PSInfo)

	if pi.Generic, err = QueryGeneric(); err != nil {
		return nil, err
	}
	if pi.DmiInfo, err = QueryDmi(); err != nil {
		return nil, err
	}
	pi.IsMemoryOS = IsMemoryOS()
	pi.IsVirtualHost = IsVirtualHost(pi.DmiInfo.SystemName)
	pi.BootType = QueryBootType()

	//
	// Linux系统有效的信息
	//

	if pi.LinuxKernels, err = QueryLinuxKernels("/"); err != nil {
		return nil, err
	}
	pi.LinuxRelease = QueryLinuxRelease("/")

	return pi, nil
}
