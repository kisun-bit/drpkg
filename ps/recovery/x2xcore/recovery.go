package x2xcore

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

type RecoveryParameter struct {
	// OfflineSystemDisks 离线系统所在的磁盘列表。
	// 支持 qcow2 等虚拟磁盘镜像文件及宿主机本地块设备。
	// 修复过程中，这些磁盘将作为离线系统磁盘挂载至修复虚拟机，供修复程序访问和修改。
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

	// X2xLibrary 驱动修复所使用的驱动库路径。由程序自动扫描得到，无需用户填入。
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
