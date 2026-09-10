package journal

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/kisun-bit/drpkg/cdp/drvctl/v2/ioctl"
	biotrkmeta "github.com/kisun-bit/drpkg/cdp/drvctl/v2/meta"
	"github.com/kisun-bit/drpkg/disk/image/hkc"
	"github.com/kisun-bit/drpkg/rpc/aio/proto"
	"github.com/lunixbochs/struc"
)

// ============================================================================
// 辅助函数
// ============================================================================

func makeDeviceID(s string) [biotrkmeta.DeviceIDLen]byte {
	var id [biotrkmeta.DeviceIDLen]byte
	copy(id[:], s)
	return id
}

func makeDiskExtent(diskID string, start, size uint64) biotrkmeta.DiskExtent {
	var dkID biotrkmeta.DiskID
	copy(dkID.ID[:], diskID)
	return biotrkmeta.DiskExtent{DiskID: dkID, Start: start, Size: size}
}

func makeProtectedDevice(typ biotrkmeta.DeviceType, name string, extents ...biotrkmeta.ProtectedExtent) biotrkmeta.ProtectedDevice {
	dev := biotrkmeta.ProtectedDevice{
		Type:    typ,
		Extents: extents,
	}
	copy(dev.DeviceID[:], name)
	dev.ExtentCount = uint32(len(extents))
	return dev
}

func makeProtectedExtent(extent biotrkmeta.DiskExtent, bitmapStart, bitmapCount uint64) biotrkmeta.ProtectedExtent {
	pe := biotrkmeta.ProtectedExtent{
		Extent:          extent,
		BitmapUnitStart: bitmapStart,
		BitmapUnitCount: bitmapCount,
	}
	pe.BitmapExtentCount = 0
	return pe
}

func makeIoRecord(diskID string, offset, length uint64, ioType, consistency uint8) *ioctl.IoRecord {
	var dkID biotrkmeta.DiskID
	copy(dkID.ID[:], diskID)
	data := make([]byte, length)
	if ioType == 0 && length > 0 {
		// 普通 IO：填充模拟数据
		binary.LittleEndian.PutUint64(data, offset)
	}
	return &ioctl.IoRecord{
		Header: ioctl.IoHeader{
			DiskID:      dkID,
			Type:        ioType,
			Consistency: consistency,
			Offset:      offset,
			Length:      length,
			Timestamp:   offset, // 简化：用 offset 作为 timestamp
		},
		Data: data,
	}
}

func makeDiskID(s string) biotrkmeta.DiskID {
	var d biotrkmeta.DiskID
	copy(d.ID[:], s)
	return d
}

// ============================================================================
// MaskString / IsFlagRecordByMask / IsDataRecordByMask
// ============================================================================

func TestMaskString(t *testing.T) {
	tests := []struct {
		mask proto.IOMask
		want string
	}{
		{proto.IOMask_DATA_REALTIME, "DATA_REALTIME"},
		{proto.IOMask_DATA_FULL, "DATA_FULL"},
		{proto.IOMask_DATA_BITMAP, "DATA_BITMAP"},
		{proto.IOMask_FLAG_OPEN_FULL, "FLAG_OPEN_FULL"},
		{999, "UNKNOWN"},
	}

	for _, tt := range tests {
		if got := MaskString(tt.mask); got != tt.want {
			t.Errorf("MaskString(%d) = %q, want %q", tt.mask, got, tt.want)
		}
	}
}

func TestIsFlagRecordByMask(t *testing.T) {
	if !IsFlagRecordByMask(proto.IOMask_FLAG_OPEN_FULL) {
		t.Error("FLAG_OPEN_FULL should be flag")
	}
	if !IsFlagRecordByMask(proto.IOMask_FLAG_CLOSE_FULL) {
		t.Error("FLAG_CLOSE_FULL should be flag")
	}
	if IsFlagRecordByMask(proto.IOMask_DATA_REALTIME) {
		t.Error("DATA_REALTIME should not be flag")
	}
	if IsFlagRecordByMask(proto.IOMask_DATA_BITMAP) {
		t.Error("DATA_BITMAP should not be flag")
	}
}

func TestIsDataRecordByMask(t *testing.T) {
	if !IsDataRecordByMask(proto.IOMask_DATA_REALTIME) {
		t.Error("DATA_REALTIME should be data")
	}
	if !IsDataRecordByMask(proto.IOMask_DATA_FULL) {
		t.Error("DATA_FULL should be data")
	}
	if IsDataRecordByMask(proto.IOMask_FLAG_OPEN_FULL) {
		t.Error("FLAG_OPEN_FULL should not be data")
	}
}

// ============================================================================
// isRangeInExtent
// ============================================================================

func TestIsRangeInExtent(t *testing.T) {
	ext := makeDiskExtent("disk0", 1024, 4096) // [1024, 5120)

	tests := []struct {
		name   string
		off    uint64
		length uint64
		want   bool
	}{
		{"fully inside", 2048, 1024, true},
		{"exact match", 1024, 4096, true},
		{"starts before", 0, 2048, false},
		{"ends after", 4096, 2048, false},
		{"completely outside", 0, 512, false},
		{"zero length", 2048, 0, false},
		{"at end boundary", 5120, 1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRangeInExtent(ext, tt.off, tt.length); got != tt.want {
				t.Errorf("isRangeInExtent(%d, %d) = %v, want %v",
					tt.off, tt.length, got, tt.want)
			}
		})
	}
}

// ============================================================================
// deviceContainsRange
// ============================================================================

func TestDeviceContainsRange(t *testing.T) {
	dev := makeProtectedDevice(biotrkmeta.DeviceTypeDisk, "test-disk",
		makeProtectedExtent(makeDiskExtent("disk0", 0, 4096), 0, 4),
		makeProtectedExtent(makeDiskExtent("disk1", 1048576, 2048), 4, 2),
	)

	disk0 := makeDiskID("disk0")
	disk1 := makeDiskID("disk1")
	disk2 := makeDiskID("disk2")

	if !deviceContainsRange(dev, disk0, 100, 500) {
		t.Error("should contain range on disk0")
	}
	if !deviceContainsRange(dev, disk1, 1048576, 1024) {
		t.Error("should contain range on disk1")
	}
	if deviceContainsRange(dev, disk2, 0, 100) {
		t.Error("should not contain range on disk2")
	}
	if deviceContainsRange(dev, disk0, 4000, 200) {
		t.Error("[4000+200) exceeds extent [0,4096), should be false")
	}
}

// ============================================================================
// classifyShmIO
// ============================================================================

func TestClassifyShmIODiskLevel(t *testing.T) {
	devices := []biotrkmeta.ProtectedDevice{
		makeProtectedDevice(biotrkmeta.DeviceTypeDisk, "disk-A",
			makeProtectedExtent(makeDiskExtent("phys0", 0, 10000), 0, 10),
		),
		makeProtectedDevice(biotrkmeta.DeviceTypeDisk, "disk-B",
			makeProtectedExtent(makeDiskExtent("phys1", 2048, 4096), 0, 4),
		),
	}

	// IO 在 phys0 上，落在 disk-A 的区间内
	io := makeIoRecord("phys0", 500, 256, 0, 1)
	id := classifyShmIO(devices, io)
	if id != "disk-A" {
		t.Errorf("got %q, want disk-A", id)
	}

	// IO 在 phys1 上，落在 disk-B 的区间内
	io = makeIoRecord("phys1", 4096, 512, 0, 1)
	id = classifyShmIO(devices, io)
	if id != "disk-B" {
		t.Errorf("got %q, want disk-B", id)
	}

	// IO 不在任何受保护区间内
	io = makeIoRecord("phys1", 0, 100, 0, 1)
	id = classifyShmIO(devices, io)
	if id != "" {
		t.Errorf("expected empty, got %q", id)
	}
}

func TestClassifyShmIOVolumeLevel(t *testing.T) {
	// 一个物理磁盘上有两个卷
	devices := []biotrkmeta.ProtectedDevice{
		makeProtectedDevice(biotrkmeta.DeviceTypeVolume, "volume-001",
			makeProtectedExtent(makeDiskExtent("phys0", 0, 4096), 0, 4),
		),
		makeProtectedDevice(biotrkmeta.DeviceTypeVolume, "volume-002",
			makeProtectedExtent(makeDiskExtent("phys0", 4096, 4096), 4, 4),
		),
	}

	// IO 落在 volume-001
	io := makeIoRecord("phys0", 100, 256, 0, 1)
	id := classifyShmIO(devices, io)
	if id != "volume-001" {
		t.Errorf("got %q, want volume-001", id)
	}

	// IO 落在 volume-002
	io = makeIoRecord("phys0", 5000, 512, 0, 1)
	id = classifyShmIO(devices, io)
	if id != "volume-002" {
		t.Errorf("got %q, want volume-002", id)
	}

	// IO 跨两个卷的边界 — 不应被任一归类
	io = makeIoRecord("phys0", 4000, 200, 0, 1)
	id = classifyShmIO(devices, io)
	if id != "" {
		t.Errorf("cross-boundary IO should not be classified, got %q", id)
	}
}

func TestClassifyShmIOMixedDiskAndVolume(t *testing.T) {
	// 同时有磁盘级和卷级保护
	devices := []biotrkmeta.ProtectedDevice{
		makeProtectedDevice(biotrkmeta.DeviceTypeDisk, "full-disk",
			makeProtectedExtent(makeDiskExtent("phys0", 0, 1048576), 0, 1024),
		),
		makeProtectedDevice(biotrkmeta.DeviceTypeVolume, "vol-on-disk",
			makeProtectedExtent(makeDiskExtent("phys0", 2048, 4096), 0, 4),
		),
	}

	// IO 同时落在 full-disk 和 vol-on-disk 上，返回第一个匹配（full-disk）
	io := makeIoRecord("phys0", 3000, 256, 0, 1)
	id := classifyShmIO(devices, io)
	if id != "full-disk" {
		t.Errorf("got %q, want full-disk (first match)", id)
	}
}

func TestClassifyShmIONilIO(t *testing.T) {
	devices := []biotrkmeta.ProtectedDevice{
		makeProtectedDevice(biotrkmeta.DeviceTypeDisk, "d", makeProtectedExtent(makeDiskExtent("p", 0, 100), 0, 1)),
	}
	if id := classifyShmIO(devices, nil); id != "" {
		t.Error("nil IO should return empty string")
	}
}

// ============================================================================
// isBlockProtected
// ============================================================================

func TestIsBlockProtected(t *testing.T) {
	devices := []biotrkmeta.ProtectedDevice{
		makeProtectedDevice(biotrkmeta.DeviceTypeDisk, "disk",
			makeProtectedExtent(makeDiskExtent("phys0", 4096, 8192), 0, 8),
		),
	}
	disk0 := makeDiskID("phys0")

	if !isBlockProtected(devices, disk0, 4096, 1024) {
		t.Error("should be protected")
	}
	if isBlockProtected(devices, disk0, 0, 4096) {
		t.Error("should not be protected")
	}
}

// ============================================================================
// protectedExtentsForDisk
// ============================================================================

func TestProtectedExtentsForDisk(t *testing.T) {
	devices := []biotrkmeta.ProtectedDevice{
		makeProtectedDevice(biotrkmeta.DeviceTypeDisk, "d1",
			makeProtectedExtent(makeDiskExtent("phys0", 0, 100), 0, 1),
		),
		makeProtectedDevice(biotrkmeta.DeviceTypeVolume, "v1",
			makeProtectedExtent(makeDiskExtent("phys1", 200, 300), 0, 1),
			makeProtectedExtent(makeDiskExtent("phys0", 100, 200), 1, 1),
		),
	}

	extents := protectedExtentsForDisk(devices, makeDiskID("phys0"))
	if len(extents) != 2 {
		t.Fatalf("expected 2 extents for phys0, got %d", len(extents))
	}

	extents = protectedExtentsForDisk(devices, makeDiskID("phys2"))
	if len(extents) != 0 {
		t.Fatalf("expected 0 extents for phys2, got %d", len(extents))
	}
}

// ============================================================================
// deviceIDToString
// ============================================================================

func TestDeviceIDToString(t *testing.T) {
	disk := biotrkmeta.ProtectedDevice{Type: biotrkmeta.DeviceTypeDisk}
	copy(disk.DeviceID[:], "disk-name")
	if got := deviceIDToString(disk); got != "disk-name" {
		t.Errorf("got %q, want disk-name", got)
	}

	vol := biotrkmeta.ProtectedDevice{Type: biotrkmeta.DeviceTypeVolume}
	copy(vol.DeviceID[:], "vol-uuid\x00")
	if got := deviceIDToString(vol); got != "vol-uuid" {
		t.Errorf("got %q, want vol-uuid", got)
	}
}

// ============================================================================
// CdpRecord Pack/Unpack 往返
// ============================================================================

func TestCdpRecordPackUnpack(t *testing.T) {
	orig := &CdpRecord{
		Header: IOHeader{
			Mask:        proto.IOMask_DATA_REALTIME,
			DeviceID:    "test-device-id",
			DeviceIDLen: uint32(len("test-device-id")),
			Timestamp:   1234567890,
			Sequence:    42,
		},
		Data: hkc.Cluster{}, // 空集群数据（测试基本序列化）
	}

	var buf bytes.Buffer
	if err := orig.Pack(&buf); err != nil {
		t.Fatalf("Pack: %v", err)
	}

	decoded, err := UnpackCdpRecord(&buf)
	if err != nil {
		t.Fatalf("UnpackCdpRecord: %v", err)
	}

	if decoded.Header.Mask != orig.Header.Mask {
		t.Error("Mask mismatch")
	}
	if decoded.Header.DeviceID != orig.Header.DeviceID {
		t.Error("DeviceID mismatch")
	}
	if decoded.Header.Sequence != orig.Header.Sequence {
		t.Error("Sequence mismatch")
	}
	if decoded.Header.Timestamp != orig.Header.Timestamp {
		t.Error("Timestamp mismatch")
	}
}

func TestCdpRecordPackRoundTrip(t *testing.T) {
	p := &CdpRecord{
		Header: IOHeader{
			Mask:        proto.IOMask_DATA_BITMAP,
			DeviceID:    "SCSI\\DISK&VEN_INTEL&PROD_SSDSC2KB480G8",
			DeviceIDLen: uint32(len("SCSI\\DISK&VEN_INTEL&PROD_SSDSC2KB480G8")),
			Timestamp:   9876543210,
			Sequence:    100,
		},
	}

	var buf bytes.Buffer
	if err := struc.Pack(&buf, p); err != nil {
		t.Fatalf("struc.Pack: %v", err)
	}

	if err := struc.Unpack(&buf, p); err != nil {
		t.Fatalf("struc.Unpack: %v", err)
	}
}

func TestIOHeaderBinaryStructSize(t *testing.T) {
	h := IOHeader{
		DeviceID:    "hello",
		DeviceIDLen: 5,
	}
	want := 24 + uint64(h.DeviceIDLen)
	if got := h.BinaryStructSize(); got != want {
		t.Errorf("BinaryStructSize: got %d, want %d", got, want)
	}
}

// ============================================================================
// partitionHeadersBelongToDevice
// ============================================================================

func TestPartitionHeadersBelongToDevice(t *testing.T) {
	devices := []biotrkmeta.ProtectedDevice{
		makeProtectedDevice(biotrkmeta.DeviceTypeDisk, "disk-A",
			makeProtectedExtent(makeDiskExtent("phys0", 0, 512), 0, 1),
		),
		makeProtectedDevice(biotrkmeta.DeviceTypeVolume, "vol-B",
			makeProtectedExtent(makeDiskExtent("phys0", 4096, 1024), 0, 1),
		),
	}

	diskID := makeDiskID("phys0")
	headers := []biotrkmeta.DiskPartTableData{
		{Offset: 0, Data: []byte("MBR-HEADER")},        // 落入 disk-A
		{Offset: 4096, Data: []byte("VOL-HEADER")},     // 落入 vol-B
		{Offset: 99999, Data: []byte("OUT-OF-BOUNDS")}, // 无匹配
	}

	byDevice := partitionHeadersBelongToDevice(devices, diskID, headers)

	if len(byDevice["disk-A"]) != 1 {
		t.Errorf("disk-A should have 1 header, got %d", len(byDevice["disk-A"]))
	}
	if len(byDevice["vol-B"]) != 1 {
		t.Errorf("vol-B should have 1 header, got %d", len(byDevice["vol-B"]))
	}
}

// ============================================================================
// SequenceGenerator
// ============================================================================

func TestSequenceGenerator(t *testing.T) {
	gen := NewSequenceGenerator(1)

	if seq := gen.Get(); seq != 1 {
		t.Errorf("first seq: got %d, want 1", seq)
	}
	if seq := gen.Get(); seq != 2 {
		t.Errorf("second seq: got %d, want 2", seq)
	}

	gen.Rollback()
	if seq := gen.Get(); seq != 2 {
		t.Errorf("after rollback: got %d, want 2", seq)
	}

	gen.Freeze()
	if seq := gen.Get(); seq != 3 {
		t.Errorf("frozen first: got %d, want 3", seq)
	}
	if seq := gen.Get(); seq != 3 {
		t.Errorf("frozen second: got %d, want 3", seq)
	}

	gen.Unfreeze()
	// Unfreeze 后首次 Get 返回冻结期间的值（3），然后递增到 4
	if seq := gen.Get(); seq != 3 {
		t.Errorf("after unfreeze: got %d, want 3", seq)
	}
	if seq := gen.Get(); seq != 4 {
		t.Errorf("after unfreeze second: got %d, want 4", seq)
	}
}

func TestSequenceGeneratorReset(t *testing.T) {
	gen := NewSequenceGenerator(10)
	gen.Get() // 10
	gen.Get() // 11
	gen.Reset()
	if seq := gen.Get(); seq != 10 {
		t.Errorf("after reset: got %d, want 10", seq)
	}
}

// ============================================================================
// WriteJournalChecksum / ValidateJournalChecksum
// ============================================================================

func TestJournalChecksumRoundTrip(t *testing.T) {
	journal := &proto.CdpJournal{
		Mask:       proto.IOMask_DATA_REALTIME,
		CdpRecords: []byte("test cdp records data"),
	}

	WriteJournalChecksum(journal)
	if err := ValidateJournalChecksum(journal); err != nil {
		t.Errorf("ValidateJournalChecksum: %v", err)
	}

	// 篡改数据应检测到
	journal.CdpRecords[0] ^= 0xFF
	if err := ValidateJournalChecksum(journal); err == nil {
		t.Error("should detect checksum mismatch")
	}
}

func TestJournalChecksumFlagRecord(t *testing.T) {
	journal := &proto.CdpJournal{
		Mask:       proto.IOMask_FLAG_OPEN_FULL,
		CdpRecords: []byte("flag data"),
	}

	WriteJournalChecksum(journal)
	// 标记记录不计算 checksum，所以字段保持为 0
	if journal.CdpRecordsChecksum != 0 {
		t.Errorf("flag record checksum should be 0, got %d", journal.CdpRecordsChecksum)
	}
	if err := ValidateJournalChecksum(journal); err != nil {
		t.Errorf("flag record validation should not error: %v", err)
	}
}

func TestJournalChecksumNil(t *testing.T) {
	WriteJournalChecksum(nil)
	if err := ValidateJournalChecksum(nil); err != nil {
		t.Error("nil journal should not error")
	}
}

// ============================================================================
// NewCdpJournalBuilder
// ============================================================================

func TestNewCdpJournalBuilder(t *testing.T) {
	t.Run("nil option", func(t *testing.T) {
		_, err := NewCdpJournalBuilder(nil)
		if err == nil {
			t.Error("expected error for nil option")
		}
	})

	t.Run("invalid mask", func(t *testing.T) {
		_, err := NewCdpJournalBuilder(&CdpJournalOption{
			Mask: proto.IOMask_FLAG_OPEN_FULL,
		})
		if err == nil {
			t.Error("expected error for flag mask")
		}
	})

	t.Run("nil sequence generator", func(t *testing.T) {
		_, err := NewCdpJournalBuilder(&CdpJournalOption{
			Mask: proto.IOMask_DATA_REALTIME,
		})
		if err == nil {
			t.Error("expected error for nil sequence generator")
		}
	})
}

func TestCdpJournalBuilderAddDiskRecord(t *testing.T) {
	gen := NewSequenceGenerator(1)
	b, err := NewCdpJournalBuilder(&CdpJournalOption{
		Mask:              proto.IOMask_DATA_FULL,
		SequenceGenerator: gen,
	})
	if err != nil {
		t.Fatalf("NewCdpJournalBuilder: %v", err)
	}

	diskID := makeDiskID("test-disk")
	data := []byte("hello disk io")

	if err := b.AddDiskRecord(diskID, 4096, data); err != nil {
		t.Fatalf("AddDiskRecord: %v", err)
	}

	// Build 验证
	journal := b.Build()
	if journal == nil {
		t.Fatal("Build returned nil")
	}
	if journal.Mask != proto.IOMask_DATA_FULL {
		t.Error("Mask mismatch")
	}
	if len(journal.CdpRecords) == 0 {
		t.Fatal("CdpRecords is empty")
	}

	// 二次 Build 返回空 journal（buffer 已清空）
	journal2 := b.Build()
	if journal2 == nil {
		t.Error("second Build should return journal (empty buffer)")
	}
	if len(journal2.CdpRecords) != 0 {
		t.Error("second journal CdpRecords should be empty")
	}
}

func TestCdpJournalBuilderAddEmptyData(t *testing.T) {
	gen := NewSequenceGenerator(1)
	b, _ := NewCdpJournalBuilder(&CdpJournalOption{
		Mask:              proto.IOMask_DATA_REALTIME,
		SequenceGenerator: gen,
	})

	if err := b.AddDiskRecord(makeDiskID("d"), 0, nil); err != nil {
		t.Errorf("empty data should be silently ignored: %v", err)
	}
}

func TestCdpJournalBuilderReset(t *testing.T) {
	gen := NewSequenceGenerator(1)
	b, _ := NewCdpJournalBuilder(&CdpJournalOption{
		Mask:              proto.IOMask_DATA_REALTIME,
		SequenceGenerator: gen,
	})

	b.AddDiskRecord(makeDiskID("d"), 0, []byte("data1"))
	b.Build()
	b.Reset()

	b.AddDiskRecord(makeDiskID("d"), 0, []byte("data2"))
	journal := b.Build()
	if journal == nil {
		t.Error("Build after Reset should succeed")
	}
}

func TestCdpJournalBuilderAddDiskRecordEmptyData(t *testing.T) {
	gen := NewSequenceGenerator(1)
	b, _ := NewCdpJournalBuilder(&CdpJournalOption{
		Mask:              proto.IOMask_DATA_REALTIME,
		SequenceGenerator: gen,
	})

	// 空数据应静默忽略
	if err := b.AddDiskRecord(makeDiskID("d"), 0, []byte{}); err != nil {
		t.Error(err)
	}
	journal := b.Build()
	// Build 返回非 nil journal，但 CdpRecords 为空
	if journal == nil {
		t.Error("Build should return non-nil journal even with no records")
	}
	if len(journal.CdpRecords) != 0 {
		t.Error("CdpRecords should be empty")
	}
}

// ============================================================================
// AddSharedMemoryRecord 卷归类测试
// ============================================================================

func TestAddSharedMemoryRecordVolumeClassification(t *testing.T) {
	gen := NewSequenceGenerator(1)
	b, err := NewCdpJournalBuilder(&CdpJournalOption{
		Mask:              proto.IOMask_DATA_FULL,
		SequenceGenerator: gen,
	})
	if err != nil {
		t.Fatalf("NewCdpJournalBuilder: %v", err)
	}

	// 磁盘上有两个卷
	devices := []biotrkmeta.ProtectedDevice{
		makeProtectedDevice(biotrkmeta.DeviceTypeVolume, "vol-A",
			makeProtectedExtent(makeDiskExtent("phys0", 0, 4096), 0, 4),
		),
		makeProtectedDevice(biotrkmeta.DeviceTypeVolume, "vol-B",
			makeProtectedExtent(makeDiskExtent("phys0", 4096, 4096), 4, 4),
		),
	}

	// IO 在 vol-A 范围内
	shmIOs := []*ioctl.IoRecord{
		makeIoRecord("phys0", 1024, 512, 0, 0),
	}

	containsRealtime, err := b.AddSharedMemoryRecord(devices, shmIOs)
	if err != nil {
		t.Fatalf("AddSharedMemoryRecord: %v", err)
	}
	if containsRealtime {
		t.Error("should not contain realtime")
	}

	journal := b.Build()
	if journal == nil {
		t.Fatal("Build returned nil")
	}
	if len(journal.CdpRecords) == 0 {
		t.Fatal("expected at least one record")
	}
}

func TestAddSharedMemoryRecordRealtime(t *testing.T) {
	gen := NewSequenceGenerator(1)
	b, _ := NewCdpJournalBuilder(&CdpJournalOption{
		Mask:              proto.IOMask_DATA_REALTIME,
		SequenceGenerator: gen,
	})

	devices := []biotrkmeta.ProtectedDevice{
		makeProtectedDevice(biotrkmeta.DeviceTypeDisk, "d",
			makeProtectedExtent(makeDiskExtent("phys0", 0, 10000), 0, 10),
		),
	}

	// 实时阶段不应出现一致性=0的IO
	shmIOs := []*ioctl.IoRecord{
		makeIoRecord("phys0", 100, 256, 0, 0), // consistency=0, not allowed in realtime
	}

	_, err := b.AddSharedMemoryRecord(devices, shmIOs)
	if err == nil {
		t.Error("should error on inconsistent IO during realtime")
	}
}

func TestAddSharedMemoryRecordPartitionTable(t *testing.T) {
	// 分区表修改 IO 测试（无需真实磁盘——需要 mock）
	// 此测试在无驱动环境下验证类型安全
	gen := NewSequenceGenerator(1)
	b, _ := NewCdpJournalBuilder(&CdpJournalOption{
		Mask:              proto.IOMask_DATA_FULL,
		SequenceGenerator: gen,
	})

	devices := []biotrkmeta.ProtectedDevice{
		makeProtectedDevice(biotrkmeta.DeviceTypeDisk, "disk",
			makeProtectedExtent(makeDiskExtent("phys0", 0, 512), 0, 1),
		),
	}

	// Type=1 的分区表修改 IO 会尝试读取真实磁盘，无驱动/无访问权限时预期报错
	shmIOs := []*ioctl.IoRecord{
		makeIoRecord("phys0", 0, 0, 1, 0), // Type=1, partition table modification
	}

	_, err := b.AddSharedMemoryRecord(devices, shmIOs)
	// 预期失败（无法打开磁盘读取分区表）
	if err == nil {
		t.Skip("partition table test requires disk access")
	}
	t.Logf("partition table error (expected): %v", err)
}

func TestAddSharedMemoryRecordEmpty(t *testing.T) {
	gen := NewSequenceGenerator(1)
	b, _ := NewCdpJournalBuilder(&CdpJournalOption{
		Mask:              proto.IOMask_DATA_FULL,
		SequenceGenerator: gen,
	})

	containsRealtime, err := b.AddSharedMemoryRecord(nil, nil)
	if err != nil {
		t.Error(err)
	}
	if containsRealtime {
		t.Error("empty input should return false")
	}
}

// ============================================================================
// String 表示
// ============================================================================

func TestCdpJournalBuilderString(t *testing.T) {
	gen := NewSequenceGenerator(1)
	b, _ := NewCdpJournalBuilder(&CdpJournalOption{
		Mask:              proto.IOMask_DATA_REALTIME,
		SequenceGenerator: gen,
	})

	s := b.String()
	if s == "" {
		t.Error("String() should not be empty")
	}
}

func TestCdpRecordString(t *testing.T) {
	r := &CdpRecord{
		Header: IOHeader{
			Mask:     proto.IOMask_DATA_FULL,
			Sequence: 5,
			DeviceID: "test",
		},
	}
	s := r.String()
	if s == "" {
		t.Error("String() should not be empty")
	}
}

// ============================================================================
// 边界条件
// ============================================================================

func TestClassifyShmIOOverlap(t *testing.T) {
	// IO 范围与 Extent 部分重叠（不完全包含）不应归类
	devices := []biotrkmeta.ProtectedDevice{
		makeProtectedDevice(biotrkmeta.DeviceTypeDisk, "disk",
			makeProtectedExtent(makeDiskExtent("phys0", 100, 200), 0, 1),
		),
	}

	// IO [50, 150) — 部分重叠，不完全包含
	io := makeIoRecord("phys0", 50, 100, 0, 0)
	if id := classifyShmIO(devices, io); id != "" {
		t.Errorf("partial overlap should not match, got %q", id)
	}

	// IO [250, 300) — 完全在 Extent [100, 300) 内
	io = makeIoRecord("phys0", 250, 50, 0, 0)
	if id := classifyShmIO(devices, io); id != "disk" {
		t.Errorf("fully inside should match, got %q", id)
	}
}

func TestIsRangeInExtentOverflow(t *testing.T) {
	ext := makeDiskExtent("d", ^uint64(0)-100, 200)
	if isRangeInExtent(ext, ^uint64(0)-50, 100) {
		t.Error("overflow should be handled")
	}
}
