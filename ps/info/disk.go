package info

type DiskRoleType string

const (
	DiskRoleUnknown DiskRoleType = "unknown"
	DiskRoleLVM     DiskRoleType = "lvm"    // 表明此磁盘是一个PV
	DiskRoleVolume  DiskRoleType = "volume" // 表明此磁盘是一个包含文件系统的卷
	DiskRoleDisk    DiskRoleType = "disk"   // 表明此磁盘是一个包含分区表的磁盘
)

type DiskTableType string

const (
	DiskTableUnknown DiskTableType = "unknown"
	DiskTableMBR     DiskTableType = "mbr"
	DiskTableGPT     DiskTableType = "gpt"
)

type Disk struct {
	// Name 磁盘名
	Name string `json:"name"`
	// Device 设备路径
	Device string `json:"device"`
	// GUID 全局唯一ID
	// 计算规则: TODO 如何计算GUID
	GUID string `json:"guid"`
	// Sectors 磁盘大小（单位：字节）
	Sectors int64 `json:"size"`
	// SectorSize 物理扇区大小（单位：字节）
	SectorSize int `json:"sectorSize"`
	// Vendor 制造商
	Vendor string `json:"vendor"`
	// Model 产品型号
	Model string `json:"model"`
	// SerialNumber 硬件序列号（注意：可能为空）
	SerialNumber string `json:"serialNumber"`
	// Role 角色
	Role DiskRoleType `json:"purpose"`
	// IsOnline 是否已联机
	IsOnline bool `json:"isOnline"`
	// IsMsDynamic 是否为Windows动态磁盘
	IsMsDynamic bool `json:"isMsDynamic"`
	// IsReadOnly 是否只读
	IsReadOnly bool `json:"isReadOnly"`
}

type DiskTable struct {
	// Device 设备路径
	Device string `json:"device"`
	// Type 分区表类型
	Type DiskTableType `json:"type"`
	// Identifier 分区表唯一ID
	Identifier string `json:"identifier"`
	// Partitions 分区表项集合
	Partitions []DiskPartitionTable `json:"partitions"`
}

type DiskPartitionTable struct {
	// Type 分区表项的类型
	Type string `json:"type"`
	// Start 起始字节
	Start int `json:"start"`
	// Size 总大小
	Size int `json:"size"`
}
