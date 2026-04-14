package recovery

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

// BackupSourceType 备份主机类型
type BackupSourceType string

const (
	BSTAgentless  = "agentless"   // 基于无代理备份的主机
	BSTAgentBased = "agent-based" // 基于有代理备份的主机
)

// RecoveryOperation 恢复类型
type RecoveryOperation string

const (
	ROHomogeneous   RecoveryOperation = "homogeneous"   // 同构恢复
	ROHeterogeneous RecoveryOperation = "heterogeneous" // 异构恢复
)

// ROHeterogeneousType 异构恢复类型
type ROHeterogeneousType string

const (
	ROHTUnknown     ROHeterogeneousType = "unknown"
	ROHTCrossVirt   ROHeterogeneousType = "cross-virt"    // 跨云恢复
	ROHTToVirt      ROHeterogeneousType = "to-virt"       // 上云恢复
	ROHTToBareMetal ROHeterogeneousType = "to-bare-metal" // 物理恢复
)

// RecoveryParameter 恢复参数
// 接下来对恢复场景的参数做示例：
//
//   - 虚拟机 -> 虚拟机
//     {
//     "source": {
//     "base": "virt",
//     "virt": "vmware"
//     },
//     "target": {
//     "base": "virt",
//     "virt": "qemu/kvm"
//     }
//     }
//
//   - 虚拟机 -> 裸机
//     {
//     "source": {
//     "base": "virt",
//     "virt": "vmware"
//     },
//     "target": {
//     "base": "bare-metal",
//     "pciList": ["PCI\V8086\D1d02\SV1028\SD04ce\BC01\SC06\I01\REV05", "PCI\V14e4\D165f\SV1028\SD1f5b\BC02\SC00\I00\REV00"]
//     }
//     }
//
//   - 裸机 -> 虚拟机
//     {
//     "source": {
//     "base": "bare-metal",
//     "pciList": ["PCI\V8086\D1d02\SV1028\SD04ce\BC01\SC06\I01\REV05", "PCI\V14e4\D165f\SV1028\SD1f5b\BC02\SC00\I00\REV00"]
//     },
//     "target": {
//     "base": "virt",
//     "virt": "vmware"
//     }
//     }
//
//   - 裸机 -> 裸机
//     {
//     "source": {
//     "base": "bare-metal",
//     "pciList": ["PCI\V8086\D1d02\SV1028\SD04ce\BC01\SC06\I01\REV05", "PCI\V14e4\D165f\SV1028\SD1f5b\BC02\SC00\I00\REV00"]
//     },
//     "target": {
//     "base": "bare-metal",
//     "pciList": ["PCI\V8086\D1d02\SV1028\SD04ce\BC01\SC06\I01\REV05", "PCI\V14e4\D165f\SV1028\SD1f5b\BC02\SC00\I00\REV00"]
//     }
//     }
type RecoveryParameter struct {
	Source Platform `json:"sourcePlatform"`
	Target Platform `json:"targetPlatform"`
}

type RecoveryManager struct {
	Parameter RecoveryParameter `json:"parameter"`
}
