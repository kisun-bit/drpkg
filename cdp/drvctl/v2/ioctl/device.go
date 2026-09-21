package ioctl

import biotrkmeta "github.com/kisun-bit/drpkg/cdp/drvctl/v2/meta"

// IDsFromMeta 将 biotrkmeta.ID 切片编码为扁平字节序列。
//
// 返回值 data 为所有 ID 首尾相接的字节序列，每个 ID 占 biotrkmeta.DeviceIDLen 字节。
func IDsFromMeta(ids []biotrkmeta.ID) (data []byte) {
	data = make([]byte, len(ids)*biotrkmeta.DeviceIDLen)
	for i, id := range ids {
		copy(data[i*biotrkmeta.DeviceIDLen:], id[:])
	}
	return data
}

// IDsToMeta 将驱动通信用的扁平字节序列解码为 biotrkmeta.ID 切片。
//
// data 为所有 ID 首尾相接的字节序列。若 data 长度不是 DeviceIDLen 的整数倍，
// 尾部不足一整个 ID 的字节会被忽略。
func IDsToMeta(data []byte) []biotrkmeta.ID {
	count := len(data) / biotrkmeta.DeviceIDLen
	result := make([]biotrkmeta.ID, count)
	for i := range result {
		copy(result[i][:], data[i*biotrkmeta.DeviceIDLen:])
	}
	return result
}

//
// CDP 受保护设备管理：
// * 列举已保护的磁盘（从驱动内存中查询得到）
// * 增加部分保护磁盘
// * 移除部分保护磁盘
//

// ListProtectedDevices 表示受保护设备列表的响应。
type ListProtectedDevices struct {
	// DevicesLen 为受保护设备列表的元素数量（响应参数）。
	DevicesLen uint32 `struc:"sizeof=Devices"`

	// Devices 为受保护设备列表（响应参数）。
	Devices []biotrkmeta.ProtectedDevice
}

// AddProtectedDevicesRequest 表示批量新增受保护设备的请求。
type AddProtectedDevicesRequest struct {
	// DevicesLen 为 Devices 的元素数量。
	DevicesLen uint32 `struc:"sizeof=Devices"`

	// Devices 为待新增的受保护设备列表。
	//
	// 该列表由 biotrkmeta 逐一调用 AddProtectDevice，并最终 Flush 后，
	// 再通过 ListValidProtectDevice 筛选得到。
	// 驱动拿到此值后，会更新（追加Devices）维护在内存中的 ProtectedDevice 列表。
	Devices []biotrkmeta.ProtectedDevice

	// NewMetadataExtentsLen 为 NewMetadataExtents 的元素数量。
	NewMetadataExtentsLen uint32 `struc:"sizeof=NewMetadataExtents"`

	// NewMetadataExtents 新增受保护设备后，元数据区域新的物理分布信息。
	//
	// 该值由 biotrkmeta 的 Flush 或 PhysicalExtents 得到。
	NewMetadataExtents []biotrkmeta.DiskExtent
}

// RemoveProtectedDevicesRequest 表示批量移除受保护设备的请求。
//
// 线缆格式：DiskIDsLen + DiskIDs + DeviceIDsLen + DeviceIDs + NewMetadataExtentsLen + NewMetadataExtents。
// DiskIDsLen / DeviceIDsLen / NewMetadataExtentsLen 均为对应字段的元素个数。
type RemoveProtectedDevicesRequest struct {
	// DiskIDsLen 为 DiskIDs 的元素个数。
	DiskIDsLen uint32 `struc:"sizeof=DiskIDs"`

	// DiskIDs 为待删除的磁盘 ID 列表，每个 ID 占 DeviceIDLen 字节。
	// 驱动会基于此字段去删除维护在内存中的磁盘位图物理分布信息。
	DiskIDs []biotrkmeta.ID

	// DeviceIDsLen 为 DeviceIDs 的元素个数。
	DeviceIDsLen uint32 `struc:"sizeof=DeviceIDs"`

	// DeviceIDs 为待删除的设备 ID 列表，每个 ID 占 DeviceIDLen 字节。
	// 驱动基于此字段更新维护在内存中的受保护设备信息。
	DeviceIDs []biotrkmeta.ID

	// NewMetadataExtentsLen 为 NewMetadataExtents 的元素数量。
	NewMetadataExtentsLen uint32 `struc:"sizeof=NewMetadataExtents"`

	// NewMetadataExtents 删除受保护设备后，元数据区域新的物理分布信息。
	//
	// 该值由 biotrkmeta 的 Flush 或 PhysicalExtents 得到。
	NewMetadataExtents []biotrkmeta.DiskExtent
}
