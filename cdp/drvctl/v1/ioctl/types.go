package ioctl

// 该模块用于实现"驱动<--->应用层"之间的通信协议
// 从应用层对驱动的调用而言，分为了如下接口：

// - 接口 请求参数  返回值

// - 启动                   DRVReqStart               ErrCode
// - 增加保护磁盘            DRVReqAdd                  ErrCode
// - 移除部分保护磁盘         DRVReqReduce              ErrCode
// - 删除策略               无                         ErrCode
// - 切换定时模式            无                         ErrCode（已弃用，删除共享缓存且存在cdp任务时自动切换定时模式）
// - 设置一致性标记（切换实时） 无                        ErrCode
// - 状态查询               DRVReqGetStatus            ErrCode
// - 清理位图	           DRVReqClearBitmap         ErrCode
// - 查询位图大小            DRVReqSelectBitmap        ErrCode
// - 获取位图               DRVReqGetBitmap            ErrCode
// - 创建共享缓存            DRVReqCreateRingBuffer    ErrCode
// - 删除共享缓存            无                         ErrCode
// - 设置日志事件            DRVReqSetLogEvent         ErrCode
// - 获取日志               待定                       ErrCode

type FixedName [MaxnameLen]byte

// ///////////////////////////////////////////////////////////////////
// type
type DiskGuid struct {
	Major  uint32    `struc:"little" json:"major"` // 仅Linux平台有效
	Minor  uint32    `struc:"little" json:"minor"` // 仅Linux平台有效
	Name   FixedName `struc:"little" json:"name"`  // 仅Linux平台有效
	DiskId uint32    `struc:"little" json:"disk_id"`
}

func (d *DiskGuid) String() string {
	if path, _ := GetDiskPathByGuid(*d); path != "" {
		return path
	}
	return "DiskGuid(Unknown)"
}

type ProtectDisk struct {
	// 磁盘id
	Guid DiskGuid `struc:"little" json:"disk_guid"`

	// 磁盘大小
	Size uint64 `struc:"little" json:"size"`

	// 磁盘受保护区域个数
	ProtectRegionsLen uint32 `struc:"little,sizeof=ProtectRegions" json:"protect_regions_len"`

	// 磁盘的受保护区域
	ProtectRegions []Segment `struc:"little" json:"protect_regions"`

	// 磁盘的位图区域个数。仅Linux平台有效
	DiskBitmapLen uint32 `struc:"little,sizeof=DiskBitmap" json:"disk_bitmap_len"`

	// 磁盘的位图区域。仅Linux平台有效
	DiskBitmap []Segment `struc:"little" json:"disk_bitmaps"`
}

type Segment struct {
	Start uint64 `struc:"little" json:"start"`
	Size  uint64 `struc:"little" json:"size"`
}

type MetadataRegions struct {
	Count   uint32           `struc:"little,sizeof=Regions"`
	Regions []MetadataRegion `struc:"little"`
}

type MetadataRegion struct {
	DiskId uint32 `struc:"little"`
	Start  uint64 `struc:"little"`
	Size   uint64 `struc:"little"`
}

/////////////////////////////////////////////////////////////////////
// request

// DRVReqStart 启动参数 以bin格式下发
type DRVReqStart struct {
	// 元数据的所处磁盘   元数据分为 [驱动私有数据] 以及 [各保护磁盘的位图数据]
	MetadataDiskGuid DiskGuid `json:"metadata_disk_guid"`

	// 驱动私有数据存储位置，即元数据头部区域在MetadataDiskGuid所处的位置
	PrivateSegments Segment `json:"private_segments"`

	// 待保护磁盘的数量
	ProtectDisksLen uint32 `struc:"sizeof=ProtectDisks" json:"protect_disks_len"`

	// 待保护磁盘列表
	ProtectDisks []ProtectDisk `json:"protect_disks"`

	// 驱动工作模式 1 性能优先  2 数据优先
	PerfPriority uint8 `json:"perf_priority"`

	// 共享内存大小
	ShareMemorySize uint64 `json:"share_memory_size"`
}

// DRVReqAdd 添加磁盘参数 以bin格式下发
type DRVReqAdd struct {
	// 待保护磁盘的数量
	ProtectDisksLen uint32 `struc:"sizeof=ProtectDisks"`

	// 待保护磁盘列表
	ProtectDisks []ProtectDisk
}

// DRVReqReduce 移除某些磁盘的保护
type DRVReqReduce struct {
	// 要移除的磁盘的个数
	ReduceDisksLen uint32 `struc:"sizeof=DiskGuid"`

	// 磁盘的id
	DiskGuid []DiskGuid
}

// DRVReqGetStatus 状态查询
type DRVReqGetStatus struct {
	// 状态  0 空闲    1 实时    2 定时   3 错误
	Status int32

	// 错误状态码
	ErrStatusCode uint64 // FIXME: 新增字段，待Linux驱动确认

	// 错误码
	ErrCode int32
}

// DRVReqClearBitmap 清理位图
type DRVReqClearBitmap struct {
	// 1 实时io   2 位图io
	IoType uint8

	// 磁盘ID
	Guid DiskGuid

	// 要清理的区域个数
	ClearBitmapLen uint64 `struc:"sizeof=ClearBitmap"`

	// 要清理的区域
	ClearBitmap []Segment
}

// DRVReqSelectBitmap 查询位图大小
type DRVReqSelectBitmap struct {
	// 磁盘的id
	MetadataDiskGuid DiskGuid

	// 位图大小
	BitmapSize uint64
}

// DRVReqGetBitmap 获取位图数据
type DRVReqGetBitmap struct {
	// 磁盘的id
	MetadataDiskGuid DiskGuid

	// 要获取的起始字节
	StartBytes uint64

	// 要获取的长度
	GetBitmapLen uint64

	// BitmapBuffer的有效字节数
	BufferSize uint64 `struc:"sizeof=BitmapBuffer"`

	// 位图数据
	BitmapBuffer []byte
}

// DRVReqCreateRingBuffer 创建共享缓存
type DRVReqCreateRingBuffer struct {
	EventFd    uint64
	SHMAddress uint64
}

type DRVReqSetLogEvent struct {
	// 事件的文件描述符
	Handle uint64
}

type DRVReqGetLog struct {
	LogLevel  uint32
	TimeStamp uint64
	BufferLen uint32
	Buffer    [512]byte
}

type DRVReqGetErrStr struct {
	ErrCode int32
	ErrStr  [256]byte
}

// ////////////////////////////////
// 测试 ioctl
type DRVReqSelect struct {
	// 磁盘的id
	DiskGuid DiskGuid
}

// ///////////////////////////////////////////////////////////////////
// reply
type DRVRespGetStatus = DRVReqGetStatus

type DRVRespSelectBitmap = DRVReqSelectBitmap

type DRVRespGetBitmap = DRVReqGetBitmap

type DRVRespGetLog = DRVReqGetLog

type DRVRespGetErrStr = DRVReqGetErrStr
