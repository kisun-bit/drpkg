package ioctl

import (
	"bytes"
	"encoding/binary"
	"sync/atomic"
	"testing"
	"unsafe"

	biotrkmeta "github.com/kisun-bit/drpkg/cdp/drvctl/v2/meta"
)

// ============================================================================
// 辅助函数
// ============================================================================

// mockShmRing 创建一个用于测试的 ShmRing，使用 Go 分配的内存模拟共享内存。
func mockShmRing(totalSize uint64, maxReadLen uint64) *ShmRing {
	data := make([]byte, totalSize)

	ringHeader := (*RingHeader)(unsafe.Pointer(&data[0]))
	ringHeader.RingSize = totalSize

	return &ShmRing{
		ringHeader:  ringHeader,
		ringData:    (*byte)(unsafe.Pointer(&data[RingHeaderWireSize])),
		ringSize:    totalSize,
		dataSize:    totalSize - RingHeaderWireSize,
		nextReadPos: -1,
		maxReadLen:  maxReadLen,
	}
}

// writeIoToRing 将 IoHeader 和 data 写入环形缓冲区的指定位置。
func writeIoToRing(r *ShmRing, pos int64, header *IoHeader, data []byte) int64 {
	headerBuf, _ := PackIoHeader(header)
	target := unsafe.Slice(r.ringOffset(pos), len(headerBuf))
	copy(target, headerBuf)
	pos += int64(len(headerBuf))

	if len(data) > 0 {
		target := unsafe.Slice(r.ringOffset(pos), len(data))
		copy(target, data)
		pos += int64(len(data))
	}
	return pos
}

// ============================================================================
// TransferStatus 常量
// ============================================================================

func TestTransferStatusValues(t *testing.T) {
	if TransferSuccess != 0 {
		t.Error("TransferSuccess should be 0")
	}
	if TransferError != 1 {
		t.Error("TransferError should be 1")
	}
	if TransferNoData != 2 {
		t.Error("TransferNoData should be 2")
	}
	if TransferOverflow != 3 {
		t.Error("TransferOverflow should be 3")
	}
}

// ============================================================================
// ShmConfig 类型
// ============================================================================

func TestShmConfigDefaults(t *testing.T) {
	cfg := ShmConfig{}
	if cfg.Size != 0 {
		t.Error("default Size should be 0")
	}
	if cfg.EventHandle != 0 {
		t.Error("default EventHandle should be 0")
	}
	if cfg.Address != 0 {
		t.Error("default Address should be 0")
	}
}

// ============================================================================
// IoHeader 序列化往返
// ============================================================================

func TestIoHeaderPackUnpack(t *testing.T) {
	orig := &IoHeader{
		Type:        1,
		Consistency: 1,
		Offset:      4096,
		Length:      512,
		Timestamp:   1234567890,
	}
	copy(orig.DiskID.ID[:], "test-disk-for-ioheader")

	packed, err := PackIoHeader(orig)
	if err != nil {
		t.Fatalf("PackIoHeader: %v", err)
	}

	if len(packed) != IoHeaderWireSize {
		t.Errorf("packed size: got %d, want %d", len(packed), IoHeaderWireSize)
	}

	decoded, err := UnpackIoHeader(packed)
	if err != nil {
		t.Fatalf("UnpackIoHeader: %v", err)
	}

	if diskIDStr(decoded.DiskID) != diskIDStr(orig.DiskID) {
		t.Error("DiskID mismatch")
	}
	if decoded.Type != orig.Type {
		t.Error("Type mismatch")
	}
	if decoded.Consistency != orig.Consistency {
		t.Error("Consistency mismatch")
	}
	if decoded.Offset != orig.Offset {
		t.Error("Offset mismatch")
	}
	if decoded.Length != orig.Length {
		t.Error("Length mismatch")
	}
	if decoded.Timestamp != orig.Timestamp {
		t.Error("Timestamp mismatch")
	}
}

func TestIoHeaderWireSizeConstant(t *testing.T) {
	want := biotrkmeta.DiskIDBinSize + 1 + 1 + 8 + 8 + 8
	if IoHeaderWireSize != want {
		t.Errorf("IoHeaderWireSize: got %d, want %d", IoHeaderWireSize, want)
	}
}

// ============================================================================
// RingHeader wire 大小
// ============================================================================

func TestRingHeaderWireSize(t *testing.T) {
	// RingSize(8) + Overflow(8) + ReadPos(8) + WritePos(8) = 32
	if RingHeaderWireSize != 32 {
		t.Errorf("RingHeaderWireSize: got %d, want 32", RingHeaderWireSize)
	}
}

// ============================================================================
// SortByDiskID
// ============================================================================

func TestSortByDiskID(t *testing.T) {
	records := []*IoRecord{
		{Header: IoHeader{DiskID: makeDiskIDForIo("ccc")}},
		{Header: IoHeader{DiskID: makeDiskIDForIo("aaa")}},
		{Header: IoHeader{DiskID: makeDiskIDForIo("bbb")}},
		{Header: IoHeader{DiskID: makeDiskIDForIo("aaa")}},
	}

	SortByDiskID(records)

	expected := []string{"aaa", "aaa", "bbb", "ccc"}
	for i, rec := range records {
		got := diskIDStr(rec.Header.DiskID)
		if got != expected[i] {
			t.Errorf("position %d: got %q, want %q", i, got, expected[i])
		}
	}
}

func TestSortByDiskIDEmpty(t *testing.T) {
	SortByDiskID(nil) // 不 panic
	SortByDiskID([]*IoRecord{})
}

// ============================================================================
// diskIDEqual
// ============================================================================

func TestDiskIDEqual(t *testing.T) {
	a := makeDiskIDForIo("disk-a")
	b := makeDiskIDForIo("disk-a")
	c := makeDiskIDForIo("disk-b")

	if !diskIDEqual(a, b) {
		t.Error("identical IDs should be equal")
	}
	if diskIDEqual(a, c) {
		t.Error("different IDs should not be equal")
	}
}

// ============================================================================
// ringOffset 辅助
// ============================================================================

func TestRingOffset(t *testing.T) {
	r := mockShmRing(4096, 65536)
	defer func() {
		// 清理 mock 内存，防止 GC 问题
	}()

	p0 := r.ringOffset(0)
	p100 := r.ringOffset(100)

	if uintptr(unsafe.Pointer(p100))-uintptr(unsafe.Pointer(p0)) != 100 {
		t.Error("ringOffset(100) should be 100 bytes after ringOffset(0)")
	}
}

// ============================================================================
// mock 环形缓冲区读写测试
// ============================================================================

func TestReadCacheNoWrap(t *testing.T) {
	r := mockShmRing(4096, 65536)

	src := []byte("hello world")
	copy(unsafe.Slice(r.ringOffset(0), len(src)), src)

	buf := make([]byte, len(src))
	newPos, err := r.readCache(buf, int64(len(src)), 0, 4096)
	if err != nil {
		t.Fatalf("readCache: %v", err)
	}

	if !bytes.Equal(buf, src) {
		t.Errorf("got %q, want %q", buf, src)
	}
	if newPos != int64(len(src)) {
		t.Errorf("newPos: got %d, want %d", newPos, len(src))
	}
}

func TestReadCacheWraparound(t *testing.T) {
	r := mockShmRing(4096, 65536)

	dataSize := int64(r.dataSize)

	// 在环形缓冲区末尾写入 50 字节，开头写入 50 字节
	endData := bytes.Repeat([]byte{0xAA}, 50)
	startData := bytes.Repeat([]byte{0xBB}, 50)
	copy(unsafe.Slice(r.ringOffset(dataSize-50), 50), endData)
	copy(unsafe.Slice(r.ringOffset(0), 50), startData)

	// 从 dataSize-50 读取 100 字节，应该回绕
	buf := make([]byte, 100)
	newPos, err := r.readCache(buf, 100, dataSize-50, 50)
	if err != nil {
		t.Fatalf("readCache wraparound: %v", err)
	}

	if !bytes.Equal(buf[:50], endData) {
		t.Error("first 50 bytes should be endData")
	}
	if !bytes.Equal(buf[50:], startData) {
		t.Error("last 50 bytes should be startData")
	}
	if newPos != 50 {
		t.Errorf("newPos: got %d, want 50", newPos)
	}
}

func TestReadCacheInsufficientData(t *testing.T) {
	r := mockShmRing(4096, 65536)

	buf := make([]byte, 100)
	_, err := r.readCache(buf, 100, 3900, 3950)
	if err == nil {
		t.Error("expected error for insufficient data")
	}
}

// ============================================================================
// 完整 Read 流程测试（使用 mock 环形缓冲区）
// ============================================================================

func TestReadWithMockRing(t *testing.T) {
	r := mockShmRing(8192, 65536)

	// 写入两条 IO 记录
	header1 := &IoHeader{
		Type:        0,
		Consistency: 1,
		Offset:      0,
		Length:      100,
		Timestamp:   1000,
	}
	copy(header1.DiskID.ID[:], "disk1")
	data1 := bytes.Repeat([]byte{0x11}, 100)

	header2 := &IoHeader{
		Type:        0,
		Consistency: 1,
		Offset:      4096,
		Length:      200,
		Timestamp:   2000,
	}
	copy(header2.DiskID.ID[:], "disk2")
	data2 := bytes.Repeat([]byte{0x22}, 200)

	pos := int64(0)
	pos = writeIoToRing(r, pos, header1, data1)
	pos = writeIoToRing(r, pos, header2, data2)

	// 设置写指针
	atomic.StoreInt64(&r.ringHeader.WritePos, pos)

	// 读取
	records, status, err := r.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if status != TransferSuccess {
		t.Fatalf("status: got %d, want TransferSuccess", status)
	}
	if len(records) != 2 {
		t.Fatalf("records count: got %d, want 2", len(records))
	}

	// 验证第一条记录
	if diskIDStr(records[0].Header.DiskID) != "disk1" {
		t.Error("record 0 DiskID mismatch")
	}
	if records[0].Header.Length != 100 {
		t.Error("record 0 Length mismatch")
	}
	if len(records[0].Data) != 100 {
		t.Error("record 0 Data length mismatch")
	}
	if !bytes.Equal(records[0].Data, data1) {
		t.Error("record 0 Data mismatch")
	}

	// 验证第二条记录
	if diskIDStr(records[1].Header.DiskID) != "disk2" {
		t.Error("record 1 DiskID mismatch")
	}
	if records[1].Header.Length != 200 {
		t.Error("record 1 Length mismatch")
	}
	if len(records[1].Data) != 200 {
		t.Error("record 1 Data length mismatch")
	}
}

func TestReadWithMockRingNoData(t *testing.T) {
	r := mockShmRing(4096, 65536)

	// 没有写入任何数据
	_, status, err := r.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if status != TransferNoData {
		t.Errorf("status: got %d, want TransferNoData", status)
	}
}

func TestReadWithMockRingOverflow(t *testing.T) {
	r := mockShmRing(4096, 65536)

	atomic.StoreInt64(&r.ringHeader.Overflow, 1)

	_, status, err := r.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if status != TransferOverflow {
		t.Errorf("status: got %d, want TransferOverflow", status)
	}
}

func TestReadWithMockRingTypeSkipData(t *testing.T) {
	r := mockShmRing(8192, 65536)

	// 分区表修改类型的 IO (Type=1) 不应该读取数据
	header := &IoHeader{
		Type:        1,
		Consistency: 0,
		Offset:      0,
		Length:      100,
		Timestamp:   3000,
	}
	copy(header.DiskID.ID[:], "partition-disk")

	pos := writeIoToRing(r, 0, header, nil)
	atomic.StoreInt64(&r.ringHeader.WritePos, pos)

	records, status, err := r.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if status != TransferSuccess {
		t.Fatalf("status: got %d, want TransferSuccess", status)
	}
	if len(records) != 1 {
		t.Fatalf("records count: got %d, want 1", len(records))
	}
	if records[0].Header.Type != 1 {
		t.Error("Type should be 1")
	}
	if len(records[0].Data) != 0 {
		t.Error("Data should be empty for Type=1")
	}
}

func TestReadWithMaxReadLen(t *testing.T) {
	r := mockShmRing(8192, 256) // 限制每次最多 256 字节

	// 写入两条各 200 字节的 IO 记录
	for i := 0; i < 2; i++ {
		header := &IoHeader{
			Type:        0,
			Consistency: 1,
			Offset:      uint64(i * 4096),
			Length:      200,
			Timestamp:   uint64(i * 1000),
		}
		copy(header.DiskID.ID[:], "disk")
		data := bytes.Repeat([]byte{byte(i + 1)}, 200)

		writePos := atomic.LoadInt64(&r.ringHeader.WritePos)
		newPos := writeIoToRing(r, writePos, header, data)
		atomic.StoreInt64(&r.ringHeader.WritePos, newPos)
	}

	// 第一条读取（应该只读到 1 条，因为 maxReadLen=256，一条 200 字节的 OK，但不够第二条）
	records, status, err := r.Read()
	if err != nil {
		t.Fatalf("first Read: %v", err)
	}
	if status != TransferSuccess {
		t.Fatalf("first status: got %d, want TransferSuccess", status)
	}
	if len(records) < 1 {
		t.Error("should read at least 1 record")
	}
}

func TestPeekNextLength(t *testing.T) {
	r := mockShmRing(4096, 65536)

	header := &IoHeader{
		Type:        0,
		Consistency: 1,
		Offset:      0,
		Length:      555,
		Timestamp:   999,
	}
	copy(header.DiskID.ID[:], "peek-disk")

	pos := writeIoToRing(r, 0, header, nil)
	atomic.StoreInt64(&r.ringHeader.WritePos, pos)

	length, err := r.peekNextLength(0, pos)
	if err != nil {
		t.Fatalf("peekNextLength: %v", err)
	}
	if length != 555 {
		t.Errorf("peeked length: got %d, want 555", length)
	}
}

func TestConfirmReadUpdatesReadPos(t *testing.T) {
	r := mockShmRing(8192, 65536)

	// 写入一条 IO 记录
	header := &IoHeader{
		Type:        0,
		Consistency: 1,
		Offset:      0,
		Length:      50,
		Timestamp:   100,
	}
	copy(header.DiskID.ID[:], "confirm-disk")
	data := bytes.Repeat([]byte{0x33}, 50)

	pos := writeIoToRing(r, 0, header, data)
	atomic.StoreInt64(&r.ringHeader.WritePos, pos)

	// 读取
	records, status, err := r.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if status != TransferSuccess {
		t.Fatalf("status: got %d, want TransferSuccess", status)
	}

	oldReadPos := atomic.LoadInt64(&r.ringHeader.ReadPos)

	// ConfirmRead 会更新 ReadPos（如果驱动可用会调用 ClearBitmapReference，这里会失败但不影响测试目的）
	r.ConfirmRead(records)

	newReadPos := atomic.LoadInt64(&r.ringHeader.ReadPos)
	if newReadPos <= oldReadPos {
		t.Errorf("ReadPos should have advanced: old=%d, new=%d", oldReadPos, newReadPos)
	}
}

// ============================================================================
// 驱动相关测试（需要驱动存在）
// ============================================================================

func TestNewShmRingNoDriver(t *testing.T) {
	if !driverAvailable() {
		t.Skip("driver not available")
	}

	r, err := NewShmRing(4096, 65536)
	if err != nil {
		t.Fatalf("NewShmRing: %v", err)
	}
	defer r.Close()

	if r.ringHeader == nil {
		t.Error("ringHeader should not be nil")
	}
	if r.maxReadLen != 65536 {
		t.Errorf("maxReadLen: got %d, want 65536", r.maxReadLen)
	}
}

func TestShmRingCloseNoDriver(t *testing.T) {
	if !driverAvailable() {
		t.Skip("driver not available")
	}

	r, err := NewShmRing(4096, 65536)
	if err != nil {
		t.Fatalf("NewShmRing: %v", err)
	}

	if err := r.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestShmRingWaitNoDriver(t *testing.T) {
	if !driverAvailable() {
		t.Skip("driver not available")
	}

	r, err := NewShmRing(4096, 65536)
	if err != nil {
		t.Fatalf("NewShmRing: %v", err)
	}
	defer r.Close()

	// 无数据时立即超时
	if err := r.Wait(0); err != nil {
		t.Errorf("Wait(0): %v", err)
	}
}

// ============================================================================
// 辅助函数
// ============================================================================

func makeDiskIDForIo(s string) biotrkmeta.DiskID {
	var d biotrkmeta.DiskID
	copy(d.ID[:], s)
	return d
}

// ============================================================================
// IoHeader 字节序验证
// ============================================================================

func TestIoHeaderLittleEndianEncoding(t *testing.T) {
	orig := &IoHeader{
		Type:        0x01,
		Consistency: 0x02,
		Offset:      0x0102030405060708,
		Length:      0x1122334455667788,
		Timestamp:   0xAABBCCDDEEFF0011,
	}

	packed, err := PackIoHeader(orig)
	if err != nil {
		t.Fatalf("PackIoHeader: %v", err)
	}

	// DiskID 之后：Type(1) + Consistency(1) + Offset(8) + Length(8) + Timestamp(8)
	off := biotrkmeta.DiskIDBinSize

	if packed[off] != 0x01 {
		t.Errorf("Type: got 0x%02X, want 0x01", packed[off])
	}
	if packed[off+1] != 0x02 {
		t.Errorf("Consistency: got 0x%02X, want 0x02", packed[off+1])
	}

	offsetVal := binary.LittleEndian.Uint64(packed[off+2:])
	if offsetVal != orig.Offset {
		t.Errorf("Offset: got 0x%016X, want 0x%016X", offsetVal, orig.Offset)
	}
}
