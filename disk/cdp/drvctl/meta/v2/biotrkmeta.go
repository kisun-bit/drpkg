package biotrkmeta

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
func Create(file string, offset int64) (*BioTrkMetadata, error) {
	flags := xutil.MetaOpenFlags
	if !isDevicePath(file) {
		flags |= os.O_CREATE
	}
	f, err := os.OpenFile(file, flags, 0644)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create metadata file")
	}

	h := defaultHeader()
	h.ProtectedRegionOffset = HeaderSize
	h.ProtectedRegionSize = 0
	h.ProtectedDeviceCount = 0
	// 为 Protected Region 预分配最大设备数空间，避免后续添加设备时与 Allocation Map 重叠
	h.BitmapAllocMapOffset = h.ProtectedRegionOffset + uint64(DefaultMaxProtectedDevices)*ProtectedDeviceRecordSize
	h.BitmapAllocMapSize = uint64((h.TotalBitmapUnits + 7) / 8)
	h.BitmapDataOffset = h.BitmapAllocMapOffset + h.BitmapAllocMapSize

	h.HeaderCRC32 = calcCRC32(&h)

	// 预分配文件总大小（普通文件需要，物理设备上此调用预计失败，非致命）
	totalSize := offset + int64(h.BitmapDataOffset) + int64(h.BitmapClusterSize)*int64(h.TotalBitmapUnits)
	_ = f.Truncate(totalSize)

	// 写入 Header
	if err := writeHeader(f, offset, &h); err != nil {
		f.Close()
		return nil, err
	}

	// 写入 Allocation Map（全零）
	allocMap := make([]byte, h.BitmapAllocMapSize)
	if _, err := f.WriteAt(allocMap, offset+int64(h.BitmapAllocMapOffset)); err != nil {
		f.Close()
		return nil, errors.Wrap(err, "failed to write allocation map")
	}

	return &BioTrkMetadata{
		file:     f,
		filePath: file,
		offset:   offset,
		header:   h,
		devices:  nil,
		allocMap: allocMap,
	}, nil
}

// Load 从文件的指定偏移处加载元数据文件。
func Load(file string, offset int64) (*BioTrkMetadata, error) {
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

	// 读取 ProtectedDevice 记录
	devices := make([]ProtectedDevice, h.ProtectedDeviceCount)
	for i := uint32(0); i < h.ProtectedDeviceCount; i++ {
		devOffset := offset + int64(h.ProtectedRegionOffset) + int64(i)*int64(ProtectedDeviceRecordSize)
		if err := readProtectedDevice(f, devOffset, &devices[i]); err != nil {
			f.Close()
			return nil, errors.Wrapf(err, "failed to read protected device %d", i)
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
		file:     f,
		filePath: file,
		offset:   offset,
		header:   *h,
		devices:  devices,
		allocMap: allocMap,
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
func (bm *BioTrkMetadata) AddProtectDevice(devType DeviceType, devID ID, extents []DiskExtent) error {
	// 检查是否已存在同名设备
	for i := range bm.devices {
		if bm.devices[i].isValid() && bytes.Equal(bm.devices[i].DeviceID[:], devID[:]) {
			return errors.Errorf("device %s already exists", trimZeroString(devID[:]))
		}
	}

	if len(extents) == 0 {
		return errors.New("extents must not be empty")
	}
	if len(extents) > MaxExtentsPerDevice {
		return errors.Errorf("too many extents: %d > %d", len(extents), MaxExtentsPerDevice)
	}

	// 检查是否超出预分配的保护设备记录空间
	maxRegionSize := bm.header.BitmapAllocMapOffset - bm.header.ProtectedRegionOffset
	if uint64(len(bm.devices)+1)*ProtectedDeviceRecordSize > maxRegionSize {
		return errors.Errorf("protected device limit reached: max %d devices",
			maxRegionSize/ProtectedDeviceRecordSize)
	}

	// 为每个 Extent 分配 Bitmap Unit
	pd := ProtectedDevice{
		Type:     devType,
		DeviceID: devID,
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
			IsValid:         true,
			Extent:          ext,
			BitmapUnitStart: startUnit,
			BitmapUnitCount: neededUnits,
		}
	}

	bm.devices = append(bm.devices, pd)
	bm.header.ProtectedDeviceCount = uint32(len(bm.devices))
	bm.header.ProtectedRegionSize = uint64(len(bm.devices)) * ProtectedDeviceRecordSize

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
		if ext.IsValid {
			bm.freeBitmapUnits(ext.BitmapUnitStart, ext.BitmapUnitCount)
		}
	}

	// 移除设备记录
	bm.devices = append(bm.devices[:idx], bm.devices[idx+1:]...)
	bm.header.ProtectedDeviceCount = uint32(len(bm.devices))
	bm.header.ProtectedRegionSize = uint64(len(bm.devices)) * ProtectedDeviceRecordSize

	return nil
}

// RemoveAllProtectDevice 移除所有受保护设备。
func (bm *BioTrkMetadata) RemoveAllProtectDevice() error {
	for i := range bm.devices {
		for _, ext := range bm.devices[i].Extents {
			if ext.IsValid {
				bm.freeBitmapUnits(ext.BitmapUnitStart, ext.BitmapUnitCount)
			}
		}
	}
	bm.devices = nil
	bm.header.ProtectedDeviceCount = 0
	bm.header.ProtectedRegionSize = 0
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
		return 0, nil, errors.Errorf("device %s not found", trimZeroString(devID[:]))
	}

	bitsPerUnit := uint64(bm.header.BitmapClusterSize) * 8
	unitBytes := int64(bm.header.BitmapClusterSize)

	// 计算总有效 bit 数
	var totalBits uint64
	for i := range pd.Extents {
		if pd.Extents[i].IsValid {
			totalBits += (pd.Extents[i].Extent.Size + bm.header.BitIndexSpace - 1) / bm.header.BitIndexSpace
		}
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
		if !pe.IsValid {
			continue
		}

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

// Flush 将内存中的元数据持久化到磁盘。
func (bm *BioTrkMetadata) Flush() error {
	// 1. 写入 ProtectedDevice 记录
	for i := range bm.devices {
		devOffset := bm.offset + int64(bm.header.ProtectedRegionOffset) + int64(i)*int64(ProtectedDeviceRecordSize)
		if err := writeProtectedDevice(bm.file, devOffset, &bm.devices[i]); err != nil {
			return errors.Wrapf(err, "failed to write protected device %d", i)
		}
	}

	// 2. 写入 Allocation Map
	if _, err := bm.file.WriteAt(bm.allocMap, bm.offset+int64(bm.header.BitmapAllocMapOffset)); err != nil {
		return errors.Wrap(err, "failed to write allocation map")
	}

	// 3. 更新 CRC32 并写入 Header
	bm.header.HeaderCRC32 = calcCRC32(&bm.header)
	if err := writeHeader(bm.file, bm.offset, &bm.header); err != nil {
		return err
	}

	return bm.file.Sync()
}

// Size 返回元数据区域总大小（字节）。
func (bm *BioTrkMetadata) Size() int64 {
	return int64(bm.header.BitmapDataOffset) + int64(bm.header.BitmapClusterSize)*int64(bm.header.TotalBitmapUnits)
}

// PhysicalExtents 返回元数据区域在物理磁盘上的存储区域。
//
// 对于普通文件，通过文件系统查询文件所占的物理磁盘区间，截取 [offset, offset+size) 范围。
// 对于设备路径（如 \\.\PHYSICALDRIVE0），直接返回设备上 offset 处的单一区间。
func (bm *BioTrkMetadata) PhysicalExtents() ([]xutil.FileDiskExtentSegment, error) {
	size := bm.Size()

	// 设备路径：元数据直接位于设备上
	if isDevicePath(bm.filePath) {
		return []xutil.FileDiskExtentSegment{
			{Disk: bm.filePath, Start: bm.offset, Size: size},
		}, nil
	}

	// 普通文件：查询文件系统级物理区间，截取元数据区域
	allExtents, err := xutil.FileDiskExtents(bm.filePath)
	if err != nil {
		return nil, err
	}

	metaStart := bm.offset
	metaEnd := bm.offset + size
	var result []xutil.FileDiskExtentSegment
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
		result = append(result, xutil.FileDiskExtentSegment{
			Disk:  e.Disk,
			Start: physStart,
			Size:  clipEnd - clipStart,
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

// defaultHeader 创建默认 Header。
func defaultHeader() Header {
	h := Header{
		Version:           VersionV1_0,
		BitIndexSpace:     DefaultBitIndexSpace,
		BitmapClusterSize: DefaultBitmapClusterSize,
		TotalBitmapUnits:  DefaultTotalBitmapUnits,
	}
	copy(h.Signature[:], SignatureStr)
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
func validateHeader(f *os.File, offset int64, h *Header) error {
	// Signature
	if trimZeroString(h.Signature[:]) != SignatureStr {
		return errors.Errorf("signature mismatch: got %q, expected %q", trimZeroString(h.Signature[:]), SignatureStr)
	}

	// Version
	if h.Version != VersionV1_0 {
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

	// ProtectedRegion
	expectedRegionSize := uint64(h.ProtectedDeviceCount) * ProtectedDeviceRecordSize
	if h.ProtectedRegionSize != expectedRegionSize {
		return errors.Errorf("ProtectedRegionSize mismatch: %d != %d", h.ProtectedRegionSize, expectedRegionSize)
	}

	// 布局约束
	if h.ProtectedRegionOffset < HeaderSize {
		return errors.New("ProtectedRegionOffset < HeaderSize")
	}
	if h.ProtectedRegionOffset+h.ProtectedRegionSize > h.BitmapAllocMapOffset {
		return errors.New("protected region overlaps allocation map")
	}

	// Allocation Map
	minAllocMapSize := uint64((h.TotalBitmapUnits + 7) / 8)
	if h.BitmapAllocMapSize < minAllocMapSize {
		return errors.Errorf("BitmapAllocMapSize too small: %d < %d", h.BitmapAllocMapSize, minAllocMapSize)
	}

	if h.BitmapAllocMapOffset+h.BitmapAllocMapSize > h.BitmapDataOffset {
		return errors.New("allocation map overlaps bitmap data")
	}

	// Bitmap Data
	expectedDataSize := uint64(h.BitmapClusterSize) * uint64(h.TotalBitmapUnits)
	// 设备路径上 Stat 不可用，跳过大小检查（设备容量必然足够）
	if !isDevicePath(f.Name()) {
		stat, err := f.Stat()
		if err != nil {
			return errors.Wrap(err, "failed to stat file")
		}
		if uint64(stat.Size()) < uint64(offset)+h.BitmapDataOffset+expectedDataSize {
			return errors.New("file too small for bitmap data region")
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
		if !pe.IsValid {
			continue
		}

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
		for i := 0; i < MaxExtentsPerDevice; i++ {
			if !pd.Extents[i].IsValid {
				continue
			}
			aStart := pd.Extents[i].BitmapUnitStart
			aEnd := aStart + pd.Extents[i].BitmapUnitCount
			for j := i + 1; j < MaxExtentsPerDevice; j++ {
				if !pd.Extents[j].IsValid {
					continue
				}
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

// isBitmapUnitAllocated 检查 Bitmap Unit 是否已分配。
func isBitmapUnitAllocated(allocMap []byte, unit uint64) bool {
	byteIdx := unit / 8
	bitIdx := unit % 8
	return allocMap[byteIdx]&(1<<bitIdx) != 0
}

// setBitmapUnitAllocated 设置 Bitmap Unit 分配状态。
func setBitmapUnitAllocated(allocMap []byte, unit uint64, allocated bool) {
	byteIdx := unit / 8
	bitIdx := unit % 8
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
func (pd *ProtectedDevice) isValid() bool {
	return (pd.Type == DeviceTypeDisk || pd.Type == DeviceTypeVolume) &&
		!isEmptyDeviceID(pd.DeviceID)
}

// packDiskID 将 DiskID 编码为二进制。
func packDiskID(w *bytes.Buffer, d *DiskID) {
	binary.Write(w, binary.LittleEndian, d.ID)
	binary.Write(w, binary.LittleEndian, d.Major)
	binary.Write(w, binary.LittleEndian, d.Minor)
}

// unpackDiskID 从二进制解码 DiskID。
func unpackDiskID(r *bytes.Reader, d *DiskID) error {
	if err := binary.Read(r, binary.LittleEndian, &d.ID); err != nil {
		return err
	}
	if err := binary.Read(r, binary.LittleEndian, &d.Major); err != nil {
		return err
	}
	return binary.Read(r, binary.LittleEndian, &d.Minor)
}

// packDiskExtent 将 DiskExtent 编码为二进制。
func packDiskExtent(w *bytes.Buffer, de *DiskExtent) {
	packDiskID(w, &de.DiskID)
	binary.Write(w, binary.LittleEndian, de.Start)
	binary.Write(w, binary.LittleEndian, de.Size)
}

// unpackDiskExtent 从二进制解码 DiskExtent。
func unpackDiskExtent(r *bytes.Reader, de *DiskExtent) error {
	if err := unpackDiskID(r, &de.DiskID); err != nil {
		return err
	}
	if err := binary.Read(r, binary.LittleEndian, &de.Start); err != nil {
		return err
	}
	return binary.Read(r, binary.LittleEndian, &de.Size)
}

// packProtectedExtent 将 ProtectedExtent 编码为二进制。
func packProtectedExtent(w *bytes.Buffer, pe *ProtectedExtent) {
	var isValid uint32
	if pe.IsValid {
		isValid = 1
	}
	binary.Write(w, binary.LittleEndian, isValid)
	packDiskExtent(w, &pe.Extent)
	binary.Write(w, binary.LittleEndian, pe.BitmapUnitStart)
	binary.Write(w, binary.LittleEndian, pe.BitmapUnitCount)
}

// unpackProtectedExtent 从二进制解码 ProtectedExtent。
func unpackProtectedExtent(r *bytes.Reader, pe *ProtectedExtent) error {
	var isValid uint32
	if err := binary.Read(r, binary.LittleEndian, &isValid); err != nil {
		return err
	}
	pe.IsValid = isValid == 1
	if err := unpackDiskExtent(r, &pe.Extent); err != nil {
		return err
	}
	if err := binary.Read(r, binary.LittleEndian, &pe.BitmapUnitStart); err != nil {
		return err
	}
	return binary.Read(r, binary.LittleEndian, &pe.BitmapUnitCount)
}

// packProtectedDevice 将 ProtectedDevice 编码为二进制。
func packProtectedDevice(w *bytes.Buffer, pd *ProtectedDevice) {
	binary.Write(w, binary.LittleEndian, uint32(pd.Type))
	binary.Write(w, binary.LittleEndian, pd.DeviceID)
	for i := range pd.Extents {
		packProtectedExtent(w, &pd.Extents[i])
	}
	binary.Write(w, binary.LittleEndian, pd._padding)
}

// unpackProtectedDevice 从二进制解码 ProtectedDevice。
func unpackProtectedDevice(r *bytes.Reader, pd *ProtectedDevice) error {
	var devType uint32
	if err := binary.Read(r, binary.LittleEndian, &devType); err != nil {
		return err
	}
	pd.Type = DeviceType(devType)
	if err := binary.Read(r, binary.LittleEndian, &pd.DeviceID); err != nil {
		return err
	}
	for i := range pd.Extents {
		if err := unpackProtectedExtent(r, &pd.Extents[i]); err != nil {
			return err
		}
	}
	return binary.Read(r, binary.LittleEndian, &pd._padding)
}

// writeProtectedDevice 将 ProtectedDevice 写入文件指定偏移。
func writeProtectedDevice(f *os.File, offset int64, pd *ProtectedDevice) error {
	buf := new(bytes.Buffer)
	packProtectedDevice(buf, pd)
	if buf.Len() != ProtectedDeviceRecordSize {
		return errors.Errorf("protected device record size mismatch: %d != %d", buf.Len(), ProtectedDeviceRecordSize)
	}
	_, err := f.WriteAt(buf.Bytes(), offset)
	return err
}

// readProtectedDevice 从文件指定偏移读取 ProtectedDevice。
func readProtectedDevice(f *os.File, offset int64, pd *ProtectedDevice) error {
	buf := make([]byte, ProtectedDeviceRecordSize)
	if _, err := f.ReadAt(buf, offset); err != nil {
		return err
	}
	return unpackProtectedDevice(bytes.NewReader(buf), pd)
}

// isDevicePath 判断路径是否为 Windows 设备路径（如 \\.\PHYSICALDRIVE0）。
// 设备路径不应使用 O_CREATE 标志打开。
func isDevicePath(path string) bool {
	return strings.HasPrefix(path, `\\.\`) || strings.HasPrefix(path, `//./`)
}
