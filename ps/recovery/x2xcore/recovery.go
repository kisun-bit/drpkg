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
	// OfflineSystemDisks 离线系统磁盘列表。
	// 支持 qcow2 等虚拟磁盘镜像文件及宿主机本地块设备。
	// 修复过程中，这些磁盘将挂载到修复虚拟机，供修复程序访问和修改。
	OfflineSystemDisks []Disk `json:"offlineSystemDisks"`

	// Source 源主机的硬件平台信息。
	Source Platform `json:"sourcePlatform"`

	// Target 目标主机的硬件平台信息。
	Target Platform `json:"targetPlatform"`

	//
	// 修复配置
	//

	// TimeoutSeconds 修复操作超时时间，单位：秒。
	TimeoutSeconds int `json:"timeoutSeconds"`

	// OSType 目标操作系统类型，例如 linux、windows。
	OSType string `json:"osType"`

	// X2xLibrary 驱动修复使用的驱动库路径。
	// 该字段由程序自动扫描生成，无需用户指定。
	X2xLibrary string `json:"x2xLibrary"`

	// FsckFs 是否强制执行文件系统检查与修复。
	// 启用后可修复部分文件系统问题，但会增加修复耗时。
	FsckFs bool `json:"fsckFs"`

	// SkipDriverRepairIfPlatformUnchanged 硬件平台未发生变化时是否跳过驱动修复。
	SkipDriverRepairIfPlatformUnchanged bool `json:"skipDriverRepairIfPlatformUnchanged"`

	//
	// 源系统配置
	//

	// KernelVersion 源系统内核版本。
	// 修复过程中会根据该版本匹配修复虚拟机资源，优先选择内核版本最接近的修复环境。
	KernelVersion string `json:"kernelVersion"`

	// SourceDeviceMap 源系统设备映射关系。
	// 描述源系统设备与目标系统设备的对应关系，仅 Linux 系统使用。
	SourceDeviceMap []DeviceMap `json:"sourceDeviceMap"`

	// LuksGlobalPassword 源系统 LUKS 加密卷的全局解锁密码，仅 Linux 系统使用。
	LuksGlobalPassword string `json:"luksGlobalPassword"`

	// BitlockerGlobalRecoveryKey 源系统 BitLocker 加密卷的全局恢复密钥，仅 Windows 系统使用。
	// 恢复密钥为 48 位数字，由 8 组 6 位数字组成，以短横线分隔。
	// 修复阶段将使用该密钥自动解锁 BitLocker 加密卷。
	// 不支持智能卡等非恢复密钥解锁方式。
	BitlockerGlobalRecoveryKey string `json:"bitlockerGlobalRecoveryKey"`

	//
	// 目标系统配置
	//

	// NetworkConfigType 目标系统恢复后的网络配置策略。
	//   0 - 自动配置：不修改网络配置，由操作系统自行处理。
	//   1 - 保留原配置：保留源系统网络配置（暂未支持）。
	//   2 - 自定义配置：使用 Network 指定网络配置。
	NetworkConfigType int `json:"networkConfigType"`

	// Network 目标系统恢复后的网络配置。
	// 当 NetworkConfigType 为 2 时使用用户指定配置；
	// 为 1 时由程序根据源系统网络配置生成；
	// 为 0 时忽略该字段。
	Network NetworkConfig `json:"network"`

	// RaidNotExisted 表示目标系统不存在源系统的 RAID 设备。
	// 对整机备份或整机 CDP 代理备份恢复时通常应设置为 true。
	RaidNotExisted bool `json:"raidNotExisted"`

	// MultipathNotExisted 表示目标系统不存在源系统的多路径设备。
	// 对整机备份或整机 CDP 代理备份恢复时通常应设置为 true。
	MultipathNotExisted bool `json:"multipathNotExisted"`
}

type DeviceMap struct {
	Origin     string // 原机的设备名，如：/dev/sda1、/dev/nvme0n1等
	Mountpoint string // 原机的挂载点，如：/、/boot等
	UUID       string // 设备UUID
}
