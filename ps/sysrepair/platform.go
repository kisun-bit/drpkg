package sysrepair

import (
	"fmt"
	"runtime"

	"github.com/kisun-bit/drpkg/ps/bus/pci/universal"
)

//
// =========================
// 基础硬件平台（Hardware Platform）
// =========================
//
// [virt]        虚拟化 / 云平台
//               包括但不限于：vmware / qemu-kvm / xen / hyper-v
//
// [bare-metal]  裸机（物理机）
//
//
//
// =========================
// 备份主机类型（Backup Source Type）
// =========================
//
// [agentless]   无代理备份主机
//               - 仅存在于 [virt] 平台
//               - 通过虚拟化/云平台 API 获取数据
//               - 无需在客户操作系统内安装代理
//
// [agent-based] 有代理备份主机
//               - 适用于任意平台（virt / bare-metal）
//               - 需在操作系统内安装代理程序
//               - 由代理负责数据采集与传输
//
//
//
// =========================
// 恢复类型（Recovery Type）
// =========================
//
// [homogeneous]   同构恢复（Homogeneous Recovery）
//                 - 目标平台与源平台类型一致
//
// [heterogeneous] 异构恢复（Heterogeneous Recovery）
//                 - 目标平台与源平台类型不一致
//                 - 细分为：
//                     * [cross-cloud]     跨云恢复（virt → 不同 virt）
//                     * [to-cloud]        上云恢复（bare-metal → virt）
//                     * [to-bare-metal]   物理恢复（virt → bare-metal / 跨物理机）
//
//
//
// =========================
// 用户恢复操作映射（Recovery Scenarios）
// =========================
//
// 一、恢复 [agentless]
//
//   virt → 相同 virt 平台
//       = homogeneous（同构恢复）
//
//   virt → 不同 virt 平台
//       = heterogeneous / cross-cloud（跨云恢复）
//
//   virt → bare-metal
//       = heterogeneous / to-physical（物理恢复，BMR）
//
//
//
// 二、恢复 [agent-based]
//
//   bare-metal → 相同硬件/兼容环境
//       = homogeneous（同构恢复）
//
//   bare-metal → 不同硬件
//       = heterogeneous / to-physical（物理恢复 / 硬件适配恢复）
//
//   bare-metal → virt
//       = heterogeneous / to-cloud（上云恢复，P2V）
//
//

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
