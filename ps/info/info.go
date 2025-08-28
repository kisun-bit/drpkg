package info

type PSInfo struct {
	Generic
	DmiInfo

	// IsMemoryOS 是否是内存操作系统
	IsMemoryOS bool `json:"isMemoryOS"`
	// IsVirtualHost 是否是虚拟机
	IsVirtualHost bool `json:"isVirtualHost"`
	// BootType 启动类型(bios、uefi)
	BootType string `json:"bootType"`
	// IFList 网卡信息
	IFList []IF `json:"ifList"`

	//
	// Linux系统有效的信息
	//

	// LinuxKernels Linux系统内核
	LinuxKernels []LinuxKernel `json:"linuxKernels"`
	LinuxRelease LinuxRelease  `json:"linuxRelease"`
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

	if pi.IFList, err = QueryIFList(); err != nil {
		return nil, err
	}

	//
	// Linux系统有效的信息
	//

	if pi.LinuxKernels, err = QueryLinuxKernels("/"); err != nil {
		return nil, err
	}
	pi.LinuxRelease = QueryLinuxRelease("/")

	return pi, nil
}
