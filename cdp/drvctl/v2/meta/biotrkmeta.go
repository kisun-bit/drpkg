package meta

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"os"
	"strings"

	"github.com/kisun-bit/drpkg/xutil"
	"github.com/pkg/errors"
)

// Create 在文件的指定偏移处创建新的元数据文件。
//
// file 可以是普通文件路径，也可以是物理磁盘设备路径（如 \\.\physicaldrive0）。
// 传入设备路径时，元数据直接写入磁盘的 offset 偏移处，不会截断设备。
//
// offset 必须是 AlignSize（4096）的整数倍：所有区域起始偏移与区域大小都按 4096 对齐，
// 只有这样每个区域才能在裸设备上整体做扇区对齐读写。
//
// Create 在文件的指定偏移处创建新的元数据文件。
//
// Create 会把整个基础区域（Header + Allocation Map + Bitmap Unit Data）写零。
// 普通文件仅靠 Truncate 撑大时，NTFS 的有效数据长度（VDL）之外的簇通过文件 API
// 读出为 0，物理簇里却是旧数据；驱动是按物理偏移直读的，两者必须一致。
// 写零同时消除文件空洞，PhysicalExtents 才能覆盖整个区域。
//
// Protected Region 位于最后，初始大小为 0，随设备记录增减而变化。
//
// offset 必须是 AlignSize（4096）的整数倍。
// totalBitmapUnits 指定位图单元总数，传 0 则使用 DefaultTotalBitmapUnits（8192）。
// bitIndexSpace 指定每个 Bitmap Bit 对应的磁盘空间大小，传 0 则使用 DefaultBitIndexSpace（512 KiB）。
//
// 位图单元数量与 bitIndexSpace 决定了可索引的最大受保护磁盘空间（默认 512 KiB 时）：
//
//	总空间 = totalBitmapUnits × BitmapClusterSize × 8 × BitIndexSpace
//	       = totalBitmapUnits × 4096 × 8 × 512 KiB
//	       ≈ totalBitmapUnits × 16 GiB
//
// 调用方需要自行保证 offset + Size() 落在可用空间内：
// 记录区后面没有其他区域，增长不会破坏本格式的其他数据，
// 但在裸设备上可能超出调用方预留的范围。
func Create(file string, offset int64, totalBitmapUnits uint64, bitIndexSpace uint64) (*BioTrkMetadata, error) {
	if offset%AlignSize != 0 {
		return nil, errors.Errorf("offset %d is not aligned to %d", offset, AlignSize)
	}

	flags := xutil.MetaOpenFlags
	if !isDevicePath(file) {
		flags |= os.O_CREATE
	}
	f, err := os.OpenFile(file, flags, 0644)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create metadata file")
	}

	h := defaultHeader(totalBitmapUnits, bitIndexSpace)
	h.ProtectedRegionSize = 0
	h.ProtectedDeviceCount = 0
	h.HeaderCRC32 = calcCRC32(&h)

	// 基础区域整体写零
	//
	// 普通文件还要连 [0, offset) 一起写零：这段前缀如果留成文件空洞，
	// FSCTL_GET_RETRIEVAL_POINTERS 会对它返回 LCN=-1，PhysicalExtents 就会算出
	// 负偏移的错误物理段。裸设备上这段前缀属于别人的数据，绝对不能碰。
	zeroStart := offset
	if !isDevicePath(file) {
		zeroStart = 0
	}
	if err := zeroRegion(f, zeroStart, offset-zeroStart+h.TotalSize()); err != nil {
		f.Close()
		return nil, err
	}

	// 普通文件兜底：确保文件长度覆盖整个基础区域（裸设备上 Truncate 会失败，跳过）
	if !isDevicePath(file) {
		if err := f.Truncate(offset + h.TotalSize()); err != nil {
			f.Close()
			return nil, errors.Wrap(err, "failed to extend metadata file")
		}
	}

	if err := writeHeader(f, offset, &h); err != nil {
		f.Close()
		return nil, err
	}

	return &BioTrkMetadata{
		file:     f,
		filePath: file,
		offset:   offset,
		header:   h,
		devices:  nil,
		allocMap: make([]byte, h.BitmapAllocMapSize),
	}, nil
}

// Load 从文件的指定偏移处加载元数据文件。
//
// offset 必须与 Create 时一致，且必须是 AlignSize（4096）的整数倍。
func Load(file string, offset int64) (*BioTrkMetadata, error) {
	if offset%AlignSize != 0 {
		return nil, errors.Errorf("offset %d is not aligned to %d", offset, AlignSize)
	}

	f, err := os.OpenFile(file, xutil.MetaOpenFlags, 0644)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open metadata file")
	}

	// 读取 Header
	h, err := readHeader(f, offset)
	if err != nil {
		f.Close()
		return nil, err
	}

	// 校验 Header
	if err := validateHeader(f, offset, h); err != nil {
		f.Close()
		return nil, err
	}

	// 读取磁盘位图分配区域
	var diskBitmaps []DiskBitmap
	if h.DiskBitmapAllocRegionSize > 0 {
		region := make([]byte, h.DiskBitmapAllocRegionSize)
		if _, err := f.ReadAt(region, offset+int64(h.DiskBitmapAllocRegionOffset)); err != nil {
			f.Close()
			return nil, errors.Wrap(err, "failed to read disk bitmap region")
		}
		diskBitmaps, err = parseDiskBitmapRegion(region, h.DiskCount)
		if err != nil {
			f.Close()
			return nil, err
		}
	}

	// 读取 Protected Device 记录：整个区域一次对齐读入，再在内存里按 TotalSize 前缀顺序解析。
	// 变长记录的单条长度不保证扇区对齐，因此不能按条读（裸设备上会失败）。
	var devices []ProtectedDevice
	if h.ProtectedRegionSize > 0 {
		region := make([]byte, h.ProtectedRegionSize)
		if _, err := f.ReadAt(region, offset+int64(h.ProtectedRegionOffset)); err != nil {
			f.Close()
			return nil, errors.Wrap(err, "failed to read protected region")
		}
		devices, err = parseProtectedRegion(region, h.ProtectedDeviceCount)
		if err != nil {
			f.Close()
			return nil, err
		}
	}

	// 读取 Allocation Map
	allocMap := make([]byte, h.BitmapAllocMapSize)
	if _, err := f.ReadAt(allocMap, offset+int64(h.BitmapAllocMapOffset)); err != nil {
		f.Close()
		return nil, errors.Wrap(err, "failed to read allocation map")
	}

	// 校验磁盘位图记录一致性
	for i := range diskBitmaps {
		if err := validateDiskBitmap(&diskBitmaps[i], h, allocMap); err != nil {
			f.Close()
			return nil, errors.Wrapf(err, "disk bitmap %d validation failed", i)
		}
	}

	// 校验磁盘位图单元范围不重叠
	if err := validateBitmapUnitNoOverlap(diskBitmaps); err != nil {
		f.Close()
		return nil, err
	}

	// 校验 ProtectedDevice 一致性
	for i := range devices {
		if err := validateProtectedDevice(&devices[i], h, allocMap); err != nil {
			f.Close()
			return nil, errors.Wrapf(err, "protected device %d validation failed", i)
		}
	}

	return &BioTrkMetadata{
		file:            f,
		filePath:        file,
		offset:          offset,
		header:          *h,
		devices:         devices,
		diskBitmaps:     diskBitmaps,
		allocMap:        allocMap,
		regionHighWater: h.ProtectedRegionSize,
	}, nil
}

// ListValidProtectDevice 返回所有有效的受保护设备。
func (bm *BioTrkMetadata) ListValidProtectDevice() ([]*ProtectedDevice, error) {
	var pds []*ProtectedDevice
	for i := range bm.devices {
		if bm.devices[i].isValid() {
			pds = append(pds, &bm.devices[i])
		}
	}
	return pds, nil
}

// distinctDisks 返回 extents 中去重后的磁盘 ID 集合（保持首次出现顺序）。
func distinctDisks(extents []DiskExtent) []ID {
	var order []ID
	seen := make(map[ID]bool)
	for _, e := range extents {
		if seen[e.DiskID.ID] {
			continue
		}
		seen[e.DiskID.ID] = true
		order = append(order, e.DiskID.ID)
	}
	return order
}

// diskExtentLayout 描述某个磁盘位图按记录顺序拼接后的布局。
type diskExtentLayout struct {
	diskID    DiskID
	extents   []DiskExtent
	bitStart  []uint64 // 每个 extent 在拼接位图中的起始 bit
	totalBits uint64
}

// buildDiskLayouts 从 devices 推导每个磁盘按记录顺序拼接的位图布局。
//
// 磁盘去重顺序 = 第一次出现顺序；extent 顺序 = (设备记录顺序, 设备内 extent 顺序)。
// 每 extent 占 ceil(Size / BitIndexSpace) bit。
func buildDiskLayouts(devices []ProtectedDevice, bitIndexSpace uint64) []diskExtentLayout {
	var order []DiskID
	byID := make(map[ID]*diskExtentLayout)

	for di := range devices {
		for _, ext := range devices[di].Extents {
			lo, ok := byID[ext.DiskID.ID]
			if !ok {
				lo = &diskExtentLayout{diskID: ext.DiskID}
				order = append(order, ext.DiskID)
				byID[ext.DiskID.ID] = lo
			}
			lo.extents = append(lo.extents, ext)
		}
	}

	result := make([]diskExtentLayout, 0, len(order))
	for _, d := range order {
		lo := byID[d.ID]
		lo.bitStart = make([]uint64, len(lo.extents))
		var off uint64
		for i := range lo.extents {
			lo.bitStart[i] = off
			off += (lo.extents[i].Size + bitIndexSpace - 1) / bitIndexSpace
		}
		lo.totalBits = off
		result = append(result, *lo)
	}
	return result
}

// findExtentIndex 在 extents 中查找与 target 完全相同的区间（DiskID+Start+Size）。
// 同一磁盘内的受保护区间不重叠、不重复，因此 Start+Size 唯一标识。
func findExtentIndex(extents []DiskExtent, target DiskExtent) int {
	for i := range extents {
		e := &extents[i]
		if !bytes.Equal(e.DiskID.ID[:], target.DiskID.ID[:]) {
			continue
		}
		if e.Start == target.Start && e.Size == target.Size {
			return i
		}
	}
	return -1
}

// layoutsByID 把磁盘位图布局切片转换为按 DiskID 索引的 map。
func layoutsByID(layouts []diskExtentLayout) map[ID]diskExtentLayout {
	m := make(map[ID]diskExtentLayout, len(layouts))
	for _, lo := range layouts {
		m[lo.diskID.ID] = lo
	}
	return m
}

// syncDiskBitmaps 依据 bm.devices 现状，把 bm.diskBitmaps 同步到一致状态。
//
// oldLayoutByID 为修改前各磁盘的位图布局，用于在扩容搬迁时把幸存区间（在旧布局中
// 出现的区间）的 bit 映射到新位置；调用方在修改 bm.devices 之前通过 buildDiskLayouts
// 计算得出。
//
// 各磁盘按以下规则处理：
//   - 复用：磁盘仍存在且现有 BitmapUnitCount 已满足新的 unit 需求，保留原区间不动；
//   - 扩容：现有 unit 不足时，优先原地向尾部连续增长（无需搬迁数据），
//     尾部空间不足则整体搬迁到新的更大连续区间；
//   - 新增：磁盘首次出现，从零分配连续 unit；
//   - 释放：磁盘不再被任何受保护区间引用，释放其 unit 并删除记录。
//
// sync 失败时会恢复调用前的 diskBitmaps / allocMap / DiskCount，保证内存态一致。
func (bm *BioTrkMetadata) syncDiskBitmaps(oldLayoutByID map[ID]diskExtentLayout) (err error) {
	// 失败时恢复调用前状态
	oldDisks := append([]DiskBitmap(nil), bm.diskBitmaps...)
	oldAllocMap := append([]byte(nil), bm.allocMap...)
	oldCount := bm.header.DiskCount
	defer func() {
		if err != nil {
			bm.diskBitmaps = oldDisks
			bm.allocMap = oldAllocMap
			bm.header.DiskCount = oldCount
		}
	}()

	bitIndexSpace := bm.header.BitIndexSpace
	bitsPerUnit := uint64(bm.header.BitmapClusterSize) * 8

	newLayouts := buildDiskLayouts(bm.devices, bitIndexSpace)
	newLayoutByID := make(map[ID]diskExtentLayout, len(newLayouts))
	for _, lo := range newLayouts {
		newLayoutByID[lo.diskID.ID] = lo
	}

	oldByID := make(map[ID]*DiskBitmap, len(bm.diskBitmaps))
	for i := range bm.diskBitmaps {
		oldByID[bm.diskBitmaps[i].DiskID.ID] = &bm.diskBitmaps[i]
	}

	newDisks := make([]DiskBitmap, 0, len(newLayouts))
	for i := range newLayouts {
		lo := &newLayouts[i]
		unitsNeeded := (lo.totalBits + bitsPerUnit - 1) / bitsPerUnit
		if unitsNeeded == 0 {
			continue
		}

		old := oldByID[lo.diskID.ID]
		if old == nil {
			// 新增磁盘
			start, err := bm.allocBitmapUnits(unitsNeeded)
			if err != nil {
				return err
			}
			newDisks = append(newDisks, DiskBitmap{
				DiskID:          lo.diskID,
				BitmapUnitStart: start,
				BitmapUnitCount: unitsNeeded,
			})
			continue
		}

		if unitsNeeded <= old.BitmapUnitCount {
			// 复用：容量已够，保留原区间
			db := *old
			db.BitmapExtents = nil
			db.BitmapExtentCount = 0
			newDisks = append(newDisks, db)
			continue
		}

		// 扩容
		grown := DiskBitmap{}
		if canGrowInPlace(bm.allocMap, &bm.header, old, unitsNeeded) {
			if err := bm.claimBitmapUnits(old.BitmapUnitStart+old.BitmapUnitCount, unitsNeeded-old.BitmapUnitCount); err != nil {
				return err
			}
			grown = DiskBitmap{
				DiskID:          old.DiskID,
				BitmapUnitStart: old.BitmapUnitStart,
				BitmapUnitCount: unitsNeeded,
			}
		} else {
			oldLo := oldLayoutByID[old.DiskID.ID]
			g, err := bm.relocateDiskBitmap(old, unitsNeeded, lo, oldLo)
			if err != nil {
				return err
			}
			grown = *g
		}
		newDisks = append(newDisks, grown)
	}

	// 释放不再被任何受保护区间引用的磁盘
	for i := range bm.diskBitmaps {
		id := bm.diskBitmaps[i].DiskID.ID
		if _, ok := newLayoutByID[id]; !ok {
			bm.freeBitmapUnits(bm.diskBitmaps[i].BitmapUnitStart, bm.diskBitmaps[i].BitmapUnitCount)
		}
	}

	bm.diskBitmaps = newDisks
	bm.header.DiskCount = uint32(len(newDisks))
	return nil
}

// canGrowInPlace 判断磁盘位图能否原地向后连续扩展到 unitsNeeded 个 unit。
func canGrowInPlace(allocMap []byte, h *Header, old *DiskBitmap, unitsNeeded uint64) bool {
	total := uint64(h.TotalBitmapUnits)
	if old.BitmapUnitStart+unitsNeeded > total {
		return false
	}
	for i := old.BitmapUnitCount; i < unitsNeeded; i++ {
		if isBitmapUnitAllocated(allocMap, old.BitmapUnitStart+i) {
			return false
		}
	}
	return true
}

// claimBitmapUnits 把 [start, start+count) 区间清零并标记为已分配。
// 要求该区间当前全部空闲，否则报错且不做任何修改。
func (bm *BioTrkMetadata) claimBitmapUnits(start, count uint64) error {
	total := uint64(bm.header.TotalBitmapUnits)
	if count == 0 {
		return nil
	}
	if start+count > total {
		return errors.Errorf("bitmap unit range [%d, %d) exceeds total %d", start, start+count, total)
	}
	for i := uint64(0); i < count; i++ {
		if isBitmapUnitAllocated(bm.allocMap, start+i) {
			return errors.Errorf("bitmap unit %d already allocated", start+i)
		}
	}

	unitSize := int64(bm.header.BitmapClusterSize)
	zero := make([]byte, unitSize)
	for i := uint64(0); i < count; i++ {
		off := bm.offset + int64(bm.header.BitmapDataOffset) + int64(start+i)*unitSize
		if _, err := bm.file.WriteAt(zero, off); err != nil {
			return errors.Wrapf(err, "zero bitmap unit %d", start+i)
		}
	}

	for i := uint64(0); i < count; i++ {
		setBitmapUnitAllocated(bm.allocMap, start+i, true)
	}
	return nil
}

// relocateDiskBitmap 把磁盘位图整体搬迁到新的连续区间，保留幸存区间的位图值。
func (bm *BioTrkMetadata) relocateDiskBitmap(old *DiskBitmap, unitsNeeded uint64, newLo *diskExtentLayout, oldLo diskExtentLayout) (*DiskBitmap, error) {
	clusterSize := int64(bm.header.BitmapClusterSize)
	unitBytes := uint64(clusterSize)
	bitIndexSpace := bm.header.BitIndexSpace

	// 读旧数据
	raw := make([]byte, old.BitmapUnitCount*unitBytes)
	off := bm.offset + int64(bm.header.BitmapDataOffset) + int64(old.BitmapUnitStart)*clusterSize
	if _, err := bm.file.ReadAt(raw, off); err != nil {
		return nil, errors.Wrap(err, "read old disk bitmap")
	}

	// 分配新连续区间
	start, err := bm.allocBitmapUnits(unitsNeeded)
	if err != nil {
		return nil, err
	}

	// 迁移幸存区间
	newBytes := make([]byte, unitsNeeded*unitBytes)
	for i := range newLo.extents {
		bitsForExtent := (newLo.extents[i].Size + bitIndexSpace - 1) / bitIndexSpace
		if idx := findExtentIndex(oldLo.extents, newLo.extents[i]); idx >= 0 {
			bitCopy(newBytes, newLo.bitStart[i], raw, oldLo.bitStart[idx], bitsForExtent)
		}
	}

	// 写新数据
	newOff := bm.offset + int64(bm.header.BitmapDataOffset) + int64(start)*clusterSize
	if _, err := bm.file.WriteAt(newBytes, newOff); err != nil {
		bm.freeBitmapUnits(start, unitsNeeded)
		return nil, errors.Wrap(err, "write new disk bitmap")
	}

	// 释放旧区间
	bm.freeBitmapUnits(old.BitmapUnitStart, old.BitmapUnitCount)

	return &DiskBitmap{DiskID: old.DiskID, BitmapUnitStart: start, BitmapUnitCount: unitsNeeded}, nil
}

// diskLayoutFor 返回指定磁盘的位图布局，找不到返回 nil。
func (bm *BioTrkMetadata) diskLayoutFor(diskID ID) *diskExtentLayout {
	layouts := buildDiskLayouts(bm.devices, bm.header.BitIndexSpace)
	for i := range layouts {
		if bytes.Equal(layouts[i].diskID.ID[:], diskID[:]) {
			return &layouts[i]
		}
	}
	return nil
}

// findDiskBitmap 返回指定磁盘的位图记录，找不到返回 nil。
func (bm *BioTrkMetadata) findDiskBitmap(diskID ID) *DiskBitmap {
	for i := range bm.diskBitmaps {
		if bytes.Equal(bm.diskBitmaps[i].DiskID.ID[:], diskID[:]) {
			return &bm.diskBitmaps[i]
		}
	}
	return nil
}

// AddProtectDevice 添加一个受保护设备。不支持同名 devID 同时存在。
//
// 位图以磁盘为单位维护：去重得到本次新增受保护对象触及的磁盘集合，
// 追加这些磁盘的位图记录（对已有磁盘则增长其位图）。
func (bm *BioTrkMetadata) AddProtectDevice(devType DeviceType, devID ID, extents []DiskExtent) error {
	// 检查是否已存在同名设备
	for i := range bm.devices {
		if bm.devices[i].isValid() && bytes.Equal(bm.devices[i].DeviceID[:], devID[:]) {
			return errors.Errorf("device %s already exists", xutil.TrimZeroString(devID[:]))
		}
	}

	if len(extents) == 0 {
		return errors.New("extents must not be empty")
	}

	// 检查新 extents 是否与已有设备的 extents 重叠
	for _, newExt := range extents {
		for _, existingDev := range bm.devices {
			if !existingDev.isValid() {
				continue
			}
			for _, existingExt := range existingDev.Extents {
				if extentsOverlap(newExt, existingExt) {
					return errors.Errorf(
						"extent %s overlaps with existing extent %s of device %s",
						newExt.String(), existingExt.String(),
						xutil.TrimZeroString(existingDev.DeviceID[:]),
					)
				}
			}
		}
	}

	oldLayouts := layoutsByID(buildDiskLayouts(bm.devices, bm.header.BitIndexSpace))

	bm.devices = append(bm.devices, ProtectedDevice{
		Type:        devType,
		DeviceID:    devID,
		ExtentCount: uint32(len(extents)),
		Extents:     append([]DiskExtent(nil), extents...),
	})
	bm.header.ProtectedDeviceCount = uint32(len(bm.devices))

	if err := bm.syncDiskBitmaps(oldLayouts); err != nil {
		bm.devices = bm.devices[:len(bm.devices)-1]
		bm.header.ProtectedDeviceCount = uint32(len(bm.devices))
		return err
	}

	return nil
}

// RemoveProtectDevice 移除指定设备 ID 的受保护设备。若不存在也返回 nil。
//
// 位图以磁盘为单位维护：去重得到被删除受保护对象触及的磁盘集合，
// 裁剪这些磁盘的位图记录（磁盘不再有任何受保护区间时删除记录并释放 unit）。
func (bm *BioTrkMetadata) RemoveProtectDevice(devID ID) error {
	idx := -1
	for i := range bm.devices {
		if bm.devices[i].isValid() && bytes.Equal(bm.devices[i].DeviceID[:], devID[:]) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}

	oldLayouts := layoutsByID(buildDiskLayouts(bm.devices, bm.header.BitIndexSpace))

	removed := bm.devices[idx]
	bm.devices = append(bm.devices[:idx], bm.devices[idx+1:]...)
	bm.header.ProtectedDeviceCount = uint32(len(bm.devices))

	if err := bm.syncDiskBitmaps(oldLayouts); err != nil {
		bm.devices = append(bm.devices, ProtectedDevice{})
		copy(bm.devices[idx+1:], bm.devices[idx:])
		bm.devices[idx] = removed
		bm.header.ProtectedDeviceCount = uint32(len(bm.devices))
		return err
	}

	return nil
}

// RemoveAllProtectDevice 移除所有受保护设备。
func (bm *BioTrkMetadata) RemoveAllProtectDevice() error {
	for i := range bm.diskBitmaps {
		bm.freeBitmapUnits(bm.diskBitmaps[i].BitmapUnitStart, bm.diskBitmaps[i].BitmapUnitCount)
	}
	bm.diskBitmaps = nil
	bm.header.DiskCount = 0

	bm.devices = nil
	bm.header.ProtectedDeviceCount = 0
	return nil
}

// ReadDiskBitmap 读取指定物理磁盘的位图，按该盘受保护区间拼接后的顺序返回。
//
// 返回的 bitCount 为总有效 bit 数，bitmapData 为 packed bytes（bitCount 不足 8 的倍数时最后一个字节高位补 0）。
func (bm *BioTrkMetadata) ReadDiskBitmap(diskID ID) (bitCount uint32, bitmapData []byte, err error) {
	layout := bm.diskLayoutFor(diskID)
	if layout == nil || layout.totalBits == 0 {
		return 0, nil, errors.Errorf("disk %s not found", xutil.TrimZeroString(diskID[:]))
	}

	db := bm.findDiskBitmap(diskID)
	if db == nil {
		return 0, nil, errors.Errorf("disk %s has no bitmap record", xutil.TrimZeroString(diskID[:]))
	}

	unitBytes := int64(bm.header.BitmapClusterSize)
	raw := make([]byte, db.BitmapUnitCount*uint64(unitBytes))
	off := bm.offset + int64(bm.header.BitmapDataOffset) + int64(db.BitmapUnitStart)*unitBytes
	if _, err := bm.file.ReadAt(raw, off); err != nil {
		return 0, nil, errors.Wrap(err, "read disk bitmap")
	}

	dataBytes := (layout.totalBits + 7) / 8
	bitmapData = make([]byte, dataBytes)
	bitCopy(bitmapData, 0, raw, 0, layout.totalBits)

	return uint32(layout.totalBits), bitmapData, nil
}

// ResolveBitmapExtents 解析每个磁盘位图记录的位图数据在物理磁盘上的分布。
//
// 必须在 Flush 之前调用（Flush 内部会自动调用），确保写入磁盘的记录包含正确的
// BitmapExtents。驱动后续基于 BitmapExtents 直接写入物理磁盘更新位图，
// 若数据错误可能导致写到元数据位图数据区之外，损坏磁盘数据。
//
// 算法：
//  1. 获取整个元数据区域的物理磁盘分布（PhysicalExtents）
//  2. 裁剪到位图数据区 [BitmapDataOffset, BitmapDataOffset + BitmapClusterSize*TotalBitmapUnits)
//  3. 对每个磁盘位图记录，将其 bitmap unit 范围映射到物理 extent
//
// 位图数据区位于记录区之前，在 Create 时就已完整分配且永不移动，
// 因此解析结果与设备记录的内容和数量完全无关，Flush 一次写入即可得到最终结果。
func (bm *BioTrkMetadata) ResolveBitmapExtents() error {
	physExtents, err := bm.PhysicalExtents()
	if err != nil {
		return errors.Wrap(err, "failed to get physical extents for bitmap resolution")
	}

	clusterSize := int64(bm.header.BitmapClusterSize)
	bitmapDataStart := int64(bm.header.BitmapDataOffset)
	bitmapDataEnd := bitmapDataStart + clusterSize*int64(bm.header.TotalBitmapUnits)

	// 将元数据物理 extent 裁剪到位图数据区
	type physSeg struct {
		diskID    DiskID
		physStart int64 // 物理磁盘偏移
		metaStart int64 // 在元数据区域内的逻辑偏移
		size      int64
	}
	var bitmapSegs []physSeg
	var metaOff int64

	for _, e := range physExtents {
		extMetaStart := metaOff
		extMetaEnd := metaOff + int64(e.Size)

		if extMetaEnd <= bitmapDataStart {
			metaOff += int64(e.Size)
			continue
		}
		if extMetaStart >= bitmapDataEnd {
			break
		}

		clipStart := extMetaStart
		if clipStart < bitmapDataStart {
			clipStart = bitmapDataStart
		}
		clipEnd := extMetaEnd
		if clipEnd > bitmapDataEnd {
			clipEnd = bitmapDataEnd
		}

		bitmapSegs = append(bitmapSegs, physSeg{
			diskID:    e.DiskID,
			physStart: int64(e.Start) + (clipStart - extMetaStart),
			metaStart: clipStart - bitmapDataStart, // 相对于位图数据区起始的偏移
			size:      clipEnd - clipStart,
		})

		metaOff += int64(e.Size)
	}

	// 为每个磁盘位图记录解析 BitmapExtents
	for i := range bm.diskBitmaps {
		db := &bm.diskBitmaps[i]

		unitStart := int64(db.BitmapUnitStart) * clusterSize
		unitEnd := int64(db.BitmapUnitStart+db.BitmapUnitCount) * clusterSize

		extents := make([]DiskExtent, 0, len(bitmapSegs))
		for _, seg := range bitmapSegs {
			segStart := seg.metaStart
			segEnd := seg.metaStart + seg.size

			if segEnd <= unitStart {
				continue
			}
			if segStart >= unitEnd {
				break
			}

			clipStart := segStart
			if clipStart < unitStart {
				clipStart = unitStart
			}
			clipEnd := segEnd
			if clipEnd > unitEnd {
				clipEnd = unitEnd
			}

			extents = append(extents, DiskExtent{
				DiskID: seg.diskID,
				Start:  uint64(seg.physStart + (clipStart - segStart)),
				Size:   uint64(clipEnd - clipStart),
			})
		}

		db.BitmapExtents = extents
		db.BitmapExtentCount = uint32(len(extents))
	}

	return nil
}

// ListDiskBitmaps 返回所有存在保护区域的磁盘位图记录。
func (bm *BioTrkMetadata) ListDiskBitmaps() []*DiskBitmap {
	out := make([]*DiskBitmap, 0, len(bm.diskBitmaps))
	for i := range bm.diskBitmaps {
		out = append(out, &bm.diskBitmaps[i])
	}
	return out
}

// Flush 将内存中的元数据持久化到磁盘。
//
// 单趟写入：位图数据区在 Create 时就已完整分配，物理分布不会因记录区增长而改变，
// 所以 ResolveBitmapExtents 的结果在写记录之前就已经是最终值，
// 不存在"先写再解析再重写"的中间态。
//
// 返回元数据区域在物理磁盘上的分布（DiskExtent 列表），
// 调用方可直接用它定位本文件所占的物理区间。
func (bm *BioTrkMetadata) Flush() ([]DiskExtent, error) {
	if err := bm.ResolveBitmapExtents(); err != nil {
		return nil, err
	}

	if err := bm.flushRecords(); err != nil {
		return nil, err
	}

	if err := bm.file.Sync(); err != nil {
		return nil, err
	}

	return bm.PhysicalExtents()
}

// flushRecords 写入 Disk Bitmap Allocation、Protected Region、Allocation Map 和 Header。
//
// 两个变长记录区各自打包成整体、末尾补齐到 AlignSize 后一次写入：
// 变长记录的单条长度不保证扇区对齐，只有整区读写才能在裸设备上成立。
func (bm *BioTrkMetadata) flushRecords() error {
	// 1) Disk Bitmap Allocation 区域
	var diskSize uint64
	for i := range bm.diskBitmaps {
		diskSize += uint64(calcDiskBitmapBinSize(&bm.diskBitmaps[i]))
	}
	bm.header.DiskBitmapAllocRegionSize = alignUp(diskSize)

	diskBlobSize := alignUp(diskSize)
	if diskBlobSize > 0 {
		blob := make([]byte, diskBlobSize)
		pos := 0
		for i := range bm.diskBitmaps {
			buf, err := packDiskBitmap(&bm.diskBitmaps[i])
			if err != nil {
				return errors.Wrapf(err, "failed to pack disk bitmap %d", i)
			}
			copy(blob[pos:], buf)
			pos += len(buf)
		}

		off := bm.offset + int64(bm.header.DiskBitmapAllocRegionOffset)
		if _, err := bm.file.WriteAt(blob, off); err != nil {
			return errors.Wrap(err, "failed to write disk bitmap region")
		}
	}

	// 2) Protected Region 紧随磁盘位图分配区域
	bm.header.ProtectedRegionOffset = bm.header.DiskBitmapAllocRegionOffset + bm.header.DiskBitmapAllocRegionSize

	var recordsSize uint64
	for i := range bm.devices {
		recordsSize += uint64(calcProtectedDeviceBinSize(&bm.devices[i]))
	}

	blobSize := alignUp(recordsSize)
	// 记录变少时区域会缩小，把历史高水位以下一并写零，不留已删除设备的残留记录
	if blobSize < bm.regionHighWater {
		blobSize = bm.regionHighWater
	}

	if blobSize > 0 {
		blob := make([]byte, blobSize)
		pos := 0
		for i := range bm.devices {
			buf, err := packProtectedDevice(&bm.devices[i])
			if err != nil {
				return errors.Wrapf(err, "failed to pack protected device %d", i)
			}
			copy(blob[pos:], buf)
			pos += len(buf)
		}

		regionOffset := bm.offset + int64(bm.header.ProtectedRegionOffset)
		if _, err := bm.file.WriteAt(blob, regionOffset); err != nil {
			return errors.Wrap(err, "failed to write protected region")
		}
	}
	bm.regionHighWater = blobSize

	// 3) Allocation Map
	if _, err := bm.file.WriteAt(bm.allocMap, bm.offset+int64(bm.header.BitmapAllocMapOffset)); err != nil {
		return errors.Wrap(err, "failed to write allocation map")
	}

	// 4) Header
	bm.header.ProtectedRegionSize = alignUp(recordsSize)
	bm.header.ProtectedDeviceCount = uint32(len(bm.devices))
	bm.header.DiskCount = uint32(len(bm.diskBitmaps))
	bm.header.HeaderCRC32 = calcCRC32(&bm.header)
	if err := writeHeader(bm.file, bm.offset, &bm.header); err != nil {
		return err
	}

	return nil
}

// Size 返回元数据区域总大小（字节）。
//
// Protected Region 是最后一个区域，因此该值随设备记录增减而变化。
func (bm *BioTrkMetadata) Size() int64 {
	return bm.header.TotalSize()
}

// PhysicalExtents 返回元数据区域在物理磁盘上的存储区域。
//
// 对于普通文件，通过文件系统查询文件所占的物理磁盘区间，截取 [offset, offset+size) 范围。
// 对于设备路径（如 \\.\PHYSICALDRIVE0），直接返回设备上 offset 处的单一区间。
//
// DiskID 的 ID 字段存放磁盘的 PNPDeviceID（与 DiskID 的契约一致）——
// 调用方如需打开设备，取 DiskID.DevicePath() 即可。
func (bm *BioTrkMetadata) PhysicalExtents() ([]DiskExtent, error) {
	size := bm.Size()

	// 设备路径：元数据直接位于设备上
	if isDevicePath(bm.filePath) {
		var d DiskExtent
		d.Start = uint64(bm.offset)
		d.Size = uint64(size)
		diskId, err := getPhysicalDiskID(bm.filePath)
		if err != nil {
			return nil, errors.Wrap(err, "failed to resolve disk id")
		}
		copy(d.DiskID.ID[:], diskId)
		return []DiskExtent{d}, nil
	}

	// 普通文件：查询文件系统级物理区间，截取元数据区域
	allExtents, err := xutil.FileDiskExtents(bm.filePath)
	if err != nil {
		return nil, err
	}

	metaStart := bm.offset
	metaEnd := bm.offset + size
	var result []DiskExtent
	var fileOff int64 // 当前 extent 在文件内的起始偏移

	for _, e := range allExtents {
		extFileStart := fileOff
		extFileEnd := fileOff + e.Size

		// 区间在元数据区域之前，跳过
		if extFileEnd <= metaStart {
			fileOff += e.Size
			continue
		}
		// 区间在元数据区域之后，结束
		if extFileStart >= metaEnd {
			break
		}

		// 计算文件内交集
		clipStart := extFileStart
		if clipStart < metaStart {
			clipStart = metaStart
		}
		clipEnd := extFileEnd
		if clipEnd > metaEnd {
			clipEnd = metaEnd
		}

		// 物理磁盘偏移 = extent 物理起始 + (交集起始 - extent 文件起始)
		physStart := e.Start + (clipStart - extFileStart)
		diskId, err := getPhysicalDiskID(e.Disk)
		if err != nil {
			return nil, errors.Wrap(err, "failed to resolve disk id")
		}
		result = append(result, DiskExtent{
			DiskID: func() DiskID { var id DiskID; copy(id.ID[:], diskId); return id }(),
			Start:  uint64(physStart),
			Size:   uint64(clipEnd - clipStart),
		})

		fileOff += e.Size
	}

	return result, nil
}

// StreamDirtyBlocks 遍历位图中每个置 1 的 bit，从源磁盘读取对应数据块并回调 handle。
//
// des 是受保护设备在多个物理磁盘上的线性分布，各 Extent 按顺序首尾相接形成连续逻辑空间。
// 例如 [{disk0, 0, 1024}, {disk1, 1048576, 1024}] 表示逻辑偏移 [0, 2048) 的线性空间，
// 其中 [0, 1024) 映射到 disk0 偏移 0，[1024, 2048) 映射到 disk1 偏移 1048576。
//
// bitIndexSpace 是每个 bit 对应的磁盘空间大小（即 Header.BitIndexSpace）。
// 第 K 个 bit 对应逻辑偏移 K × bitIndexSpace，在该偏移处读取 bitIndexSpace 字节。
func StreamDirtyBlocks(
	des []DiskExtent,
	bitIndexSpace uint64,
	bitCount uint32,
	bitmapData []byte,
	handle func(diskData []byte) error,
) error {
	blockBuf := xutil.AlignedBlock(int(bitIndexSpace))
	// 按磁盘缓存已打开的文件句柄，避免重复 open/close
	diskFiles := make(map[string]*os.File)
	defer func() {
		for _, f := range diskFiles {
			f.Close()
		}
	}()

	for i := uint32(0); i < bitCount; i++ {
		if !bitTest(bitmapData, uint64(i)) {
			continue
		}

		logicalOff := uint64(i) * bitIndexSpace
		extIdx, offInExtent, err := mapLinearToExtent(des, logicalOff)
		if err != nil {
			return err
		}

		ext := &des[extIdx]
		physOff := ext.Start + offInExtent

		diskPath, err := ext.DiskID.DevicePath()
		if err != nil {
			return errors.Wrapf(err, "resolve device path for %s", ext.DiskID.String())
		}

		df, ok := diskFiles[diskPath]
		if !ok {
			df, err = os.OpenFile(diskPath, xutil.DiskReadFlags, 0)
			if err != nil {
				return errors.Wrapf(err, "open disk %s", diskPath)
			}
			diskFiles[diskPath] = df
		}

		if _, err := df.ReadAt(blockBuf, int64(physOff)); err != nil {
			return errors.Wrapf(err, "read disk %s at offset %d", diskPath, physOff)
		}

		if err := handle(blockBuf); err != nil {
			return err
		}
	}

	return nil
}

// findDevice 按 devID 查找受保护设备，返回指针或 nil。
func (bm *BioTrkMetadata) findDevice(devID ID) *ProtectedDevice {
	for i := range bm.devices {
		if bm.devices[i].isValid() && bytes.Equal(bm.devices[i].DeviceID[:], devID[:]) {
			return &bm.devices[i]
		}
	}
	return nil
}

// bitCopy 按位拷贝，将 src 中 srcBitOff 起的 n 位复制到 dst 的 dstBitOff 位置。
// 调用方保证 dst 和 src 不重叠且不越界。
func bitCopy(dst []byte, dstBitOff uint64, src []byte, srcBitOff uint64, n uint64) {
	for i := uint64(0); i < n; i++ {
		srcByte := (srcBitOff + i) / 8
		srcBit := (srcBitOff + i) % 8
		dstByte := (dstBitOff + i) / 8
		dstBit := (dstBitOff + i) % 8

		if src[srcByte]&(1<<srcBit) != 0 {
			dst[dstByte] |= 1 << dstBit
		} else {
			dst[dstByte] &^= 1 << dstBit
		}
	}
}

// bitTest 测试位图中第 k 位是否为 1。
func bitTest(bitmap []byte, k uint64) bool {
	return bitmap[k/8]&(1<<(k%8)) != 0
}

// mapLinearToExtent 将线性逻辑偏移映射到 des 中的某个 DiskExtent。
// 返回 extent 索引及 extent 内的偏移。
func mapLinearToExtent(des []DiskExtent, logicalOff uint64) (extIdx int, offInExtent uint64, err error) {
	var cumulative uint64
	for i := range des {
		if logicalOff < cumulative+des[i].Size {
			return i, logicalOff - cumulative, nil
		}
		cumulative += des[i].Size
	}
	return 0, 0, errors.Errorf("logical offset %d out of range", logicalOff)
}

// defaultHeader 创建默认 Header，并推导好与记录内容无关的区域布局。
//
// totalBitmapUnits 指定位图单元总数，传 0 则使用 DefaultTotalBitmapUnits。
// bitIndexSpace 指定每个 Bitmap Bit 对应的磁盘空间大小，传 0 则使用 DefaultBitIndexSpace。
func defaultHeader(totalBitmapUnits, bitIndexSpace uint64) Header {
	if totalBitmapUnits == 0 {
		totalBitmapUnits = DefaultTotalBitmapUnits
	}
	if bitIndexSpace == 0 {
		bitIndexSpace = DefaultBitIndexSpace
	}

	h := Header{
		Version:           Versionv1_0,
		BitIndexSpace:     bitIndexSpace,
		BitmapClusterSize: DefaultBitmapClusterSize,
		TotalBitmapUnits:  uint32(totalBitmapUnits),
	}
	copy(h.Signature[:], SignatureStr)
	applyBaseLayout(&h)
	return h
}

// calcCRC32 计算 Header CRC32，计算时 HeaderCRC32 字段按 0 处理。
func calcCRC32(h *Header) uint32 {
	saved := h.HeaderCRC32
	h.HeaderCRC32 = 0
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, h)
	h.HeaderCRC32 = saved
	return crc32.ChecksumIEEE(buf.Bytes())
}

// writeHeader 写入 Header 到文件指定偏移。
func writeHeader(f *os.File, offset int64, h *Header) error {
	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.LittleEndian, h); err != nil {
		return err
	}
	if buf.Len() != HeaderSize {
		return errors.Errorf("header size mismatch: %d != %d", buf.Len(), HeaderSize)
	}
	_, err := f.WriteAt(buf.Bytes(), offset)
	return err
}

// readHeader 从文件指定偏移读取 Header。
func readHeader(f *os.File, offset int64) (*Header, error) {
	h := new(Header)
	buf := make([]byte, HeaderSize)
	if _, err := f.ReadAt(buf, offset); err != nil {
		return nil, errors.Wrap(err, "failed to read header")
	}
	if err := binary.Read(bytes.NewReader(buf), binary.LittleEndian, h); err != nil {
		return nil, errors.Wrap(err, "failed to decode header")
	}
	return h, nil
}

// validateHeader 校验 Header 一致性。
//
// 除 ProtectedRegionSize 之外的区域偏移与大小都能由 Bitmap 参数推导出来，
// 因此按等值校验，任何一处对不上都说明 Header 或区域被破坏。
func validateHeader(f *os.File, offset int64, h *Header) error {
	// Signature
	if xutil.TrimZeroString(h.Signature[:]) != SignatureStr {
		return errors.Errorf("signature mismatch: got %q, expected %q",
			xutil.TrimZeroString(h.Signature[:]), SignatureStr)
	}

	// Version
	if h.Version != Versionv1_0 {
		return errors.Errorf("unsupported version: 0x%08x", h.Version)
	}

	// CRC32
	expectedCRC := calcCRC32(h)
	if h.HeaderCRC32 != expectedCRC {
		return errors.Errorf("header CRC32 mismatch: 0x%08x != 0x%08x", h.HeaderCRC32, expectedCRC)
	}

	// Bitmap 参数
	if h.BitmapClusterSize == 0 {
		return errors.New("BitmapClusterSize must be > 0")
	}
	if h.TotalBitmapUnits == 0 {
		return errors.New("TotalBitmapUnits must be > 0")
	}
	if h.BitIndexSpace == 0 {
		return errors.New("BitIndexSpace must be > 0")
	}

	// 区域布局必须与推导结果完全一致
	expected := *h
	applyBaseLayout(&expected)
	if h.BitmapAllocMapOffset != expected.BitmapAllocMapOffset {
		return errors.Errorf("BitmapAllocMapOffset %d != expected %d",
			h.BitmapAllocMapOffset, expected.BitmapAllocMapOffset)
	}
	if h.BitmapAllocMapSize != expected.BitmapAllocMapSize {
		return errors.Errorf("BitmapAllocMapSize %d != expected %d",
			h.BitmapAllocMapSize, expected.BitmapAllocMapSize)
	}
	if h.BitmapDataOffset != expected.BitmapDataOffset {
		return errors.Errorf("BitmapDataOffset %d != expected %d",
			h.BitmapDataOffset, expected.BitmapDataOffset)
	}
	if h.DiskBitmapAllocRegionOffset != expected.DiskBitmapAllocRegionOffset {
		return errors.Errorf("DiskBitmapAllocRegionOffset %d != expected %d",
			h.DiskBitmapAllocRegionOffset, expected.DiskBitmapAllocRegionOffset)
	}

	// DiskBitBerapAllocRegionSize / ProtectedRegionOffset 随记录内容变化
	if h.DiskBitmapAllocRegionSize%AlignSize != 0 {
		return errors.Errorf("DiskBitmapAllocRegionSize %d is not aligned to %d",
			h.DiskBitmapAllocRegionSize, AlignSize)
	}
	if h.ProtectedRegionOffset != h.DiskBitmapAllocRegionOffset+h.DiskBitmapAllocRegionSize {
		return errors.Errorf("ProtectedRegionOffset %d != DiskBitmapAllocRegionOffset %d + DiskBitmapAllocRegionSize %d",
			h.ProtectedRegionOffset, h.DiskBitmapAllocRegionOffset, h.DiskBitmapAllocRegionSize)
	}

	// DiskCount / DiskBitmapAllocRegionSize 下限
	if h.DiskCount > 0 {
		minSize := uint64(h.DiskCount) * DiskBitmapMinBinSize
		if h.DiskBitmapAllocRegionSize < minSize {
			return errors.Errorf("DiskBitmapAllocRegionSize %d too small for %d records (need >= %d)",
				h.DiskBitmapAllocRegionSize, h.DiskCount, minSize)
		}
	} else if h.DiskBitmapAllocRegionSize != 0 {
		return errors.Errorf("DiskBitmapAllocRegionSize must be 0 when DiskCount is 0, got %d",
			h.DiskBitmapAllocRegionSize)
	}

	// ProtectedRegionSize 随记录内容变化，只校验对齐与下限
	if h.ProtectedRegionSize%AlignSize != 0 {
		return errors.Errorf("ProtectedRegionSize %d is not aligned to %d", h.ProtectedRegionSize, AlignSize)
	}
	if h.ProtectedDeviceCount > 0 {
		minSize := uint64(h.ProtectedDeviceCount) * ProtectedDeviceMinBinSize
		if h.ProtectedRegionSize < minSize {
			return errors.Errorf("ProtectedRegionSize %d too small for %d records (need >= %d)",
				h.ProtectedRegionSize, h.ProtectedDeviceCount, minSize)
		}
	} else if h.ProtectedRegionSize != 0 {
		return errors.Errorf("ProtectedRegionSize must be 0 when ProtectedDeviceCount is 0, got %d",
			h.ProtectedRegionSize)
	}

	// 区域起始偏移必须对齐，否则裸设备上无法整体读写
	for _, v := range []uint64{
		h.BitmapAllocMapOffset,
		h.BitmapDataOffset,
		h.DiskBitmapAllocRegionOffset,
		h.ProtectedRegionOffset,
	} {
		if v%AlignSize != 0 {
			return errors.Errorf("region offset %d is not aligned to %d", v, AlignSize)
		}
	}

	// 元数据区域必须完整落在文件内
	// 设备路径上 Stat 返回的不是设备容量，跳过大小检查（设备容量由调用方保证）
	if !isDevicePath(f.Name()) {
		stat, err := f.Stat()
		if err != nil {
			return errors.Wrap(err, "failed to stat file")
		}
		if uint64(stat.Size()) < uint64(offset)+uint64(h.TotalSize()) {
			return errors.Errorf("file too small: %d bytes, metadata region needs %d bytes at offset %d",
				stat.Size(), h.TotalSize(), offset)
		}
	}

	return nil
}

// validateProtectedDevice 校验单条 ProtectedDevice。
//
// 位图单元相关的校验已经移交到 DiskBitmap 记录（磁盘级别），这里仅校验设备与受保护区间本身。
func validateProtectedDevice(pd *ProtectedDevice, h *Header, allocMap []byte) error {
	_ = h
	_ = allocMap

	// DeviceType
	if pd.Type != DeviceTypeDisk && pd.Type != DeviceTypeVolume {
		return errors.Errorf("invalid DeviceType: %d", pd.Type)
	}

	// DeviceID
	if isEmptyDeviceID(pd.DeviceID) {
		return errors.New("DeviceID is empty")
	}

	// Extents
	for i := range pd.Extents {
		ext := &pd.Extents[i]

		if ext.Size == 0 {
			return errors.Errorf("extent %d: Size is 0", i)
		}
		if isEmptyDeviceID(ext.DiskID.ID) {
			return errors.Errorf("extent %d: DiskID is empty", i)
		}
	}

	return nil
}

// validateDiskBitmap 校验单条磁盘位图记录。
func validateDiskBitmap(db *DiskBitmap, h *Header, allocMap []byte) error {
	if isEmptyDeviceID(db.DiskID.ID) {
		return errors.New("DiskID is empty")
	}
	if db.BitmapUnitCount == 0 {
		return errors.New("BitmapUnitCount is 0")
	}

	// BitmapUnit 范围不得越界
	if db.BitmapUnitStart >= uint64(h.TotalBitmapUnits) {
		return errors.Errorf("BitmapUnitStart %d >= TotalBitmapUnits %d",
			db.BitmapUnitStart, h.TotalBitmapUnits)
	}
	if db.BitmapUnitCount > uint64(h.TotalBitmapUnits)-db.BitmapUnitStart {
		return errors.Errorf("BitmapUnit range [%d, %d) exceeds TotalBitmapUnits %d",
			db.BitmapUnitStart, db.BitmapUnitStart+db.BitmapUnitCount, h.TotalBitmapUnits)
	}

	// 引用的 Bitmap Unit 必须已分配
	for u := db.BitmapUnitStart; u < db.BitmapUnitStart+db.BitmapUnitCount; u++ {
		if !isBitmapUnitAllocated(allocMap, u) {
			return errors.Errorf("bitmap unit %d not allocated", u)
		}
	}

	return validateDiskBitmapExtents(db, h)
}

// validateDiskBitmapExtents 校验单个磁盘位图记录的位图物理分布。
//
// 驱动会照着这份分布直接往物理磁盘写位图，写错位置会损坏磁盘数据，
// 因此这里做完整校验：每段大小非零且按 Bitmap Unit 对齐、
// 各段长度之和恰好覆盖该磁盘位图记录的全部 Bitmap Unit。
//
// BitmapExtents 只在 Flush 之后才有内容，为空表示尚未解析，跳过校验。
func validateDiskBitmapExtents(db *DiskBitmap, h *Header) error {
	if len(db.BitmapExtents) == 0 {
		return nil
	}

	clusterSize := uint64(h.BitmapClusterSize)
	var total uint64
	for i := range db.BitmapExtents {
		be := &db.BitmapExtents[i]
		if isEmptyDeviceID(be.DiskID.ID) {
			return errors.Errorf("bitmap extent %d: DiskID is empty", i)
		}
		if be.Size == 0 {
			return errors.Errorf("bitmap extent %d: Size is 0", i)
		}
		if be.Size%clusterSize != 0 {
			return errors.Errorf("bitmap extent %d: Size %d is not a multiple of BitmapClusterSize %d",
				i, be.Size, clusterSize)
		}
		total += be.Size
	}

	expected := db.BitmapUnitCount * clusterSize
	if total != expected {
		return errors.Errorf("bitmap extents total size %d != expected %d (%d units x %d bytes)",
			total, expected, db.BitmapUnitCount, clusterSize)
	}

	return nil
}

// validateBitmapUnitNoOverlap 校验磁盘位图记录之间的 Bitmap Unit 范围不重叠。
func validateBitmapUnitNoOverlap(disks []DiskBitmap) error {
	for i := 0; i < len(disks); i++ {
		aStart := disks[i].BitmapUnitStart
		aEnd := aStart + disks[i].BitmapUnitCount
		for j := i + 1; j < len(disks); j++ {
			bStart := disks[j].BitmapUnitStart
			bEnd := bStart + disks[j].BitmapUnitCount
			if aStart < bEnd && bStart < aEnd {
				return errors.Errorf(
					"bitmap unit range overlap: [%d, %d) and [%d, %d)",
					aStart, aEnd, bStart, bEnd)
			}
		}
	}
	return nil
}

// allocBitmapUnits 分配连续的 Bitmap Unit，将对应 Bitmap Unit Data 初始化为零。
func (bm *BioTrkMetadata) allocBitmapUnits(count uint64) (uint64, error) {
	start, err := findFreeBitmapUnits(bm.allocMap, uint64(bm.header.TotalBitmapUnits), count)
	if err != nil {
		return 0, err
	}

	// 将对应 Bitmap Unit Data 初始化为零
	unitSize := int64(bm.header.BitmapClusterSize)
	zeroUnit := make([]byte, unitSize)
	for i := uint64(0); i < count; i++ {
		unitOffset := bm.offset + int64(bm.header.BitmapDataOffset) + int64(start+i)*unitSize
		if _, err := bm.file.WriteAt(zeroUnit, unitOffset); err != nil {
			return 0, errors.Wrapf(err, "failed to zero bitmap unit %d", start+i)
		}
	}

	// 标记为已分配
	for i := uint64(0); i < count; i++ {
		setBitmapUnitAllocated(bm.allocMap, start+i, true)
	}

	return start, nil
}

// freeBitmapUnits 释放 Bitmap Unit。
func (bm *BioTrkMetadata) freeBitmapUnits(start, count uint64) {
	for i := uint64(0); i < count; i++ {
		setBitmapUnitAllocated(bm.allocMap, start+i, false)
	}
}

// findFreeBitmapUnits 在 Allocation Map 中查找 count 个连续空闲 Bitmap Unit。
func findFreeBitmapUnits(allocMap []byte, totalUnits, count uint64) (uint64, error) {
	if count == 0 {
		return 0, errors.New("count must be > 0")
	}
	if count > totalUnits {
		return 0, errors.Errorf("not enough bitmap units: need %d, total %d", count, totalUnits)
	}

	var consecutive uint64
	for i := uint64(0); i < totalUnits; i++ {
		if isBitmapUnitAllocated(allocMap, i) {
			consecutive = 0
		} else {
			consecutive++
			if consecutive >= count {
				return i - count + 1, nil
			}
		}
	}

	return 0, errors.Errorf("no contiguous free bitmap units for count %d", count)
}

// extentsOverlap 判断两个 DiskExtent 是否有重叠区域。
//
// 仅当两个 Extent 位于同一磁盘（DiskID 相等）且字节范围存在交集时返回 true。
func extentsOverlap(a, b DiskExtent) bool {
	if !bytes.Equal(a.DiskID.ID[:], b.DiskID.ID[:]) {
		return false
	}
	if a.Size == 0 || b.Size == 0 {
		return false
	}
	aEnd := a.Start + a.Size
	bEnd := b.Start + b.Size
	return a.Start < bEnd && b.Start < aEnd
}

// isBitmapUnitAllocated 检查 Bitmap Unit 是否已分配。
func isBitmapUnitAllocated(allocMap []byte, unit uint64) bool {
	byteIdx := unit / 8
	bitIdx := unit % 8
	if byteIdx >= uint64(len(allocMap)) {
		return false
	}
	return allocMap[byteIdx]&(1<<bitIdx) != 0
}

// setBitmapUnitAllocated 设置 Bitmap Unit 分配状态。
func setBitmapUnitAllocated(allocMap []byte, unit uint64, allocated bool) {
	byteIdx := unit / 8
	bitIdx := unit % 8
	if byteIdx >= uint64(len(allocMap)) {
		return
	}
	if allocated {
		allocMap[byteIdx] |= 1 << bitIdx
	} else {
		allocMap[byteIdx] &^= 1 << bitIdx
	}
}

// isEmptyDeviceID 检查 DeviceID 是否为空。
func isEmptyDeviceID(id [DeviceIDLen]byte) bool {
	for _, b := range id {
		if b != 0 {
			return false
		}
	}
	return true
}

// isValid 检查 ProtectedDevice 是否有效。
func (d *ProtectedDevice) isValid() bool {
	return (d.Type == DeviceTypeDisk || d.Type == DeviceTypeVolume) &&
		!isEmptyDeviceID(d.DeviceID)
}

// zeroRegion 将从 offset 起的 size 字节全部写零。
//
// size 必须是 AlignSize 的整数倍，以保证裸设备上的写入偏移与长度都扇区对齐。
func zeroRegion(f *os.File, offset int64, size int64) error {
	if size%AlignSize != 0 {
		return errors.Errorf("zero region size %d is not aligned to %d", size, AlignSize)
	}

	const chunkSize = 4 << 20
	buf := make([]byte, chunkSize)

	for written := int64(0); written < size; {
		n := size - written
		if n > chunkSize {
			n = chunkSize
		}
		if _, err := f.WriteAt(buf[:n], offset+written); err != nil {
			return errors.Wrapf(err, "failed to zero region at offset %d", offset+written)
		}
		written += n
	}

	return nil
}

// recordWriter 按小端序往记录缓冲区写字段。
type recordWriter struct {
	buf []byte
	pos int
}

func (w *recordWriter) uint32(v uint32) error {
	if w.pos+4 > len(w.buf) {
		return errors.Errorf("record overflow: need 4 bytes at %d, capacity %d", w.pos, len(w.buf))
	}
	binary.LittleEndian.PutUint32(w.buf[w.pos:], v)
	w.pos += 4
	return nil
}

func (w *recordWriter) uint64(v uint64) error {
	if w.pos+8 > len(w.buf) {
		return errors.Errorf("record overflow: need 8 bytes at %d, capacity %d", w.pos, len(w.buf))
	}
	binary.LittleEndian.PutUint64(w.buf[w.pos:], v)
	w.pos += 8
	return nil
}

func (w *recordWriter) raw(b []byte) error {
	if w.pos+len(b) > len(w.buf) {
		return errors.Errorf("record overflow: need %d bytes at %d, capacity %d", len(b), w.pos, len(w.buf))
	}
	copy(w.buf[w.pos:], b)
	w.pos += len(b)
	return nil
}

// recordReader 按小端序从记录缓冲区读字段，越界一律返回错误而不是 panic。
type recordReader struct {
	buf []byte
	pos int
}

func (r *recordReader) need(n int) error {
	if n < 0 || r.pos+n > len(r.buf) {
		return errors.Errorf("record truncated: need %d bytes at %d, have %d", n, r.pos, len(r.buf))
	}
	return nil
}

func (r *recordReader) uint32() (uint32, error) {
	if err := r.need(4); err != nil {
		return 0, err
	}
	v := binary.LittleEndian.Uint32(r.buf[r.pos:])
	r.pos += 4
	return v, nil
}

func (r *recordReader) uint64() (uint64, error) {
	if err := r.need(8); err != nil {
		return 0, err
	}
	v := binary.LittleEndian.Uint64(r.buf[r.pos:])
	r.pos += 8
	return v, nil
}

func (r *recordReader) raw(dst []byte) error {
	if err := r.need(len(dst)); err != nil {
		return err
	}
	copy(dst, r.buf[r.pos:])
	r.pos += len(dst)
	return nil
}

// openRecord 解析记录开头的 TotalSize 前缀，校验其上下界，
// 并把读取范围收敛到本条记录内部。返回读取器与 TotalSize，
// 调用方据此解码记录体。
func openRecord(buf []byte, minBinSize int) (*recordReader, uint32, error) {
	if len(buf) < minBinSize {
		return nil, 0, errors.Errorf("record buffer too small: %d < %d", len(buf), minBinSize)
	}

	r := &recordReader{buf: buf}
	totalSize, err := r.uint32()
	if err != nil {
		return nil, 0, err
	}
	if totalSize < uint32(minBinSize) {
		return nil, 0, errors.Errorf("TotalSize %d < minimum %d", totalSize, minBinSize)
	}
	if uint64(totalSize) > uint64(len(buf)) {
		return nil, 0, errors.Errorf("TotalSize %d exceeds buffer %d", totalSize, len(buf))
	}
	// 收敛读取范围，防止损坏的计数字段越界读到下一条记录
	r.buf = buf[:totalSize]
	return r, totalSize, nil
}

// packDiskID 将 DiskID 编码到记录缓冲区。
func packDiskID(w *recordWriter, d *DiskID) error {
	if err := w.raw(d.ID[:]); err != nil {
		return err
	}
	if err := w.uint32(d.Major); err != nil {
		return err
	}
	return w.uint32(d.Minor)
}

// unpackDiskID 从记录缓冲区解码 DiskID。
func unpackDiskID(r *recordReader, d *DiskID) error {
	if err := r.raw(d.ID[:]); err != nil {
		return err
	}
	major, err := r.uint32()
	if err != nil {
		return err
	}
	minor, err := r.uint32()
	if err != nil {
		return err
	}
	d.Major = major
	d.Minor = minor
	return nil
}

// packDiskExtent 将 DiskExtent 编码到记录缓冲区。
func packDiskExtent(w *recordWriter, de *DiskExtent) error {
	if err := packDiskID(w, &de.DiskID); err != nil {
		return err
	}
	if err := w.uint64(de.Start); err != nil {
		return err
	}
	return w.uint64(de.Size)
}

// unpackDiskExtent 从记录缓冲区解码 DiskExtent。
func unpackDiskExtent(r *recordReader, de *DiskExtent) error {
	if err := unpackDiskID(r, &de.DiskID); err != nil {
		return err
	}
	start, err := r.uint64()
	if err != nil {
		return err
	}
	size, err := r.uint64()
	if err != nil {
		return err
	}
	de.Start = start
	de.Size = size
	return nil
}

// calcDiskBitmapBinSize 计算 DiskBitmap 记录的二进制大小（含 TotalSize 前缀）。
//
// BitmapExtents 直接复用 DiskExtent 的编码，每项 DiskExtentBinSize 字节。
func calcDiskBitmapBinSize(db *DiskBitmap) int {
	return DiskBitmapMinBinSize + len(db.BitmapExtents)*DiskExtentBinSize
}

// packDiskBitmap 将 DiskBitmap 编码为变长记录（含 TotalSize 前缀）。
//
// 格式：TotalSize + DiskID + BitmapUnitStart + BitmapUnitCount + BitmapExtentCount + BitmapExtents...
func packDiskBitmap(db *DiskBitmap) ([]byte, error) {
	buf := make([]byte, calcDiskBitmapBinSize(db))
	w := recordWriter{buf: buf}

	if err := w.uint32(uint32(len(buf))); err != nil {
		return nil, err
	}
	if err := packDiskBitmapTo(&w, db); err != nil {
		return nil, err
	}
	return buf, nil
}

// packDiskBitmapTo 将 DiskBitmap 编码到记录缓冲区（不含 TotalSize 前缀）。
func packDiskBitmapTo(w *recordWriter, db *DiskBitmap) error {
	if err := packDiskID(w, &db.DiskID); err != nil {
		return err
	}
	if err := w.uint64(db.BitmapUnitStart); err != nil {
		return err
	}
	if err := w.uint64(db.BitmapUnitCount); err != nil {
		return err
	}
	if err := w.uint32(uint32(len(db.BitmapExtents))); err != nil {
		return err
	}
	for i := range db.BitmapExtents {
		if err := packDiskExtent(w, &db.BitmapExtents[i]); err != nil {
			return errors.Wrapf(err, "bitmap extent %d", i)
		}
	}
	return nil
}

// unpackDiskBitmap 从变长记录缓冲区解码 DiskBitmap。
func unpackDiskBitmap(buf []byte, db *DiskBitmap) error {
	r, totalSize, err := openRecord(buf, DiskBitmapMinBinSize)
	if err != nil {
		return err
	}

	if err := unpackDiskBitmapFrom(r, db); err != nil {
		return err
	}

	if r.pos != int(totalSize) {
		return errors.Errorf("record size mismatch: TotalSize %d, decoded %d bytes", totalSize, r.pos)
	}

	return nil
}

// unpackDiskBitmapFrom 从记录缓冲区解码 DiskBitmap（不含 TotalSize 前缀）。
func unpackDiskBitmapFrom(r *recordReader, db *DiskBitmap) error {
	if err := unpackDiskID(r, &db.DiskID); err != nil {
		return err
	}

	var err error
	if db.BitmapUnitStart, err = r.uint64(); err != nil {
		return err
	}
	if db.BitmapUnitCount, err = r.uint64(); err != nil {
		return err
	}

	count, err := r.uint32()
	if err != nil {
		return err
	}
	// 单个 Extent 不可能比整条记录还大，用它挡掉损坏的 count 触发的巨额分配
	if uint64(count)*uint64(DiskExtentBinSize) > uint64(len(r.buf)) {
		return errors.Errorf("BitmapExtentCount %d exceeds record size %d", count, len(r.buf))
	}
	db.BitmapExtentCount = count
	db.BitmapExtents = make([]DiskExtent, 0, count)
	for i := uint32(0); i < count; i++ {
		var be DiskExtent
		if err := unpackDiskExtent(r, &be); err != nil {
			return errors.Wrapf(err, "bitmap extent %d", i)
		}
		db.BitmapExtents = append(db.BitmapExtents, be)
	}

	return nil
}

// calcProtectedDeviceBinSize 计算 ProtectedDevice 记录的二进制大小（含 TotalSize 前缀）。
func calcProtectedDeviceBinSize(pd *ProtectedDevice) int {
	size := ProtectedDeviceMinBinSize
	size += len(pd.Extents) * DiskExtentBinSize
	return size
}

// packProtectedDevice 将 ProtectedDevice 编码为变长记录。
//
// 格式：TotalSize (uint32，含自身 4 字节) + Type + DeviceID + ExtentCount + Extents(DiskExtent)...
func packProtectedDevice(pd *ProtectedDevice) ([]byte, error) {
	buf := make([]byte, calcProtectedDeviceBinSize(pd))
	w := recordWriter{buf: buf}

	if err := w.uint32(uint32(len(buf))); err != nil {
		return nil, err
	}
	if err := w.uint32(uint32(pd.Type)); err != nil {
		return nil, err
	}
	if err := w.raw(pd.DeviceID[:]); err != nil {
		return nil, err
	}
	if err := w.uint32(uint32(len(pd.Extents))); err != nil {
		return nil, err
	}
	for i := range pd.Extents {
		if err := packDiskExtent(&w, &pd.Extents[i]); err != nil {
			return nil, errors.Wrapf(err, "extent %d", i)
		}
	}

	return buf, nil
}

// unpackProtectedDevice 从变长记录缓冲区解码 ProtectedDevice。
//
// buf 至少要覆盖记录自身的 TotalSize，读取范围会被限定在 TotalSize 之内，
// 因此损坏的计数字段不可能读到下一条记录或缓冲区之外。
func unpackProtectedDevice(buf []byte, pd *ProtectedDevice) error {
	r, totalSize, err := openRecord(buf, ProtectedDeviceMinBinSize)
	if err != nil {
		return err
	}

	devType, err := r.uint32()
	if err != nil {
		return err
	}
	pd.Type = DeviceType(devType)

	if err := r.raw(pd.DeviceID[:]); err != nil {
		return err
	}

	extentCount, err := r.uint32()
	if err != nil {
		return err
	}
	// 每个 Extent 固定 DiskExtentBinSize 字节，用它挡掉损坏的 count
	if uint64(extentCount)*uint64(DiskExtentBinSize) > uint64(totalSize) {
		return errors.Errorf("ExtentCount %d exceeds record size %d", extentCount, totalSize)
	}
	pd.ExtentCount = extentCount
	pd.Extents = make([]DiskExtent, 0, extentCount)

	for i := uint32(0); i < extentCount; i++ {
		var de DiskExtent
		if err := unpackDiskExtent(r, &de); err != nil {
			return errors.Wrapf(err, "extent %d", i)
		}
		pd.Extents = append(pd.Extents, de)
	}

	if r.pos != int(totalSize) {
		return errors.Errorf("record size mismatch: TotalSize %d, decoded %d bytes", totalSize, r.pos)
	}

	return nil
}

// parseRecordRegion 按 TotalSize 前缀顺序解析整个变长记录区。
// unpack 负责解码单条记录（含 TotalSize 前缀），minBinSize 为单条记录的最小大小。
func parseRecordRegion[T any](region []byte, count uint32, minBinSize int, unpack func([]byte, *T) error) ([]T, error) {
	records := make([]T, 0, count)

	pos := 0
	for i := uint32(0); i < count; i++ {
		if pos+4 > len(region) {
			return nil, errors.Errorf("region truncated: record %d starts at %d, region is %d bytes",
				i, pos, len(region))
		}

		totalSize := int(binary.LittleEndian.Uint32(region[pos:]))
		if totalSize < minBinSize {
			return nil, errors.Errorf("record %d: TotalSize %d < minimum %d",
				i, totalSize, minBinSize)
		}
		if pos+totalSize > len(region) {
			return nil, errors.Errorf("record %d: TotalSize %d exceeds region (%d bytes remaining)",
				i, totalSize, len(region)-pos)
		}

		var rec T
		if err := unpack(region[pos:pos+totalSize], &rec); err != nil {
			return nil, errors.Wrapf(err, "record %d", i)
		}
		records = append(records, rec)

		pos += totalSize
	}

	return records, nil
}

// parseProtectedRegion 按 TotalSize 前缀顺序解析整个 Protected Region。
func parseProtectedRegion(region []byte, count uint32) ([]ProtectedDevice, error) {
	return parseRecordRegion(region, count, ProtectedDeviceMinBinSize, unpackProtectedDevice)
}

// parseDiskBitmapRegion 按 TotalSize 前缀顺序解析整个 Disk Bitmap Allocation Region。
func parseDiskBitmapRegion(region []byte, count uint32) ([]DiskBitmap, error) {
	return parseRecordRegion(region, count, DiskBitmapMinBinSize, unpackDiskBitmap)
}

// isDevicePath 判断路径是否为 Windows 设备路径（如 \\.\PHYSICALDRIVE0）。
// 设备路径不应使用 O_CREATE 标志打开。
func isDevicePath(path string) bool {
	return strings.HasPrefix(path, `\\.\`) || strings.HasPrefix(path, `//./`)
}
