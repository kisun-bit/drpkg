package info

type Volume struct {
	// Name 卷的显示名称
	// Windows: "数据卷 (D:)"
	// Linux: "/dev/sda1" 或 "/dev/mapper/rl-root"
	Name string `json:"name"`

	// Segments 卷的数据分布在哪些磁盘区间
	Segments []Segment `json:"segments"`

	// MountPoint 卷的挂载点
	// Windows: 使用卷的装入点，如："C:"
	// Linux: 使用设备的挂载点"/home"
	MountPoint string `json:"mountpoint"`

	// UUID 设备唯一标识符
	// Windows: 取自卷名（如：\\?\Volume{e3b9397c-0000-0000-0000-100000000000}\）中的GUID
	// Linux：取自 blkid 输出的UUID
	UUID string `json:"uuid"`

	// Filesystem 卷的文件系统类型
	Filesystem string `json:"filesystem"`

	UsageInfo

	// IsBootable 是否为启动卷
	// true 代表此卷与系统启动相关
	IsBootable bool `json:"isBootable"`
	// EnabledBitlocker 卷是否启用了BitLocker加密
	EnabledBitlocker bool `json:"isBitlocker"`

	// TODO 更多字段
}

// Segment 表示卷在物理磁盘上的连续数据区间
type Segment struct {
	Disk  string `json:"disk"`  // 磁盘路径，例如 "/dev/sda" 或 "\\.\PHYSICALDRIVE0"
	Start uint64 `json:"start"` // 起始偏移量
	Size  uint64 `json:"size"`  // 区间大小
}

// UsageInfo 存储空间使用信息
type UsageInfo struct {
	TotalBytes uint64 `json:"totalBytes"`
	UsedBytes  uint64 `json:"usedBytes"`
	AvailBytes uint64 `json:"availBytes"`
}
