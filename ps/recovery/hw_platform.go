package recovery

import (
	"github.com/kisun-bit/drpkg/ps/bus/pci/universal"
)

// HardwarePlatform 基础硬件平台
type HardwarePlatform string

const (
	HPUnknown   HardwarePlatform = "unknown"
	HPVirt      HardwarePlatform = "virtual"    // 虚拟化 / 云平台
	HPBareMetal HardwarePlatform = "bare-metal" // 裸机（物理机）
)

// HPVirtType 虚拟化/云平台的类别
type HPVirtType string

const (
	HPVTNone    HPVirtType = "none"
	HPVTVmware  HPVirtType = "vmware"
	HPVTQemuKvm HPVirtType = "qemu/kvm"
	HPVTXen     HPVirtType = "xen"
	HPVTHyperV  HPVirtType = "hyper-v"
)

// Platform 表示一个运行环境（源或目标）
type Platform struct {
	// Architecture OCI风格的架构标识
	Architecture string `json:"architecture"`

	// Base 平台类型：virt / bare-metal
	Base HardwarePlatform `json:"base"`

	// Virt 虚拟化类型（仅当 Base=virt 时使用）
	Virt HPVirtType `json:"virt,omitempty"`

	// PciList PCI设备列表（仅当 Base=bare-metal 时使用）
	PciList []string `json:"pciList,omitempty"`
}

// RuntimePlatform 返回当前所运行系统的硬件平台信息
func RuntimePlatform() (pf Platform, err error) {
	pf.Base = HPUnknown
	pf.Virt = HPVTNone

	pciList, err := universal.ListUniPci()
	if err != nil {
		return pf, err
	}
	for _, pci := range pciList {

		switch {
		case pci.VendorId() == 0x15ad && pf.Base == HPUnknown: // 检查是否是vmware环境
			pf.Base = HPVirt
			pf.Virt = HPVTVmware
		case pci.VendorId() == 0x5853 && pf.Base == HPUnknown: // 检查是否是xen环境
			pf.Base = HPVirt
			pf.Virt = HPVTXen
		case pci.VendorId() == 0x1af4 && pf.Base == HPUnknown: // 检查是否是qemu/kvm环境
			pf.Base = HPVirt
			pf.Virt = HPVTQemuKvm
		}

		pf.PciList = append(pf.PciList, pci.String())
	}

	// 检查是否是hyper-v环境
	yes, err := vmbusExisted()
	if err != nil {
		return pf, err
	}
	if yes && pf.Base == HPUnknown {
		pf.Base = HPVirt
		pf.Virt = HPVTHyperV
	}

	// 排除所有虚拟化，确定为物理机环境
	if pf.Base == HPUnknown {
		pf.Base = HPBareMetal
		pf.Virt = HPVTNone
	}

	return pf, nil
}
