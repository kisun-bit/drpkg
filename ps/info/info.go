package info

import (
	"encoding/json"
	"runtime"
)

type PSInfo struct {
	Public  PublicInfo  `json:"public"`
	Private PrivateInfo `json:"private"`
}

type PublicInfo struct {
	Generic
	DmiInfo

	// IsMemoryOS 是否是内存操作系统
	IsMemoryOS bool `json:"isMemoryOS"`
	// IsVirtualHost 是否是虚拟机
	IsVirtualHost bool `json:"isVirtualHost"`
	// BootType 启动类型(bios、uefi)
	BootType string `json:"bootType"`
	// EnableVTX 是否支持虚拟cpu
	EnableVTX bool `json:"enableVTX"`
	// IFList 网卡信息
	IFList []IF `json:"ifList"`
}

type PrivateInfo struct {
	Linux   LinuxPrivateInfo   `json:"linuxPrivateInfo"`
	Windows WindowsPrivateInfo `json:"windowsPrivateInfo"`
}

type LinuxPrivateInfo struct {
	Effective bool          `json:"effective"`
	EfiDir    string        `json:"efiDir"`
	Kernels   []LinuxKernel `json:"kernels"`
	Release   LinuxRelease  `json:"release"`
	Swaps     []LinuxSwap   `json:"swaps"`
}

type WindowsPrivateInfo struct {
	Effective bool `json:"effective"`
	// FIXME
}

// QueryPSInfo 查询系统信息
func QueryPSInfo() (pi *PSInfo, err error) {
	pi = new(PSInfo)

	if err = pi.fillPublicInfo(); err != nil {
		return nil, err
	}
	if err = pi.fillPrivateInfo(); err != nil {
		return nil, err
	}

	return pi, nil
}

func (p *PSInfo) String() string {
	j, _ := json.MarshalIndent(*p, "", "  ")
	return string(j)
}

func (p *PSInfo) fillPublicInfo() (err error) {
	if p.Public.Generic, err = QueryGeneric(); err != nil {
		return err
	}
	if p.Public.DmiInfo, err = QueryDmi(); err != nil {
		return err
	}
	p.Public.IsMemoryOS = IsMemoryOS()
	p.Public.IsVirtualHost = IsVirtualHost(p.Public.DmiInfo.SystemName)
	p.Public.BootType = QueryBootType()
	p.Public.EnableVTX = SupportCPUVirtual()
	if p.Public.IFList, err = QueryIFList(); err != nil {
		return err
	}
	return nil
}

func (p *PSInfo) fillPrivateInfo() (err error) {
	switch runtime.GOOS {
	case "linux":
		p.Private.Linux.Effective = true
		if p.Private.Linux.Kernels, err = QueryLinuxKernels("/"); err != nil {
			return err
		}
		p.Private.Linux.Release = QueryLinuxRelease("/")
		if p.Private.Linux.Swaps, err = QuerySwapInfo(); err != nil {
			return err
		}
	case "windows":
		p.Private.Windows.Effective = true
	}
	return nil
}
