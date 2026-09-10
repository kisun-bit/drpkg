package ioctl

import biotrkmeta "github.com/kisun-bit/drpkg/cdp/drvctl/v2/meta"

//
// CDP 任务管理：
// * 启动任务
// * 释放（删除）任务，除任务外，驱动会连带删除共享内存
// * 查询任务状态
// * 设置任务一致性标记，切换为CDP模式（实时模式）
// * 获取错误字符串
//

// StartTaskRequest 表示启动 CDP 任务的请求。
//
// 驱动收到请求后，会根据元数据区域信息读取并解析元数据，
// 将其中的 ProtectedDevice 列表（不能为0）缓存到内存中，用于 I/O 过滤和位图持久化。
type StartTaskRequest struct {
	// MetadataFile 为元数据文件路径。
	MetadataFile [512]byte

	// MetadataOffset 为元数据区域在元数据文件中的起始偏移（字节）。
	MetadataOffset uint64

	// MetadataExtentsLen 为 MetadataExtents 的元素数量。
	MetadataExtentsLen uint32 `struc:"sizeof=MetadataExtents"`

	// MetadataExtents 为元数据区域对应的物理磁盘区间。
	//
	// 驱动会根据这些物理区间读取完整的元数据区域，并将其写入注册表，
	// 以便系统重启后无需依赖文件系统即可恢复元数据。
	MetadataExtents []biotrkmeta.DiskExtent
}

// TaskStatus 表示 CDP 任务状态的请求与响应。
type TaskStatus struct {
	// Status 为当前 CDP 任务状态。
	Status biotrkmeta.CDPStatus

	// ErrorCode 为任务错误码。
	//
	// 仅当 Status 为 biotrkmeta.CDPStatusError 时有效。
	// - 0：正常
	// - 1：异常关机
	// - 其他：驱动自定义错误码，可用于查询对应的错误信息。
	ErrorCode uint64
}

// TaskErrorString 表示任务错误字符串的请求与响应。
type TaskErrorString struct {
	// ErrCode 为任务错误码（请求参数）。
	ErrCode uint64

	// ErrStr 为错误码对应的错误描述字符串（响应参数）。
	ErrStr [512]byte
}
