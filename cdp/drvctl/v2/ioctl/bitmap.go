package ioctl

import (
	biotrkmeta "github.com/kisun-bit/drpkg/cdp/drvctl/v2/meta"
)

//
// CDP 磁盘位图管理：
// * 获取位图详情
// * 获取位图数据
// * 清理位图引用
//

// BitmapDetail 表示获取磁盘位图详情的请求与响应。
type BitmapDetail struct {
	// DiskID 为目标磁盘 ID（请求参数）。
	DiskID biotrkmeta.DiskID

	// BitmapSize 为位图大小（字节）（响应参数）。
	BitmapSize uint32
}

// ClearBitmapReferenceRequest 表示清理磁盘位图引用的请求。
type ClearBitmapReferenceRequest struct {
	// RecordType 为位图记录类型。
	RecordType biotrkmeta.RecordType

	// DiskID 为目标磁盘 ID。
	DiskID biotrkmeta.DiskID

	// SegmentsLen 为 Segments 的元素数量。
	SegmentsLen uint32 `struc:"sizeof=Segments"`

	// Segments 为需要清理位图引用的磁盘区域列表。
	Segments []biotrkmeta.Segment
}

// BitmapData 表示获取磁盘位图数据的请求与响应。
type BitmapData struct {
	// DiskID 为目标磁盘 ID（请求参数）。
	DiskID biotrkmeta.DiskID

	// Offset 为要获取的起始字节偏移（请求参数）。
	Offset uint32

	// Length 为要获取的位图数据长度（请求参数）。
	Length uint32

	// DataSize 为实际返回的位图数据大小（响应参数）。
	DataSize uint32 `struc:"sizeof=Data"`

	// Data 为实际返回的位图数据（响应参数）。
	Data []byte
}
