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

	// 校验 ProtectedDevice 一致性
	for i := range devices {
		if err := validateProtectedDevice(&devices[i], h, allocMap); err != nil {
			f.Close()
			return nil, errors.Wrapf(err, "protected device %d validation failed", i)
		}
	}

	// 校验同一设备内 Bitmap Unit 范围不重叠
	if err := validateBitmapUnitNoOverlap(devices); err != nil {
		f.Close()
		return nil, err
	}

	return &BioTrkMetadata{
		file:            f,
		filePath:        file,
		offset:          offset,
		header:          *h,
		devices:         devices,
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

// AddProtectDevice 添加一个受保护设备。不支持同名 devID 同时存在。
// 分配成功后更新位图单元分配表。
//
// Extents 与设备数量都没有格式层面的上限：记录是变长的，Protected Region 位于
// 元数据区域末尾，增长不会破坏其他区域。但区域会因此变大，调用方需要自行确认
// Size() 仍然落在可用空间内（裸设备上尤其要留意预留范围与磁盘末尾）。
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
				if extentsOverlap(newExt, existingExt.Extent) {
					return errors.Errorf(
						"extent %s overlaps with existing extent %s of device %s",
						newExt.String(), existingExt.Extent.String(),
						xutil.TrimZeroString(existingDev.DeviceID[:]),
					)
				}
			}
		}
	}

	// 为每个 Extent 分配 Bitmap Unit
	pd := ProtectedDevice{
		Type:        devType,
		DeviceID:    devID,
		ExtentCount: uint32(len(extents)),
		Extents:     make([]ProtectedExtent, len(extents)),
	}

	bitmapUnitCapacity := uint64(bm.header.BitmapClusterSize) * 8 * bm.header.BitIndexSpace

	for i, ext := range extents {
		neededUnits := (ext.Size + bitmapUnitCapacity - 1) / bitmapUnitCapacity
		if neededUnits == 0 {
			neededUnits = 1
		}

		startUnit, err := bm.allocBitmapUnits(neededUnits)
		if err != nil {
			// 回滚已分配的
			for j := 0; j < i; j++ {
				bm.freeBitmapUnits(pd.Extents[j].BitmapUnitStart, pd.Extents[j].BitmapUnitCount)
			}
			return errors.Wrapf(err, "failed to allocate bitmap units for extent %d", i)
		}

		pd.Extents[i] = ProtectedExtent{
			Extent:          ext,
			BitmapUnitStart: startUnit,
			BitmapUnitCount: neededUnits,
		}
	}

	bm.devices = append(bm.devices, pd)
	bm.header.ProtectedDeviceCount = uint32(len(bm.devices))

	return nil
}

// RemoveProtectDevice 移除指定设备 ID 的受保护设备。若不存在也返回 nil。
// 删除后需要更新位图单元分配表。
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

	// 释放该设备占用的 Bitmap Unit
	for _, ext := range bm.devices[idx].Extents {
		bm.freeBitmapUnits(ext.BitmapUnitStart, ext.BitmapUnitCount)
	}

	bm.devices = append(bm.devices[:idx], bm.devices[idx+1:]...)
	bm.header.ProtectedDeviceCount = uint32(len(bm.devices))

	return nil
}

// RemoveAllProtectDevice 移除所有受保护设备。
func (bm *BioTrkMetadata) RemoveAllProtectDevice() error {
	for i := range bm.devices {
		for _, ext := range bm.devices[i].Extents {
			bm.freeBitmapUnits(ext.BitmapUnitStart, ext.BitmapUnitCount)
		}
	}
	bm.devices = nil
	bm.header.ProtectedDeviceCount = 0
	return nil
}

// ReadDeviceBitmap 读取指定受保护设备的位图，按 Extent 顺序拼接后返回。
//
// 每个 Extent 实际需要的 bit 数 = ceil(Extent.Size / BitIndexSpace)，
// 可能小于其 BitmapUnit 提供的总 bit 数（最后一个 Unit 有未使用尾部），
// 拼接时只取有效 bit，丢弃多余部分。
//
// 返回的 bitCount 为总有效 bit 数，bitmapData 为 packed bytes（bitCount 不足 8 的倍数时最后一个字节高位补 0）。
func (bm *BioTrkMetadata) ReadDeviceBitmap(devID ID) (bitCount uint32, bitmapData []byte, err error) {
	pd := bm.findDevice(devID)
	if pd == nil {
		return 0, nil, errors.Errorf("device %s not found", xutil.TrimZeroString(devID[:]))
	}

	bitsPerUnit := uint64(bm.header.BitmapClusterSize) * 8
	unitBytes := int64(bm.header.BitmapClusterSize)

	// 计算总有效 bit 数
	var totalBits uint64
	for i := range pd.Extents {
		totalBits += (pd.Extents[i].Extent.Size + bm.header.BitIndexSpace - 1) / bm.header.BitIndexSpace
	}
	if totalBits == 0 {
		return 0, nil, nil
	}

	dataBytes := (totalBits + 7) / 8
	bitmapData = make([]byte, dataBytes)

	var dstBitOff uint64 // 已写入的目标 bit 偏移
	unitBuf := make([]byte, unitBytes)

	for i := range pd.Extents {
		pe := &pd.Extents[i]

		bitsForExtent := (pe.Extent.Size + bm.header.BitIndexSpace - 1) / bm.header.BitIndexSpace
		bitsRemaining := bitsForExtent

		for u := uint64(0); u < pe.BitmapUnitCount && bitsRemaining > 0; u++ {
			unitIdx := pe.BitmapUnitStart + u
			unitOff := bm.offset + int64(bm.header.BitmapDataOffset) + int64(unitIdx)*unitBytes
			if _, err := bm.file.ReadAt(unitBuf, unitOff); err != nil {
				return 0, nil, errors.Wrapf(err, "read bitmap unit %d", unitIdx)
			}

			n := bitsRemaining
			if n > bitsPerUnit {
				n = bitsPerUnit
			}

			bitCopy(bitmapData, dstBitOff, unitBuf, 0, n)
			dstBitOff += n
			bitsRemaining -= n
		}
	}

	return uint32(totalBits), bitmapData, nil
}

// ResolveBitmapExtents 解析每个 ProtectedExtent 的位图数据在物理磁盘上的分布。
//
// 必须在 Flush 之前调用（Flush 内部会自动调用），确保写入磁盘的记录包含正确的
// BitmapExtents。驱动后续基于 BitmapExtents 直接写入物理磁盘更新位图，
// 若数据错误可能导致写到元数据位图数据区之外，损坏磁盘数据。
//
// 算法：
//  1. 获取整个元数据区域的物理磁盘分布（PhysicalExtents）
//  2. 裁剪到位图数据区 [BitmapDataOffset, BitmapDataOffset + BitmapClusterSize*TotalBitmapUnits)
//  3. 对每个 ProtectedExtent，将其 bitmap unit 范围映射到物理 extent
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

	// 为每个 ProtectedExtent 解析 BitmapExtents
	for di := range bm.devices {
		for ei := range bm.devices[di].Extents {
			pe := &bm.devices[di].Extents[ei]

			unitStart := int64(pe.BitmapUnitStart) * clusterSize
			unitEnd := int64(pe.BitmapUnitStart+pe.BitmapUnitCount) * clusterSize

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

			pe.BitmapExtents = extents
			pe.BitmapExtentCount = uint32(len(extents))
		}
	}

	return nil
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

// flushRecords 写入 Protected Region、Allocation Map 和 Header。
//
// 所有设备记录打包成一个整体、末尾补齐到 AlignSize 后一次写入：
// 变长记录的单条长度不保证扇区对齐，只有整区读写才能在裸设备上成立。
func (bm *BioTrkMetadata) flushRecords() error {
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

	// 写入 Allocation Map
	if _, err := bm.file.WriteAt(bm.allocMap, bm.offset+int64(bm.header.BitmapAllocMapOffset)); err != nil {
		return errors.Wrap(err, "failed to write allocation map")
	}

	// 更新区域大小与 CRC32，写入 Header
	bm.header.ProtectedRegionSize = alignUp(recordsSize)
	bm.header.ProtectedDeviceCount = uint32(len(bm.devices))
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
	if h.ProtectedRegionOffset != expected.ProtectedRegionOffset {
		return errors.Errorf("ProtectedRegionOffset %d != expected %d",
			h.ProtectedRegionOffset, expected.ProtectedRegionOffset)
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
	for _, v := range []uint64{h.BitmapAllocMapOffset, h.BitmapDataOffset, h.ProtectedRegionOffset} {
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
func validateProtectedDevice(pd *ProtectedDevice, h *Header, allocMap []byte) error {
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
		pe := &pd.Extents[i]

		if pe.Extent.Size == 0 {
			return errors.Errorf("extent %d: Size is 0", i)
		}
		if pe.BitmapUnitCount == 0 {
			return errors.Errorf("extent %d: BitmapUnitCount is 0", i)
		}

		// BitmapUnit 范围不得越界
		if pe.BitmapUnitStart >= uint64(h.TotalBitmapUnits) {
			return errors.Errorf("extent %d: BitmapUnitStart %d >= TotalBitmapUnits %d",
				i, pe.BitmapUnitStart, h.TotalBitmapUnits)
		}
		if pe.BitmapUnitCount > uint64(h.TotalBitmapUnits)-pe.BitmapUnitStart {
			return errors.Errorf("extent %d: BitmapUnit range [%d, %d) exceeds TotalBitmapUnits %d",
				i, pe.BitmapUnitStart, pe.BitmapUnitStart+pe.BitmapUnitCount, h.TotalBitmapUnits)
		}

		// Extent 引用的 Bitmap Unit 必须已分配
		for u := pe.BitmapUnitStart; u < pe.BitmapUnitStart+pe.BitmapUnitCount; u++ {
			if !isBitmapUnitAllocated(allocMap, u) {
				return errors.Errorf("extent %d: bitmap unit %d not allocated", i, u)
			}
		}

		// Bitmap Unit 容量必须足够
		unitCapacity := uint64(h.BitmapClusterSize) * 8 * h.BitIndexSpace
		coveredCapacity := pe.BitmapUnitCount * unitCapacity
		if coveredCapacity < pe.Extent.Size {
			return errors.Errorf("extent %d: bitmap capacity %d < extent size %d",
				i, coveredCapacity, pe.Extent.Size)
		}

		if err := validateBitmapExtents(pe, h); err != nil {
			return errors.Wrapf(err, "extent %d", i)
		}
	}

	return nil
}

// validateBitmapExtents 校验单个 ProtectedExtent 的位图物理分布。
//
// 驱动会照着这份分布直接往物理磁盘写位图，写错位置会损坏磁盘数据，
// 因此这里做完整校验：每段大小非零且按 Bitmap Unit 对齐、
// 各段长度之和恰好覆盖该 Extent 的全部 Bitmap Unit。
//
// BitmapExtents 只在 Flush 之后才有内容，为空表示尚未解析，跳过校验。
func validateBitmapExtents(pe *ProtectedExtent, h *Header) error {
	if len(pe.BitmapExtents) == 0 {
		return nil
	}

	clusterSize := uint64(h.BitmapClusterSize)
	var total uint64
	for i := range pe.BitmapExtents {
		be := &pe.BitmapExtents[i]
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

	expected := pe.BitmapUnitCount * clusterSize
	if total != expected {
		return errors.Errorf("bitmap extents total size %d != expected %d (%d units x %d bytes)",
			total, expected, pe.BitmapUnitCount, clusterSize)
	}

	return nil
}

// validateBitmapUnitNoOverlap 校验同一设备内 Bitmap Unit 范围不重叠。
func validateBitmapUnitNoOverlap(devices []ProtectedDevice) error {
	for di := range devices {
		pd := &devices[di]
		if !pd.isValid() {
			continue
		}
		for i := 0; i < len(pd.Extents); i++ {
			aStart := pd.Extents[i].BitmapUnitStart
			aEnd := aStart + pd.Extents[i].BitmapUnitCount
			for j := i + 1; j < len(pd.Extents); j++ {
				bStart := pd.Extents[j].BitmapUnitStart
				bEnd := bStart + pd.Extents[j].BitmapUnitCount
				if aStart < bEnd && bStart < aEnd {
					return errors.Errorf(
						"device %d: bitmap unit range overlap: [%d, %d) and [%d, %d)",
						di, aStart, aEnd, bStart, bEnd)
				}
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

// calcProtectedExtentBinSize 计算 ProtectedExtent 的二进制大小。
//
// BitmapExtents 直接复用 DiskExtent 的编码，每项 DiskExtentBinSize 字节。
func calcProtectedExtentBinSize(pe *ProtectedExtent) int {
	return ProtectedExtentFixedBinSize + len(pe.BitmapExtents)*DiskExtentBinSize
}

// packProtectedExtent 将 ProtectedExtent 编码为独立的二进制。
func packProtectedExtent(pe *ProtectedExtent) ([]byte, error) {
	buf := make([]byte, calcProtectedExtentBinSize(pe))
	w := recordWriter{buf: buf}
	if err := packProtectedExtentTo(&w, pe); err != nil {
		return nil, err
	}
	return buf, nil
}

// packProtectedExtentTo 将 ProtectedExtent 编码到记录缓冲区。
func packProtectedExtentTo(w *recordWriter, pe *ProtectedExtent) error {
	if err := packDiskExtent(w, &pe.Extent); err != nil {
		return err
	}
	if err := w.uint64(pe.BitmapUnitStart); err != nil {
		return err
	}
	if err := w.uint64(pe.BitmapUnitCount); err != nil {
		return err
	}
	if err := w.uint32(uint32(len(pe.BitmapExtents))); err != nil {
		return err
	}
	for i := range pe.BitmapExtents {
		if err := packDiskExtent(w, &pe.BitmapExtents[i]); err != nil {
			return errors.Wrapf(err, "bitmap extent %d", i)
		}
	}
	return nil
}

// unpackProtectedExtent 从独立的二进制解码 ProtectedExtent。
func unpackProtectedExtent(buf []byte, pe *ProtectedExtent) error {
	r := recordReader{buf: buf}
	return unpackProtectedExtentFrom(&r, pe)
}

// unpackProtectedExtentFrom 从记录缓冲区解码 ProtectedExtent。
func unpackProtectedExtentFrom(r *recordReader, pe *ProtectedExtent) error {
	if err := unpackDiskExtent(r, &pe.Extent); err != nil {
		return err
	}

	var err error
	if pe.BitmapUnitStart, err = r.uint64(); err != nil {
		return err
	}
	if pe.BitmapUnitCount, err = r.uint64(); err != nil {
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
	pe.BitmapExtentCount = count
	pe.BitmapExtents = make([]DiskExtent, 0, count)
	for i := uint32(0); i < count; i++ {
		var be DiskExtent
		if err := unpackDiskExtent(r, &be); err != nil {
			return errors.Wrapf(err, "bitmap extent %d", i)
		}
		pe.BitmapExtents = append(pe.BitmapExtents, be)
	}

	return nil
}

// calcProtectedDeviceBinSize 计算 ProtectedDevice 记录的二进制大小（含 TotalSize 前缀）。
func calcProtectedDeviceBinSize(pd *ProtectedDevice) int {
	size := ProtectedDeviceMinBinSize
	for i := range pd.Extents {
		size += calcProtectedExtentBinSize(&pd.Extents[i])
	}
	return size
}

// packProtectedDevice 将 ProtectedDevice 编码为变长记录。
//
// 格式：TotalSize (uint32，含自身 4 字节) + Type + DeviceID + ExtentCount + Extents...
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
		if err := packProtectedExtentTo(&w, &pd.Extents[i]); err != nil {
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
	if len(buf) < ProtectedDeviceMinBinSize {
		return errors.Errorf("record buffer too small: %d < %d", len(buf), ProtectedDeviceMinBinSize)
	}

	r := recordReader{buf: buf}

	totalSize, err := r.uint32()
	if err != nil {
		return err
	}
	if totalSize < ProtectedDeviceMinBinSize {
		return errors.Errorf("TotalSize %d < minimum %d", totalSize, ProtectedDeviceMinBinSize)
	}
	if uint64(totalSize) > uint64(len(buf)) {
		return errors.Errorf("TotalSize %d exceeds buffer %d", totalSize, len(buf))
	}
	// 把读取范围收敛到本条记录，防止越界读到下一条
	r.buf = buf[:totalSize]

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
	// 每个 Extent 至少 ProtectedExtentFixedBinSize 字节，用它挡掉损坏的 count
	if uint64(extentCount)*uint64(ProtectedExtentFixedBinSize) > uint64(totalSize) {
		return errors.Errorf("ExtentCount %d exceeds record size %d", extentCount, totalSize)
	}
	pd.ExtentCount = extentCount
	pd.Extents = make([]ProtectedExtent, 0, extentCount)

	for i := uint32(0); i < extentCount; i++ {
		var pe ProtectedExtent
		if err := unpackProtectedExtentFrom(&r, &pe); err != nil {
			return errors.Wrapf(err, "extent %d", i)
		}
		pd.Extents = append(pd.Extents, pe)
	}

	if r.pos != int(totalSize) {
		return errors.Errorf("record size mismatch: TotalSize %d, decoded %d bytes", totalSize, r.pos)
	}

	return nil
}

// parseProtectedRegion 按 TotalSize 前缀顺序解析整个 Protected Region。
func parseProtectedRegion(region []byte, count uint32) ([]ProtectedDevice, error) {
	devices := make([]ProtectedDevice, 0, count)

	pos := 0
	for i := uint32(0); i < count; i++ {
		if pos+4 > len(region) {
			return nil, errors.Errorf("region truncated: record %d starts at %d, region is %d bytes",
				i, pos, len(region))
		}

		totalSize := int(binary.LittleEndian.Uint32(region[pos:]))
		if totalSize < ProtectedDeviceMinBinSize {
			return nil, errors.Errorf("record %d: TotalSize %d < minimum %d",
				i, totalSize, ProtectedDeviceMinBinSize)
		}
		if pos+totalSize > len(region) {
			return nil, errors.Errorf("record %d: TotalSize %d exceeds region (%d bytes remaining)",
				i, totalSize, len(region)-pos)
		}

		var pd ProtectedDevice
		if err := unpackProtectedDevice(region[pos:pos+totalSize], &pd); err != nil {
			return nil, errors.Wrapf(err, "record %d", i)
		}
		devices = append(devices, pd)

		pos += totalSize
	}

	return devices, nil
}

// isDevicePath 判断路径是否为 Windows 设备路径（如 \\.\PHYSICALDRIVE0）。
// 设备路径不应使用 O_CREATE 标志打开。
func isDevicePath(path string) bool {
	return strings.HasPrefix(path, `\\.\`) || strings.HasPrefix(path, `//./`)
}
