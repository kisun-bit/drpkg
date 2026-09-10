package journal

import (
	"github.com/kisun-bit/drpkg/cdp/drvctl/v2/ioctl"
	biotrkmeta "github.com/kisun-bit/drpkg/cdp/drvctl/v2/meta"
	"github.com/kisun-bit/drpkg/xutil"
)

// ===========================================================================
// 受保护区域分类
// ===========================================================================

// classifyShmIO 将一条共享内存 IO 归类到它所属的受保护设备。
//
// 对于每个 ProtectedDevice（磁盘或卷），检查其 Extents 中是否存在
// 与 IO 的物理磁盘位置重叠的区域。返回第一个匹配的设备 ID。
//
// 返回值：
//   - deviceID: IO 所属的受保护设备标识符字符串；空字符串表示不属于任何受保护设备。
func classifyShmIO(devices []biotrkmeta.ProtectedDevice, io *ioctl.IoRecord) string {
	if io == nil || len(devices) == 0 {
		return ""
	}

	diskID := io.Header.DiskID
	off := io.Header.Offset
	length := io.Header.Length

	for _, dev := range devices {
		if deviceContainsRange(dev, diskID, off, length) {
			return deviceIDToString(dev)
		}
	}

	return ""
}

// deviceIDToString 将 ProtectedDevice 的 DeviceID 转为可读字符串。
func deviceIDToString(dev biotrkmeta.ProtectedDevice) string {
	switch dev.Type {
	case biotrkmeta.DeviceTypeDisk:
		// 磁盘设备：DeviceID 即为 DiskID.ID
		var dkID biotrkmeta.DiskID
		copy(dkID.ID[:], dev.DeviceID[:])
		return dkID.String()
	default:
		return xutil.TrimZeroString(dev.DeviceID[:])
	}
}

// deviceContainsRange 检查 ProtectedDevice 的 Extents 是否包含指定的磁盘范围。
//
// 仅检查 Extent.DiskID 与给定 diskID 匹配的 Extent，判断 [off, off+length)
// 是否完全位于该 Extent 之内。
func deviceContainsRange(dev biotrkmeta.ProtectedDevice, diskID biotrkmeta.DiskID, off, length uint64) bool {
	if length == 0 {
		return false
	}

	for _, ext := range dev.Extents {
		if !diskIDEqual(ext.Extent.DiskID, diskID) {
			continue
		}
		if isRangeInExtent(ext.Extent, off, length) {
			return true
		}
	}

	return false
}

// isRangeInExtent 判断 [off, off+length) 是否完全位于 extent 之内。
func isRangeInExtent(extent biotrkmeta.DiskExtent, off, length uint64) bool {
	if length == 0 || extent.Size == 0 {
		return false
	}
	// 溢出保护
	if off > ^uint64(0)-length {
		return false
	}
	end := off + length
	if extent.Start > ^uint64(0)-extent.Size {
		return false
	}
	extEnd := extent.Start + extent.Size
	return off >= extent.Start && end <= extEnd
}

// partitionHeadersBelongToDevice 将分区表头数据归类到所属的受保护设备。
//
// headers 为从磁盘读取的分区表数据（MBR/GPT 头），每个 header 包含其在磁盘上的偏移。
// 对于每个 header，检查是否有 ProtectedDevice 的 Extent 包含它。
// 返回 (设备ID字符串 → 分区表数据列表) 的映射。
func partitionHeadersBelongToDevice(
	devices []biotrkmeta.ProtectedDevice,
	diskID biotrkmeta.DiskID,
	headers []biotrkmeta.DiskPartTableData,
) map[string][]biotrkmeta.DiskPartTableData {
	result := make(map[string][]biotrkmeta.DiskPartTableData)

	for _, h := range headers {
		start := uint64(h.Offset)
		size := uint64(len(h.Data))
		if size == 0 {
			continue
		}

		for _, dev := range devices {
			if deviceContainsRange(dev, diskID, start, size) {
				key := deviceIDToString(dev)
				result[key] = append(result[key], h)
			}
		}
	}

	return result
}

// ===========================================================================
// DiskID 比较
// ===========================================================================

// diskIDEqual 基于 ID 字节比较两个 DiskID。
func diskIDEqual(a, b biotrkmeta.DiskID) bool {
	return a.Equal(&b)
}

// ===========================================================================
// 受保护区域提取
// ===========================================================================

// protectedExtentsForDisk 从 ProtectedDevice 列表中提取指定物理磁盘上的所有受保护 Extent。
//
// 同时考虑磁盘级和卷级 ProtectedDevice：只要 Extent.DiskID 匹配 diskID，即纳入结果。
func protectedExtentsForDisk(devices []biotrkmeta.ProtectedDevice, diskID biotrkmeta.DiskID) []biotrkmeta.DiskExtent {
	var result []biotrkmeta.DiskExtent
	for _, dev := range devices {
		for _, ext := range dev.Extents {
			if diskIDEqual(ext.Extent.DiskID, diskID) {
				result = append(result, ext.Extent)
			}
		}
	}
	return result
}

// isBlockProtected 判断磁盘区域是否被任一受保护 Extent 完全包含。
func isBlockProtected(
	devices []biotrkmeta.ProtectedDevice,
	diskID biotrkmeta.DiskID,
	start, size uint64,
) bool {
	for _, dev := range devices {
		if deviceContainsRange(dev, diskID, start, size) {
			return true
		}
	}
	return false
}
