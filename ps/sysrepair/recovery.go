package sysrepair

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

func (rp *RecoveryParameter) Check() error {
	// TODO
	return nil
}

func (rp *RecoveryParameter) RecoveryOperation() RecoveryOperation {
	// TODO
	return ""
}

func (rp *RecoveryParameter) ROHeterogeneousType() RecoveryOperation {
	// TODO
	return ""
}
