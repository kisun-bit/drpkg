package x2xcore

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

type Disk struct {
	// Index 磁盘索引编号
	Index int `json:"index"`

	// Path 磁盘设备路径或镜像文件路径
	Path string `json:"path"`

	// LBA 逻辑块地址（Logical Block Address）
	LBA int64 `json:"lba"`

	// PBA 物理块地址（Physical Block Address）
	PBA int64 `json:"pba"`

	// Size 磁盘大小，单位：字节
	Size int64 `json:"size"`
}

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
	// OfflineSystemDisks 离线系统磁盘列表。
	// 用于描述待修复系统所在的磁盘，支持系统盘、数据盘等多个磁盘。
	OfflineSystemDisks []Disk `json:"offlineSystemDisks"`

	// Source 源主机的硬件平台配置。
	Source Platform `json:"sourcePlatform"`

	// Target 目标主机（恢复后）的硬件平台配置。
	Target Platform `json:"targetPlatform"`

	//
	// 修复配置
	//

	// TimeoutSeconds 修复操作超时时间，单位：秒。
	TimeoutSeconds int `json:"timeoutSeconds"`

	// OSType 目标系统的操作系统类型。
	OSType string `json:"osType"`

	// X2xLibrary 驱动修复所使用的驱动库路径。
	X2xLibrary string `json:"x2xLibrary"`

	// FsckFs 是否强制执行文件系统检查与修复。
	// 为 true 时，对相关文件系统执行强制修复，可能增加修复耗时。
	FsckFs bool `json:"fsckFs"`

	// SkipDriverRepairIfPlatformUnchanged 硬件平台未发生变化时，是否跳过驱动修复。
	SkipDriverRepairIfPlatformUnchanged bool `json:"skipDriverRepairIfPlatformUnchanged"`

	//
	// 源系统配置
	//

	// SourceDeviceMap 源系统设备映射列表。
	// 用于描述源系统设备与目标系统设备之间的对应关系。
	SourceDeviceMap []DeviceMap `json:"sourceDeviceMap"`

	// LuksGlobalPassword 源系统 LUKS 加密卷的全局解锁密码。
	LuksGlobalPassword string `json:"luksGlobalPassword"`

	// BitlockerGlobalRecoveryKey 源系统 BitLocker 加密卷的全局恢复密钥。
	// 恢复密钥为 48 位数字，格式为 8 组由短横线分隔的 6 位数字。
	// 用于修复阶段自动解锁源系统中的 BitLocker 加密卷。
	// 不支持使用智能卡等非恢复密钥方式解锁的 BitLocker 卷。
	BitlockerGlobalRecoveryKey string `json:"bitlockerGlobalRecoveryKey"`

	//
	// 目标系统配置
	//

	// NetworkConfigType 目标系统恢复后的网络配置策略。
	// 0 - 自动配置：不修改网络配置，由操作系统自行配置。
	// 1 - 保留原配置：沿用源系统原有网络配置（暂未支持）。
	// 2 - 自定义配置：使用 Network 字段指定目标系统网络配置。
	NetworkConfigType int `json:"networkConfigType"`

	// Network 目标系统恢复后的网络配置。
	// 当 NetworkConfigType 为 0 时，忽略此字段。
	// 当 NetworkConfigType 为 1 时，由程序根据源系统网络配置自动填充。
	// 当 NetworkConfigType 为 2 时，使用用户指定的网络配置。
	Network NetworkConfig `json:"network"`

	// RaidNotExisted 目标系统中是否不存在源系统的 RAID 设备。
	// 对整机定时任务或整机 CDP 代理备份的数据执行修复时，应设置为 true。
	RaidNotExisted bool `json:"raidNotExisted"`

	// MultipathNotExisted 目标系统中是否不存在源系统的多路径设备。
	// 对整机定时任务或整机 CDP 代理备份的数据执行修复时，应设置为 true。
	MultipathNotExisted bool `json:"multipathNotExisted"`
}

type DeviceMap struct {
	Origin     string // 原机的设备名，如：/dev/sda1、/dev/nvme0n1等
	Mountpoint string // 原机的挂载点，如：/、/boot等
	UUID       string // 设备UUID
}
