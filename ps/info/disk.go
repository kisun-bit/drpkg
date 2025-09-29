package info

type Purpose int16

const (
	PurposeUnknown Purpose = iota
	PurposeLVM             // 表明此磁盘是一个PV
	PurposeVolume          // 表明此磁盘是一个包含文件系统的卷
	PurposeDisk            // 表明此磁盘是一个包含分区表的磁盘
)

type PartitionTableType string

const (
	UnknownLabel PartitionTableType = "unknown"
	MBR          PartitionTableType = "mbr"
	GPT          PartitionTableType = "gpt"
)

type Disk struct {
	// Name 磁盘名
	Name string `json:"name"`
	// Device 设备路径
	Device string `json:"device"`
	// GUID 全局唯一ID
	// 计算规则: TODO 如何计算GUID
	GUID string `json:"guid"`
	// Size 磁盘大小（单位：字节）
	Size int64 `json:"size"`
	// SectorSize 物理扇区大小（单位：字节）
	SectorSize int `json:"sectorSize"`
	// SerialNumber 硬件序列号
	SerialNumber string `json:"serialNumber"`
	// Purpose 用途
	Purpose Purpose `json:"purpose"`
	// IsOnline 是否已联机
	IsOnline bool `json:"isOnline"`
	// IsMsDynamic 是否为Windows动态磁盘
	IsMsDynamic bool `json:"isMsDynamic"`
	// IsReadOnly 是否只读
	IsReadOnly bool `json:"isReadOnly"`
}

type PartitionTable struct {
	// Device 设备路径
	Device string `json:"device"`
	// Type 分区表类型
	Type PartitionTableType `json:"type"`
	// Identifier 分区表唯一ID
	Identifier string `json:"identifier"`
}
