package sysrepair

import (
	"fmt"
	"runtime"

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
	// Base 平台类型：virt / bare-metal
	Base HardwarePlatform `json:"base"`

	// Virt 虚拟化类型（仅当 Base=virt 时使用）
	Virt HPVirtType `json:"virt,omitempty"`

	// PciList PCI设备列表（仅当 Base=bare-metal 时使用）
	PciList []string `json:"pciList,omitempty"`
}

func RuntimePlatform() (pf Platform, err error) {
	pf.Base = HPUnknown
	pf.Virt = HPVTNone

	pciList, err := universal.ListUniPci()
	if err != nil {
		return pf, err
	}
	for _, pci := range pciList {

		switch {
		case pci.VendorId() == 0x15ad && pf.Base == HPUnknown:
			pf.Base = HPVirt
			pf.Virt = HPVTVmware
		case pci.VendorId() == 0x5853 && pf.Base == HPUnknown:
			pf.Base = HPVirt
			pf.Virt = HPVTXen
		case pci.VendorId() == 0x1af4 && pf.Base == HPUnknown:
			pf.Base = HPVirt
			pf.Virt = HPVTQemuKvm
		}

		pf.PciList = append(pf.PciList, pci.String())
	}

	// 检查是否是hyper-v环境

	switch runtime.GOOS {
	case "linux", "windows":
		yes, err := vmbusExisted()
		if err != nil {
			return pf, err
		}
		if yes && pf.Base == HPUnknown {
			pf.Base = HPVirt
			pf.Virt = HPVTHyperV
		}
	default:
		return pf, fmt.Errorf("unsupported platform type: %s", runtime.GOOS)
	}

	if pf.Base == HPUnknown {
		pf.Base = HPBareMetal
		pf.Virt = HPVTNone
	}

	return pf, nil
}
