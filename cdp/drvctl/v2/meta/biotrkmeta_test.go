package meta

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/kisun-bit/drpkg/xutil"
)

// ============================================================================
// 辅助函数
// ============================================================================

func tempFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "biotrkmeta.bin")
}

func makeID(s string) ID {
	var id ID
	copy(id[:], s)
	return id
}

func makeDiskExtent(diskID string, start, size uint64) DiskExtent {
	var dkID DiskID
	copy(dkID.ID[:], diskID)
	return DiskExtent{DiskID: dkID, Start: start, Size: size}
}

func newMeta(t *testing.T) *BioTrkMetadata {
	t.Helper()
	bm, err := Create(tempFile(t), 0, 0, 0)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	t.Cleanup(func() { bm.file.Close() })
	return bm
}

func addDevice(t *testing.T, bm *BioTrkMetadata, devType DeviceType, devID string, extents []DiskExtent) {
	t.Helper()
	if err := bm.AddProtectDevice(devType, makeID(devID), extents); err != nil {
		t.Fatalf("AddProtectDevice(%s) failed: %v", devID, err)
	}
}

// ============================================================================
// bitTest / bitCopy / bitSet
// ============================================================================

func TestBitTest(t *testing.T) {
	buf := make([]byte, 4)
	// bit 0 set
	buf[0] = 0x01
	if !bitTest(buf, 0) {
		t.Error("bit 0 should be set")
	}
	if bitTest(buf, 1) {
		t.Error("bit 1 should be clear")
	}

	// bit 8 set (byte 1, bit 0)
	buf[1] = 0x01
	if !bitTest(buf, 8) {
		t.Error("bit 8 should be set")
	}
	if bitTest(buf, 9) {
		t.Error("bit 9 should be clear")
	}

	// bit 15 set (byte 1, bit 7)
	buf[1] = 0x80
	if !bitTest(buf, 15) {
		t.Error("bit 15 should be set")
	}
}

func TestBitCopy(t *testing.T) {
	tests := []struct {
		name      string
		src       []byte
		srcBitOff uint64
		n         uint64
		dstLen    int
		dstBitOff uint64
		expected  []byte
	}{
		{
			name:      "copy 8 bits aligned",
			src:       []byte{0xFF},
			srcBitOff: 0,
			n:         8,
			dstLen:    1,
			dstBitOff: 0,
			expected:  []byte{0xFF},
		},
		{
			name:      "copy 4 bits from offset 4",
			src:       []byte{0xF0}, // bits 4-7 set
			srcBitOff: 4,
			n:         4,
			dstLen:    1,
			dstBitOff: 0,
			expected:  []byte{0x0F},
		},
		{
			name:      "copy 3 bits unaligned",
			src:       []byte{0x07}, // bits 0-2
			srcBitOff: 0,
			n:         3,
			dstLen:    1,
			dstBitOff: 5,
			expected:  []byte{0xE0}, // bits 5-7 = 111
		},
		{
			name:      "copy across byte boundary",
			src:       []byte{0xFF, 0xFF},
			srcBitOff: 4,
			n:         8,
			dstLen:    2,
			dstBitOff: 2,
			expected:  []byte{0xFC, 0x03}, // src bits 4-11 → dst bits 2-9
		},
		{
			name:      "copy 0 bits",
			src:       []byte{0xFF},
			srcBitOff: 0,
			n:         0,
			dstLen:    1,
			dstBitOff: 0,
			expected:  []byte{0x00},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dst := make([]byte, tt.dstLen)
			bitCopy(dst, tt.dstBitOff, tt.src, tt.srcBitOff, tt.n)
			if !bytes.Equal(dst, tt.expected) {
				t.Errorf("got %08b, want %08b", dst, tt.expected)
			}
		})
	}
}

// ============================================================================
// mapLinearToExtent
// ============================================================================

func TestMapLinearToExtent(t *testing.T) {
	des := []DiskExtent{
		makeDiskExtent("disk0", 0, 1024),
		makeDiskExtent("disk1", 1048576, 2048),
		makeDiskExtent("disk2", 0, 4096),
	}

	tests := []struct {
		name       string
		logicalOff uint64
		wantExtIdx int
		wantOff    uint64
		wantErr    bool
	}{
		{"first byte of first extent", 0, 0, 0, false},
		{"last byte of first extent", 1023, 0, 1023, false},
		{"first byte of second extent", 1024, 1, 0, false},
		{"within second extent", 2048, 1, 1024, false},
		{"first byte of third extent", 3072, 2, 0, false},
		{"last byte of third extent", 7167, 2, 4095, false},
		{"out of range", 7168, 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extIdx, off, err := mapLinearToExtent(des, tt.logicalOff)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if extIdx != tt.wantExtIdx {
				t.Errorf("extIdx: got %d, want %d", extIdx, tt.wantExtIdx)
			}
			if off != tt.wantOff {
				t.Errorf("off: got %d, want %d", off, tt.wantOff)
			}
		})
	}
}

// ============================================================================
// isEmptyDeviceID / isValid
// ============================================================================

func TestIsEmptyDeviceID(t *testing.T) {
	if !isEmptyDeviceID([DeviceIDLen]byte{}) {
		t.Error("all-zero ID should be empty")
	}
	id := makeID("test")
	if isEmptyDeviceID(id) {
		t.Error("non-zero ID should not be empty")
	}
}

func TestProtectedDeviceIsValid(t *testing.T) {
	validID := makeID("disk1")

	tests := []struct {
		name string
		pd   ProtectedDevice
		want bool
	}{
		{
			name: "valid disk",
			pd:   ProtectedDevice{Type: DeviceTypeDisk, DeviceID: validID},
			want: true,
		},
		{
			name: "valid volume",
			pd:   ProtectedDevice{Type: DeviceTypeVolume, DeviceID: validID},
			want: true,
		},
		{
			name: "disk with empty ID",
			pd:   ProtectedDevice{Type: DeviceTypeDisk, DeviceID: [DeviceIDLen]byte{}},
			want: false, // was bug: returned true before fix
		},
		{
			name: "invalid type",
			pd:   ProtectedDevice{Type: DeviceType(999), DeviceID: validID},
			want: false,
		},
		{
			name: "empty device",
			pd:   ProtectedDevice{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.pd.isValid(); got != tt.want {
				t.Errorf("isValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ============================================================================
// Bitmap Unit Allocation
// ============================================================================

func TestBitmapUnitAllocFree(t *testing.T) {
	allocMap := make([]byte, 2) // 16 bitmap units

	// Initially all free
	for i := uint64(0); i < 16; i++ {
		if isBitmapUnitAllocated(allocMap, i) {
			t.Errorf("unit %d should be free initially", i)
		}
	}

	// Allocate unit 3
	setBitmapUnitAllocated(allocMap, 3, true)
	if !isBitmapUnitAllocated(allocMap, 3) {
		t.Error("unit 3 should be allocated")
	}
	if isBitmapUnitAllocated(allocMap, 2) {
		t.Error("unit 2 should still be free")
	}

	// Free unit 3
	setBitmapUnitAllocated(allocMap, 3, false)
	if isBitmapUnitAllocated(allocMap, 3) {
		t.Error("unit 3 should be free after release")
	}
}

func TestFindFreeBitmapUnits(t *testing.T) {
	tests := []struct {
		name       string
		allocMap   []byte
		totalUnits uint64
		count      uint64
		wantStart  uint64
		wantErr    bool
	}{
		{
			name:       "all free, find 1",
			allocMap:   []byte{0x00, 0x00},
			totalUnits: 16,
			count:      1,
			wantStart:  0,
		},
		{
			name:       "all free, find 5",
			allocMap:   []byte{0x00, 0x00},
			totalUnits: 16,
			count:      5,
			wantStart:  0,
		},
		{
			name:       "first 3 allocated, find 4",
			allocMap:   []byte{0x07, 0x00}, // bits 0,1,2 set
			totalUnits: 16,
			count:      4,
			wantStart:  3,
		},
		{
			name:       "fragmented, find 4 in second gap",
			allocMap:   []byte{0xFF, 0xF0}, // units 0-7 and 12-15 allocated
			totalUnits: 16,
			count:      3,
			wantStart:  8,
		},
		{
			name:       "not enough contiguous",
			allocMap:   []byte{0xFF, 0xFF},
			totalUnits: 16,
			count:      1,
			wantErr:    true,
		},
		{
			name:       "count 0",
			allocMap:   []byte{0x00},
			totalUnits: 8,
			count:      0,
			wantErr:    true,
		},
		{
			name:       "count exceeds total",
			allocMap:   []byte{0x00},
			totalUnits: 8,
			count:      9,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := findFreeBitmapUnits(tt.allocMap, tt.totalUnits, tt.count)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantStart {
				t.Errorf("got start %d, want %d", got, tt.wantStart)
			}
		})
	}
}

// ============================================================================
// CRC32
// ============================================================================

func TestCalcCRC32(t *testing.T) {
	h := defaultHeader(0, 0)
	h.HeaderCRC32 = 0

	// Manual CRC32 of the binary header
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, &h)
	expected := crc32.ChecksumIEEE(buf.Bytes())

	// calcCRC32 should set CRC32 to 0 internally
	h.HeaderCRC32 = 999 // arbitrary non-zero
	got := calcCRC32(&h)

	if got != expected {
		t.Errorf("CRC32 mismatch: got 0x%08x, want 0x%08x", got, expected)
	}
	// Verify HeaderCRC32 field was restored
	if h.HeaderCRC32 != 999 {
		t.Error("HeaderCRC32 should be restored after calcCRC32")
	}
}

// ============================================================================
// Binary Encoding Round-Trip（变长记录）
// ============================================================================

func TestHeaderBinarySize(t *testing.T) {
	h := defaultHeader(0, 0)

	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.LittleEndian, &h); err != nil {
		t.Fatalf("binary.Write failed: %v", err)
	}
	if buf.Len() != HeaderSize {
		t.Errorf("header binary size: got %d, want %d", buf.Len(), HeaderSize)
	}
}

func TestDiskIDPackUnpack(t *testing.T) {
	orig := DiskID{
		Major: 1,
		Minor: 2,
	}
	copy(orig.ID[:], "test-disk-id")

	w := recordWriter{buf: make([]byte, DiskIDBinSize)}
	if err := packDiskID(&w, &orig); err != nil {
		t.Fatalf("packDiskID failed: %v", err)
	}
	if w.pos != DiskIDBinSize {
		t.Errorf("packed size: got %d, want %d", w.pos, DiskIDBinSize)
	}

	var decoded DiskID
	if err := unpackDiskID(&recordReader{buf: w.buf}, &decoded); err != nil {
		t.Fatalf("unpackDiskID failed: %v", err)
	}

	if !bytes.Equal(decoded.ID[:], orig.ID[:]) {
		t.Error("ID mismatch")
	}
	if decoded.Major != orig.Major {
		t.Error("Major mismatch")
	}
	if decoded.Minor != orig.Minor {
		t.Error("Minor mismatch")
	}
}

func TestDiskExtentPackUnpack(t *testing.T) {
	orig := DiskExtent{
		Start: 1024,
		Size:  2048,
	}
	copy(orig.DiskID.ID[:], "disk-extent-id")
	orig.DiskID.Major = 3
	orig.DiskID.Minor = 4

	w := recordWriter{buf: make([]byte, DiskExtentBinSize)}
	if err := packDiskExtent(&w, &orig); err != nil {
		t.Fatalf("packDiskExtent failed: %v", err)
	}
	if w.pos != DiskExtentBinSize {
		t.Errorf("packed size: got %d, want %d", w.pos, DiskExtentBinSize)
	}

	var decoded DiskExtent
	if err := unpackDiskExtent(&recordReader{buf: w.buf}, &decoded); err != nil {
		t.Fatalf("unpackDiskExtent failed: %v", err)
	}

	if !bytes.Equal(decoded.DiskID.ID[:], orig.DiskID.ID[:]) {
		t.Error("DiskID.ID mismatch")
	}
	if decoded.DiskID.Major != orig.DiskID.Major {
		t.Error("DiskID.Major mismatch")
	}
	if decoded.DiskID.Minor != orig.DiskID.Minor {
		t.Error("DiskID.Minor mismatch")
	}
	if decoded.Start != orig.Start {
		t.Error("Start mismatch")
	}
	if decoded.Size != orig.Size {
		t.Error("Size mismatch")
	}
}

func TestProtectedExtentPackUnpack(t *testing.T) {
	orig := ProtectedExtent{
		BitmapUnitStart: 100,
		BitmapUnitCount: 6,
	}
	orig.Extent.Start = 4096
	orig.Extent.Size = 8192
	copy(orig.Extent.DiskID.ID[:], "extent-disk")
	orig.BitmapExtents = []DiskExtent{
		makeDiskExtent("phys0", 1<<20, 4096),
		makeDiskExtent("phys0", 2<<20, 8192),
	}
	orig.BitmapExtentCount = uint32(len(orig.BitmapExtents))

	buf, err := packProtectedExtent(&orig)
	if err != nil {
		t.Fatalf("packProtectedExtent failed: %v", err)
	}
	if len(buf) != calcProtectedExtentBinSize(&orig) {
		t.Errorf("packed size: got %d, want %d", len(buf), calcProtectedExtentBinSize(&orig))
	}

	var decoded ProtectedExtent
	if err := unpackProtectedExtent(buf, &decoded); err != nil {
		t.Fatalf("unpackProtectedExtent failed: %v", err)
	}

	if decoded.BitmapUnitStart != orig.BitmapUnitStart {
		t.Error("BitmapUnitStart mismatch")
	}
	if decoded.BitmapUnitCount != orig.BitmapUnitCount {
		t.Error("BitmapUnitCount mismatch")
	}
	if decoded.Extent.Start != orig.Extent.Start {
		t.Error("Extent.Start mismatch")
	}
	if decoded.Extent.Size != orig.Extent.Size {
		t.Error("Extent.Size mismatch")
	}
	if len(decoded.BitmapExtents) != len(orig.BitmapExtents) {
		t.Fatalf("BitmapExtents count: got %d, want %d", len(decoded.BitmapExtents), len(orig.BitmapExtents))
	}
	if decoded.BitmapExtentCount != orig.BitmapExtentCount {
		t.Errorf("BitmapExtentCount: got %d, want %d", decoded.BitmapExtentCount, orig.BitmapExtentCount)
	}
	for i := range orig.BitmapExtents {
		if decoded.BitmapExtents[i].Start != orig.BitmapExtents[i].Start {
			t.Errorf("BitmapExtents[%d].Start mismatch", i)
		}
		if decoded.BitmapExtents[i].Size != orig.BitmapExtents[i].Size {
			t.Errorf("BitmapExtents[%d].Size mismatch", i)
		}
		if decoded.BitmapExtents[i].DiskID.String() != orig.BitmapExtents[i].DiskID.String() {
			t.Errorf("BitmapExtents[%d].DiskID mismatch", i)
		}
	}
}

// TestProtectedExtentSizeGrowsWithBitmapExtents 验证 ProtectedExtent 的编码大小
// 严格等于 固定部分 + BitmapExtents 数量 × DiskExtent 大小，没有预留槽位。
func TestProtectedExtentSizeGrowsWithBitmapExtents(t *testing.T) {
	for _, n := range []int{0, 1, 2, 24, 1000} {
		pe := ProtectedExtent{Extent: makeDiskExtent("disk0", 0, 4096), BitmapUnitCount: 1}
		pe.BitmapExtents = make([]DiskExtent, n)
		for i := range pe.BitmapExtents {
			pe.BitmapExtents[i] = makeDiskExtent("disk0", uint64(i)*4096, 4096)
		}

		want := ProtectedExtentFixedBinSize + n*DiskExtentBinSize
		if got := calcProtectedExtentBinSize(&pe); got != want {
			t.Errorf("n=%d: calcProtectedExtentBinSize = %d, want %d", n, got, want)
		}

		buf, err := packProtectedExtent(&pe)
		if err != nil {
			t.Fatalf("n=%d: pack failed: %v", n, err)
		}
		if len(buf) != want {
			t.Errorf("n=%d: packed size = %d, want %d", n, len(buf), want)
		}

		var decoded ProtectedExtent
		if err := unpackProtectedExtent(buf, &decoded); err != nil {
			t.Fatalf("n=%d: unpack failed: %v", n, err)
		}
		if len(decoded.BitmapExtents) != n {
			t.Errorf("n=%d: decoded BitmapExtents = %d", n, len(decoded.BitmapExtents))
		}
	}
}

func TestProtectedDevicePackUnpack(t *testing.T) {
	orig := ProtectedDevice{
		Type:    DeviceTypeVolume,
		Extents: make([]ProtectedExtent, 2),
	}
	copy(orig.DeviceID[:], "volume-001")
	orig.Extents[0] = ProtectedExtent{
		BitmapUnitStart: 0,
		BitmapUnitCount: 4,
	}
	orig.Extents[0].Extent = makeDiskExtent("diskA", 0, 4096)
	orig.Extents[1] = ProtectedExtent{
		BitmapUnitStart: 4,
		BitmapUnitCount: 2,
	}
	orig.Extents[1].Extent = makeDiskExtent("diskB", 1048576, 2048)
	orig.ExtentCount = uint32(len(orig.Extents))

	buf, err := packProtectedDevice(&orig)
	if err != nil {
		t.Fatalf("packProtectedDevice failed: %v", err)
	}
	if len(buf) != calcProtectedDeviceBinSize(&orig) {
		t.Errorf("packed size: got %d, want %d", len(buf), calcProtectedDeviceBinSize(&orig))
	}
	// TotalSize 前缀必须等于记录自身长度，顺序扫描全靠它
	if got := binary.LittleEndian.Uint32(buf); int(got) != len(buf) {
		t.Errorf("TotalSize prefix: got %d, want %d", got, len(buf))
	}

	var decoded ProtectedDevice
	if err := unpackProtectedDevice(buf, &decoded); err != nil {
		t.Fatalf("unpackProtectedDevice failed: %v", err)
	}

	if decoded.Type != orig.Type {
		t.Error("Type mismatch")
	}
	if !bytes.Equal(decoded.DeviceID[:], orig.DeviceID[:]) {
		t.Error("DeviceID mismatch")
	}
	if decoded.ExtentCount != orig.ExtentCount {
		t.Errorf("ExtentCount: got %d, want %d", decoded.ExtentCount, orig.ExtentCount)
	}
	if len(decoded.Extents) != len(orig.Extents) {
		t.Errorf("Extents count: got %d, want %d", len(decoded.Extents), len(orig.Extents))
	}
	for i := range orig.Extents {
		if decoded.Extents[i].BitmapUnitStart != orig.Extents[i].BitmapUnitStart {
			t.Errorf("Extents[%d].BitmapUnitStart mismatch", i)
		}
		if decoded.Extents[i].BitmapUnitCount != orig.Extents[i].BitmapUnitCount {
			t.Errorf("Extents[%d].BitmapUnitCount mismatch", i)
		}
		if decoded.Extents[i].Extent.DiskID.String() != orig.Extents[i].Extent.DiskID.String() {
			t.Errorf("Extents[%d].Extent.DiskID mismatch", i)
		}
	}
}

// TestProtectedDeviceSizeGrowsWithContent 验证记录大小随内容变化且严格可算，
// 大量 Extents / BitmapExtents 也不会被任何容量上限拦住。
func TestProtectedDeviceSizeGrowsWithContent(t *testing.T) {
	sparse := ProtectedDevice{Type: DeviceTypeDisk, Extents: make([]ProtectedExtent, 1)}
	copy(sparse.DeviceID[:], "sparse")
	sparse.Extents[0] = ProtectedExtent{Extent: makeDiskExtent("disk0", 0, 4096), BitmapUnitCount: 1}

	const numExtents = 500
	const numBitmapExtents = 200
	dense := ProtectedDevice{Type: DeviceTypeDisk, Extents: make([]ProtectedExtent, numExtents)}
	copy(dense.DeviceID[:], "dense")
	for i := range dense.Extents {
		dense.Extents[i] = ProtectedExtent{
			Extent:          makeDiskExtent("disk0", uint64(i)*4096, 4096),
			BitmapUnitCount: 1,
			BitmapExtents:   make([]DiskExtent, numBitmapExtents),
		}
		for j := range dense.Extents[i].BitmapExtents {
			dense.Extents[i].BitmapExtents[j] = makeDiskExtent("disk0", uint64(j)*4096, 4096)
		}
	}

	sparseBuf, err := packProtectedDevice(&sparse)
	if err != nil {
		t.Fatalf("pack sparse failed: %v", err)
	}
	denseBuf, err := packProtectedDevice(&dense)
	if err != nil {
		t.Fatalf("pack dense failed: %v", err)
	}

	if len(sparseBuf) != ProtectedDeviceMinBinSize+ProtectedExtentFixedBinSize {
		t.Errorf("sparse record size: got %d, want %d",
			len(sparseBuf), ProtectedDeviceMinBinSize+ProtectedExtentFixedBinSize)
	}

	wantDense := ProtectedDeviceMinBinSize +
		numExtents*(ProtectedExtentFixedBinSize+numBitmapExtents*DiskExtentBinSize)
	if len(denseBuf) != wantDense {
		t.Errorf("dense record size: got %d, want %d", len(denseBuf), wantDense)
	}

	var decoded ProtectedDevice
	if err := unpackProtectedDevice(denseBuf, &decoded); err != nil {
		t.Fatalf("unpack dense failed: %v", err)
	}
	if len(decoded.Extents) != numExtents {
		t.Fatalf("dense Extents count: got %d, want %d", len(decoded.Extents), numExtents)
	}
	for i := range decoded.Extents {
		if len(decoded.Extents[i].BitmapExtents) != numBitmapExtents {
			t.Fatalf("dense Extents[%d].BitmapExtents count: got %d, want %d",
				i, len(decoded.Extents[i].BitmapExtents), numBitmapExtents)
		}
	}
}

// TestParseProtectedRegion 验证多条变长记录能在一个区域里顺序解析出来。
func TestParseProtectedRegion(t *testing.T) {
	var records [][]byte
	for i := 0; i < 5; i++ {
		pd := ProtectedDevice{
			Type:    DeviceTypeDisk,
			Extents: make([]ProtectedExtent, i+1),
		}
		copy(pd.DeviceID[:], fmt.Sprintf("dev-%d", i))
		for j := range pd.Extents {
			pd.Extents[j] = ProtectedExtent{
				Extent:          makeDiskExtent("disk0", uint64(j)*4096, 4096),
				BitmapUnitCount: 1,
			}
		}
		buf, err := packProtectedDevice(&pd)
		if err != nil {
			t.Fatalf("pack %d failed: %v", i, err)
		}
		records = append(records, buf)
	}

	// 拼成一个区域，末尾补零模拟对齐填充
	var total int
	for _, r := range records {
		total += len(r)
	}
	region := make([]byte, alignUp(uint64(total)))
	pos := 0
	for _, r := range records {
		copy(region[pos:], r)
		pos += len(r)
	}

	devices, err := parseProtectedRegion(region, uint32(len(records)))
	if err != nil {
		t.Fatalf("parseProtectedRegion failed: %v", err)
	}
	if len(devices) != len(records) {
		t.Fatalf("parsed %d devices, want %d", len(devices), len(records))
	}
	for i := range devices {
		want := fmt.Sprintf("dev-%d", i)
		if got := xutil.TrimZeroString(devices[i].DeviceID[:]); got != want {
			t.Errorf("device %d: got %q, want %q", i, got, want)
		}
		if len(devices[i].Extents) != i+1 {
			t.Errorf("device %d: got %d extents, want %d", i, len(devices[i].Extents), i+1)
		}
	}

	// 声明的记录数超过区域实际能容纳的数量时必须报错
	if _, err := parseProtectedRegion(region, uint32(len(records))+1); err == nil {
		t.Error("expected error when count exceeds region content, got nil")
	}
}

// TestUnpackRejectsCorruptRecord 验证损坏的变长记录返回错误而不是 panic 或越界读。
func TestUnpackRejectsCorruptRecord(t *testing.T) {
	const (
		extentCountOff = ProtectedDeviceMinBinSize - 4
		firstExtentOff = ProtectedDeviceMinBinSize
		bitmapCountOff = firstExtentOff + ProtectedExtentFixedBinSize - 4
	)

	tests := []struct {
		name   string
		mutate func(buf []byte)
	}{
		{
			name: "TotalSize 小于最小记录",
			mutate: func(buf []byte) {
				binary.LittleEndian.PutUint32(buf[0:], 8)
			},
		},
		{
			name: "TotalSize 超出缓冲区",
			mutate: func(buf []byte) {
				binary.LittleEndian.PutUint32(buf[0:], uint32(len(buf))+4096)
			},
		},
		{
			name: "ExtentCount 超出记录大小",
			mutate: func(buf []byte) {
				binary.LittleEndian.PutUint32(buf[extentCountOff:], 0xFFFFFFFF)
			},
		},
		{
			name: "ExtentCount 比实际多一条",
			mutate: func(buf []byte) {
				binary.LittleEndian.PutUint32(buf[extentCountOff:], 2)
			},
		},
		{
			name: "BitmapExtentCount 超出记录大小",
			mutate: func(buf []byte) {
				binary.LittleEndian.PutUint32(buf[bitmapCountOff:], 0xFFFFFFFF)
			},
		},
		{
			name: "记录尾部有多余字节",
			mutate: func(buf []byte) {
				// TotalSize 声称比实际解码出来的多 4 字节
				binary.LittleEndian.PutUint32(buf[0:], uint32(len(buf)-4))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pd := ProtectedDevice{Type: DeviceTypeDisk, Extents: make([]ProtectedExtent, 1)}
			copy(pd.DeviceID[:], "corrupt")
			pd.Extents[0] = ProtectedExtent{Extent: makeDiskExtent("disk0", 0, 4096), BitmapUnitCount: 1}

			packed, err := packProtectedDevice(&pd)
			if err != nil {
				t.Fatalf("packProtectedDevice failed: %v", err)
			}
			// 多留一点余量，让"TotalSize 声称得比实际少"这类损坏也能被读到
			buf := make([]byte, len(packed)+8)
			copy(buf, packed)
			tt.mutate(buf)

			var decoded ProtectedDevice
			if err := unpackProtectedDevice(buf, &decoded); err == nil {
				t.Error("expected error for corrupt record, got nil")
			}
		})
	}
}

func TestUnpackRejectsShortBuffer(t *testing.T) {
	var decoded ProtectedDevice
	if err := unpackProtectedDevice(make([]byte, 16), &decoded); err == nil {
		t.Error("expected error for short buffer, got nil")
	}

	var pe ProtectedExtent
	if err := unpackProtectedExtent(make([]byte, 16), &pe); err == nil {
		t.Error("expected error for short buffer, got nil")
	}

	if _, err := parseProtectedRegion(make([]byte, 16), 1); err == nil {
		t.Error("expected error for short region, got nil")
	}
}

// ============================================================================
// Create / Load
// ============================================================================

func TestCreateAndLoad(t *testing.T) {
	path := tempFile(t)

	bm, err := Create(path, 0, 0, 0)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	defer bm.file.Close()

	// Verify header
	h := bm.header
	if string(h.Signature[:len(SignatureStr)]) != SignatureStr {
		t.Error("signature mismatch")
	}
	if h.Version != Versionv1_0 {
		t.Error("version mismatch")
	}
	if h.BitmapClusterSize != DefaultBitmapClusterSize {
		t.Error("BitmapClusterSize mismatch")
	}
	if h.TotalBitmapUnits != DefaultTotalBitmapUnits {
		t.Error("TotalBitmapUnits mismatch")
	}
	if h.BitIndexSpace != DefaultBitIndexSpace {
		t.Error("BitIndexSpace mismatch")
	}
	if h.ProtectedDeviceCount != 0 {
		t.Error("ProtectedDeviceCount should be 0")
	}

	// 区域顺序：Header → Allocation Map → Bitmap Unit Data → Protected Region（最后）
	if h.BitmapAllocMapOffset != HeaderSize {
		t.Errorf("BitmapAllocMapOffset: got %d, want %d", h.BitmapAllocMapOffset, HeaderSize)
	}
	if h.BitmapDataOffset != h.BitmapAllocMapOffset+h.BitmapAllocMapSize {
		t.Errorf("BitmapDataOffset: got %d, want %d",
			h.BitmapDataOffset, h.BitmapAllocMapOffset+h.BitmapAllocMapSize)
	}
	if h.ProtectedRegionOffset != h.BitmapDataOffset+h.BitmapDataTotalSize() {
		t.Errorf("ProtectedRegionOffset: got %d, want %d",
			h.ProtectedRegionOffset, h.BitmapDataOffset+h.BitmapDataTotalSize())
	}
	// 记录区在最后，Create 时还没有任何记录
	if h.ProtectedRegionSize != 0 {
		t.Errorf("ProtectedRegionSize: got %d, want 0", h.ProtectedRegionSize)
	}
	if h.TotalSize() != int64(h.ProtectedRegionOffset) {
		t.Errorf("TotalSize: got %d, want %d", h.TotalSize(), h.ProtectedRegionOffset)
	}

	bm.file.Close()

	// Reload
	bm2, err := Load(path, 0)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	defer bm2.file.Close()

	if bm2.header.Version != Versionv1_0 {
		t.Error("reloaded version mismatch")
	}
	if bm2.header.ProtectedDeviceCount != 0 {
		t.Error("reloaded device count should be 0")
	}
	if bm2.header.ProtectedRegionOffset != h.ProtectedRegionOffset {
		t.Error("reloaded ProtectedRegionOffset mismatch")
	}
	if bm2.header.BitmapDataOffset != h.BitmapDataOffset {
		t.Error("reloaded BitmapDataOffset mismatch")
	}
}

// TestProtectedRegionIsLast 验证 Protected Region 位于所有其他区域之后。
//
// 这是变长记录能安全增长的前提：记录区后面没有任何东西，长大也踩不到
// Allocation Map 或 Bitmap Unit Data。
func TestProtectedRegionIsLast(t *testing.T) {
	h := defaultHeader(0, 0)

	if h.ProtectedRegionOffset < h.BitmapAllocMapOffset+h.BitmapAllocMapSize {
		t.Error("protected region overlaps allocation map")
	}
	if h.ProtectedRegionOffset < h.BitmapDataOffset+h.BitmapDataTotalSize() {
		t.Error("protected region overlaps bitmap data")
	}
	if h.ProtectedRegionOffset != h.BitmapDataOffset+h.BitmapDataTotalSize() {
		t.Errorf("protected region should start right after bitmap data: got %d, want %d",
			h.ProtectedRegionOffset, h.BitmapDataOffset+h.BitmapDataTotalSize())
	}
	if h.TotalSize() != int64(h.ProtectedRegionOffset+h.ProtectedRegionSize) {
		t.Errorf("TotalSize should end at the protected region tail: got %d, want %d",
			h.TotalSize(), h.ProtectedRegionOffset+h.ProtectedRegionSize)
	}
}

// TestCreateRejectsUnalignedOffset 验证起始偏移必须 4096 对齐，
// 否则裸设备上无法按扇区对齐整体读写各个区域。
func TestCreateRejectsUnalignedOffset(t *testing.T) {
	path := tempFile(t)

	if _, err := Create(path, 512, 0, 0); err == nil {
		t.Error("expected error for unaligned offset, got nil")
	}
	if _, err := Load(path, 512); err == nil {
		t.Error("expected error for unaligned offset, got nil")
	}
}

// TestCreateZeroFillsWholeRegion 验证 Create 之后整个元数据区域都是 0。
//
// 普通文件仅靠 Truncate 撑大时，NTFS 有效数据长度之外的簇通过文件 API 读出为 0，
// 物理簇里却是旧数据；驱动按物理偏移直读，两者必须一致。
func TestCreateZeroFillsWholeRegion(t *testing.T) {
	path := tempFile(t)

	bm, err := Create(path, 0, 0, 0)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	defer bm.file.Close()

	size := bm.Size()
	if size%AlignSize != 0 {
		t.Fatalf("metadata size %d is not aligned to %d", size, AlignSize)
	}

	// Header 之外的区域必须全零
	buf := make([]byte, 1<<20)
	zeros := make([]byte, 1<<20)
	for off := int64(HeaderSize); off < size; off += int64(len(buf)) {
		n := int64(len(buf))
		if off+n > size {
			n = size - off
		}
		if _, err := bm.file.ReadAt(buf[:n], off); err != nil {
			t.Fatalf("read at %d failed: %v", off, err)
		}
		if !bytes.Equal(buf[:n], zeros[:n]) {
			t.Fatalf("region at offset %d is not all zero", off)
		}
	}
}

// TestLayoutIsAligned 验证所有区域起始偏移与区域大小都按 AlignSize 对齐。
//
// 变长记录的单条长度不保证对齐，所以读写以整个区域为单位；
// 区域偏移与区域大小必须对齐，裸设备上才能整体读写。
func TestLayoutIsAligned(t *testing.T) {
	h := defaultHeader(0, 0)

	for name, v := range map[string]uint64{
		"BitmapAllocMapOffset":  h.BitmapAllocMapOffset,
		"BitmapAllocMapSize":    h.BitmapAllocMapSize,
		"BitmapDataOffset":      h.BitmapDataOffset,
		"BitmapDataTotalSize":   h.BitmapDataTotalSize(),
		"ProtectedRegionOffset": h.ProtectedRegionOffset,
		"ProtectedRegionSize":   h.ProtectedRegionSize,
	} {
		if v%AlignSize != 0 {
			t.Errorf("%s = %d is not aligned to %d", name, v, AlignSize)
		}
	}

	// 每个 Bitmap Unit 都必须对齐
	clusterSize := uint64(h.BitmapClusterSize)
	if clusterSize%AlignSize != 0 {
		t.Errorf("BitmapClusterSize %d is not aligned to %d", clusterSize, AlignSize)
	}

	// 记录区增长后（ProtectedRegionSize 非零）总大小仍然对齐
	h.ProtectedRegionSize = alignUp(1234)
	if h.TotalSize()%AlignSize != 0 {
		t.Errorf("TotalSize %d is not aligned to %d", h.TotalSize(), AlignSize)
	}
}

// TestMetadataRegionFitsPhysicalTail 验证基础区域能放进物理盘尾部预留的 100 MiB，
// 并给变长记录区留出足够余量。TestPhysicalDrive 只在磁盘末尾预留 100 MiB 写元数据。
func TestMetadataRegionFitsPhysicalTail(t *testing.T) {
	const reserveBytes = 100 * 1024 * 1024

	h := defaultHeader(0, 0)
	if h.TotalSize() > reserveBytes {
		t.Errorf("base metadata region %d bytes exceeds physical tail reserve %d bytes",
			h.TotalSize(), reserveBytes)
	}
	t.Logf("base metadata region = %d bytes (%.2f MiB), reserve = %d MiB, headroom for records = %.2f MiB",
		h.TotalSize(), float64(h.TotalSize())/(1024*1024),
		reserveBytes/(1024*1024),
		float64(reserveBytes-h.TotalSize())/(1024*1024))
	t.Logf("single record: min %d bytes, one extent %d bytes, one extent + one bitmap extent %d bytes",
		ProtectedDeviceMinBinSize,
		ProtectedDeviceMinBinSize+ProtectedExtentFixedBinSize,
		ProtectedDeviceMinBinSize+ProtectedExtentFixedBinSize+DiskExtentBinSize)
}

func TestCreateWithOffset(t *testing.T) {
	path := tempFile(t)
	const offset int64 = 4096

	bm, err := Create(path, offset, 0, 0)
	if err != nil {
		t.Fatalf("Create with offset failed: %v", err)
	}
	defer bm.file.Close()

	if bm.offset != offset {
		t.Errorf("offset mismatch: got %d, want %d", bm.offset, offset)
	}

	// [0, offset) 前缀必须被写零而不是留成文件空洞，
	// 否则 FSCTL_GET_RETRIEVAL_POINTERS 会对空洞返回 LCN=-1，
	// PhysicalExtents 就会算出负偏移的错误物理段。
	prefix := make([]byte, offset)
	if _, err := bm.file.ReadAt(prefix, 0); err != nil {
		t.Fatalf("read prefix failed: %v", err)
	}
	if !bytes.Equal(prefix, make([]byte, offset)) {
		t.Error("prefix [0, offset) is not all zero")
	}

	addDevice(t, bm, DeviceTypeDisk, "disk-001", []DiskExtent{
		makeDiskExtent("disk0", 0, 4*1024*1024),
	})
	if _, err := bm.Flush(); err != nil {
		skipIfPermissionDenied(t, err)
		t.Fatalf("Flush failed: %v", err)
	}

	// 物理段必须完整覆盖 [offset, offset+Size())，且不能出现负偏移
	phyExtents, err := bm.PhysicalExtents()
	if err != nil {
		skipIfPermissionDenied(t, err)
		t.Fatalf("PhysicalExtents failed: %v", err)
	}
	var covered int64
	for i, e := range phyExtents {
		if e.Size == 0 {
			t.Errorf("extent %d is bogus: size=0", i)
		}
		covered += int64(e.Size)
	}
	if covered != bm.Size() {
		t.Errorf("physical extents cover %d bytes, metadata region is %d bytes", covered, bm.Size())
	}

	bm.file.Close()

	bm2, err := Load(path, offset)
	if err != nil {
		t.Fatalf("Load with offset failed: %v", err)
	}
	defer bm2.file.Close()

	if bm2.offset != offset {
		t.Errorf("offset mismatch: got %d, want %d", bm2.offset, offset)
	}
	pds, err := bm2.ListValidProtectDevice()
	if err != nil {
		t.Fatalf("ListValidProtectDevice failed: %v", err)
	}
	if len(pds) != 1 {
		t.Errorf("expected 1 device after reload, got %d", len(pds))
	}
}

// ============================================================================
// AddProtectDevice
// ============================================================================

func TestAddProtectDevice(t *testing.T) {
	bm := newMeta(t)

	extents := []DiskExtent{
		makeDiskExtent("disk0", 0, 128*1024*1024*1024), // 128 GiB = 1 bitmap unit
	}
	addDevice(t, bm, DeviceTypeDisk, "disk-001", extents)

	pds, err := bm.ListValidProtectDevice()
	if err != nil {
		t.Fatalf("ListValidProtectDevice failed: %v", err)
	}
	if len(pds) != 1 {
		t.Fatalf("expected 1 device, got %d", len(pds))
	}

	pd := pds[0]
	if pd.Type != DeviceTypeDisk {
		t.Error("device type mismatch")
	}
	if xutil.TrimZeroString(pd.DeviceID[:]) != "disk-001" {
		t.Errorf("device ID mismatch: got %q", xutil.TrimZeroString(pd.DeviceID[:]))
	}

	// Verify extents
	if len(pd.Extents) != 1 {
		t.Errorf("expected 1 extent, got %d", len(pd.Extents))
	}
	for _, pe := range pd.Extents {
		if pe.BitmapUnitCount == 0 {
			t.Error("BitmapUnitCount should not be 0")
		}
	}
}

func TestAddProtectDeviceDuplicate(t *testing.T) {
	bm := newMeta(t)

	extents := []DiskExtent{
		makeDiskExtent("disk0", 0, 128*1024*1024*1024),
	}
	addDevice(t, bm, DeviceTypeDisk, "disk-001", extents)

	// Try adding same device again
	err := bm.AddProtectDevice(DeviceTypeDisk, makeID("disk-001"), extents)
	if err == nil {
		t.Error("expected error for duplicate device, got nil")
	}
}

func TestAddProtectDeviceEmptyExtents(t *testing.T) {
	bm := newMeta(t)

	err := bm.AddProtectDevice(DeviceTypeDisk, makeID("disk-001"), nil)
	if err == nil {
		t.Error("expected error for empty extents, got nil")
	}
}

// TestAddProtectDeviceManyExtents 验证 Extents 数量没有格式层面的上限，
// 变长记录想装多少装多少，只受 Bitmap Unit 总量约束。
func TestAddProtectDeviceManyExtents(t *testing.T) {
	bm := newMeta(t)

	// 每个 extent 占 1 个 bitmap unit，总数受 TotalBitmapUnits 限制
	const numExtents = 1000

	extents := make([]DiskExtent, numExtents)
	for i := range extents {
		extents[i] = makeDiskExtent("disk0", uint64(i)*1024*1024, 1024*1024)
	}

	if err := bm.AddProtectDevice(DeviceTypeDisk, makeID("disk-001"), extents); err != nil {
		t.Fatalf("AddProtectDevice with %d extents failed: %v", numExtents, err)
	}

	pds, err := bm.ListValidProtectDevice()
	if err != nil {
		t.Fatalf("ListValidProtectDevice failed: %v", err)
	}
	if len(pds[0].Extents) != numExtents {
		t.Errorf("Extents count: got %d, want %d", len(pds[0].Extents), numExtents)
	}

	// 变长记录下记录大小随 Extents 数量线性增长
	wantSize := ProtectedDeviceMinBinSize + numExtents*ProtectedExtentFixedBinSize
	if got := calcProtectedDeviceBinSize(pds[0]); got != wantSize {
		t.Errorf("record size: got %d, want %d", got, wantSize)
	}
}

func TestAddProtectDeviceMultipleDevices(t *testing.T) {
	bm := newMeta(t)

	addDevice(t, bm, DeviceTypeDisk, "disk-001", []DiskExtent{
		makeDiskExtent("disk0", 0, 128*1024*1024*1024),
	})
	addDevice(t, bm, DeviceTypeVolume, "volume-001", []DiskExtent{
		makeDiskExtent("disk1", 0, 128*1024*1024*1024),
	})

	pds, err := bm.ListValidProtectDevice()
	if err != nil {
		t.Fatalf("ListValidProtectDevice failed: %v", err)
	}
	if len(pds) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(pds))
	}
}

func TestAddProtectDeviceExtentsOverlap(t *testing.T) {
	bm := newMeta(t)

	// 先加一个设备
	extents1 := []DiskExtent{
		makeDiskExtent("disk0", 0, 4096),
	}
	if err := bm.AddProtectDevice(DeviceTypeDisk, makeID("disk-001"), extents1); err != nil {
		t.Fatalf("first AddProtectDevice failed: %v", err)
	}

	// 尝试添加与已有 Extent 完全重叠的设备
	extents2 := []DiskExtent{
		makeDiskExtent("disk0", 1024, 2048),
	}
	if err := bm.AddProtectDevice(DeviceTypeVolume, makeID("vol-001"), extents2); err == nil {
		t.Error("expected error for overlapping extent")
	}

	// 不同磁盘的 Extent 不应冲突
	extents3 := []DiskExtent{
		makeDiskExtent("disk1", 0, 4096),
	}
	if err := bm.AddProtectDevice(DeviceTypeDisk, makeID("disk-002"), extents3); err != nil {
		t.Errorf("different disk should not conflict: %v", err)
	}

	// 相邻但不重叠的 Extent 应该可以添加
	extents4 := []DiskExtent{
		makeDiskExtent("disk0", 4096, 4096),
	}
	if err := bm.AddProtectDevice(DeviceTypeVolume, makeID("vol-002"), extents4); err != nil {
		t.Errorf("adjacent extent should not conflict: %v", err)
	}
}

func TestExtentsOverlap(t *testing.T) {
	tests := []struct {
		name string
		a, b DiskExtent
		want bool
	}{
		{
			"same disk, overlap",
			makeDiskExtent("d0", 0, 100),
			makeDiskExtent("d0", 50, 100),
			true,
		},
		{
			"same disk, adjacent (no overlap)",
			makeDiskExtent("d0", 0, 100),
			makeDiskExtent("d0", 100, 100),
			false,
		},
		{
			"same disk, separated",
			makeDiskExtent("d0", 0, 100),
			makeDiskExtent("d0", 200, 100),
			false,
		},
		{
			"different disk",
			makeDiskExtent("d0", 0, 100),
			makeDiskExtent("d1", 0, 100),
			false,
		},
		{
			"one contains the other",
			makeDiskExtent("d0", 0, 1000),
			makeDiskExtent("d0", 200, 100),
			true,
		},
		{
			"zero size",
			makeDiskExtent("d0", 0, 0),
			makeDiskExtent("d0", 0, 100),
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extentsOverlap(tt.a, tt.b); got != tt.want {
				t.Errorf("extentsOverlap(%s, %s) = %v, want %v",
					tt.a.String(), tt.b.String(), got, tt.want)
			}
		})
	}
}

// ============================================================================
// RemoveProtectDevice
// ============================================================================

func TestRemoveProtectDevice(t *testing.T) {
	bm := newMeta(t)

	addDevice(t, bm, DeviceTypeDisk, "disk-001", []DiskExtent{
		makeDiskExtent("disk0", 0, 128*1024*1024*1024),
	})
	addDevice(t, bm, DeviceTypeDisk, "disk-002", []DiskExtent{
		makeDiskExtent("disk1", 0, 128*1024*1024*1024),
	})

	// Remove first
	if err := bm.RemoveProtectDevice(makeID("disk-001")); err != nil {
		t.Fatalf("RemoveProtectDevice failed: %v", err)
	}

	pds, err := bm.ListValidProtectDevice()
	if err != nil {
		t.Fatalf("ListValidProtectDevice failed: %v", err)
	}
	if len(pds) != 1 {
		t.Fatalf("expected 1 device after removal, got %d", len(pds))
	}
	if xutil.TrimZeroString(pds[0].DeviceID[:]) != "disk-002" {
		t.Errorf("remaining device should be disk-002, got %q", xutil.TrimZeroString(pds[0].DeviceID[:]))
	}
}

func TestRemoveProtectDeviceNonExistent(t *testing.T) {
	bm := newMeta(t)

	// Should not error
	if err := bm.RemoveProtectDevice(makeID("nonexistent")); err != nil {
		t.Fatalf("RemoveProtectDevice for non-existent device should return nil, got: %v", err)
	}
}

func TestRemoveAllProtectDevice(t *testing.T) {
	bm := newMeta(t)

	addDevice(t, bm, DeviceTypeDisk, "disk-001", []DiskExtent{
		makeDiskExtent("disk0", 0, 128*1024*1024*1024),
	})
	addDevice(t, bm, DeviceTypeDisk, "disk-002", []DiskExtent{
		makeDiskExtent("disk1", 0, 128*1024*1024*1024),
	})

	if err := bm.RemoveAllProtectDevice(); err != nil {
		t.Fatalf("RemoveAllProtectDevice failed: %v", err)
	}

	pds, err := bm.ListValidProtectDevice()
	if err != nil {
		t.Fatalf("ListValidProtectDevice failed: %v", err)
	}
	if len(pds) != 0 {
		t.Errorf("expected 0 devices, got %d", len(pds))
	}
}

// ============================================================================
// Flush (persistence)
// ============================================================================

func TestFlushAndReload(t *testing.T) {
	path := tempFile(t)

	bm, err := Create(path, 0, 0, 0)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	defer bm.file.Close()

	extents := []DiskExtent{
		makeDiskExtent("diskA", 0, 128*1024*1024*1024),
		makeDiskExtent("diskB", 1048576, 256*1024*1024*1024),
	}
	if err := bm.AddProtectDevice(DeviceTypeDisk, makeID("disk-001"), extents); err != nil {
		t.Fatalf("AddProtectDevice failed: %v", err)
	}

	if _, err := bm.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}
	bm.file.Close()

	// Reload
	bm2, err := Load(path, 0)
	if err != nil {
		t.Fatalf("Load after flush failed: %v", err)
	}
	defer bm2.file.Close()

	pds, err := bm2.ListValidProtectDevice()
	if err != nil {
		t.Fatalf("ListValidProtectDevice failed: %v", err)
	}
	if len(pds) != 1 {
		t.Fatalf("expected 1 device after reload, got %d", len(pds))
	}

	pd := pds[0]
	if xutil.TrimZeroString(pd.DeviceID[:]) != "disk-001" {
		t.Errorf("device ID mismatch: got %q", xutil.TrimZeroString(pd.DeviceID[:]))
	}

	// Verify extents
	if len(pd.Extents) != 2 {
		t.Errorf("expected 2 extents, got %d", len(pd.Extents))
	}
	for _, pe := range pd.Extents {
		if pe.Extent.Size == 0 {
			t.Error("extent size should not be 0")
		}
	}
}

// ============================================================================
// ReadDeviceBitmap
// ============================================================================

func TestReadDeviceBitmap(t *testing.T) {
	bm := newMeta(t)

	bitIndexSpace := uint64(bm.header.BitIndexSpace)
	extSize := bitIndexSpace * 10 // 10 bits worth of data (40 MiB)
	extents := []DiskExtent{
		makeDiskExtent("disk0", 0, extSize),
	}
	addDevice(t, bm, DeviceTypeDisk, "disk-001", extents)

	// Initially all bits should be 0
	bitCount, bitmapData, err := bm.ReadDeviceBitmap(makeID("disk-001"))
	if err != nil {
		t.Fatalf("ReadDeviceBitmap failed: %v", err)
	}
	if bitCount != 10 {
		t.Errorf("bitCount: got %d, want 10", bitCount)
	}
	expectedBytes := (10 + 7) / 8
	if len(bitmapData) != int(expectedBytes) {
		t.Errorf("bitmapData length: got %d, want %d", len(bitmapData), expectedBytes)
	}

	// All bits should be 0
	for i := uint32(0); i < bitCount; i++ {
		if bitTest(bitmapData, uint64(i)) {
			t.Errorf("bit %d should be 0 initially", i)
		}
	}
}

func TestReadDeviceBitmapNonExistent(t *testing.T) {
	bm := newMeta(t)

	_, _, err := bm.ReadDeviceBitmap(makeID("nonexistent"))
	if err == nil {
		t.Error("expected error for non-existent device")
	}
}

func TestReadDeviceBitmapPartialLastUnit(t *testing.T) {
	bm := newMeta(t)

	bitIndexSpace := uint64(bm.header.BitIndexSpace)
	// 1001 bits: 125 full bytes + 1 bit
	// This ensures the last byte has only 1 valid bit
	extSize := bitIndexSpace * 1001
	extents := []DiskExtent{
		makeDiskExtent("disk0", 0, extSize),
	}
	addDevice(t, bm, DeviceTypeDisk, "disk-001", extents)

	bitCount, bitmapData, err := bm.ReadDeviceBitmap(makeID("disk-001"))
	if err != nil {
		t.Fatalf("ReadDeviceBitmap failed: %v", err)
	}
	if bitCount != 1001 {
		t.Errorf("bitCount: got %d, want 1001", bitCount)
	}
	expectedBytes := (1001 + 7) / 8
	if len(bitmapData) != int(expectedBytes) {
		t.Errorf("bitmapData length: got %d, want %d", len(bitmapData), expectedBytes)
	}
}

func TestReadDeviceBitmapMultiExtent(t *testing.T) {
	bm := newMeta(t)

	bitIndexSpace := uint64(bm.header.BitIndexSpace)
	extents := []DiskExtent{
		makeDiskExtent("diskA", 0, bitIndexSpace*5), // 5 bits
		makeDiskExtent("diskB", 0, bitIndexSpace*7), // 7 bits
		makeDiskExtent("diskC", 0, bitIndexSpace*3), // 3 bits
	}
	addDevice(t, bm, DeviceTypeDisk, "disk-001", extents)

	bitCount, bitmapData, err := bm.ReadDeviceBitmap(makeID("disk-001"))
	if err != nil {
		t.Fatalf("ReadDeviceBitmap failed: %v", err)
	}
	if bitCount != 15 {
		t.Errorf("bitCount: got %d, want 15", bitCount)
	}
	expectedBytes := (15 + 7) / 8
	if len(bitmapData) != int(expectedBytes) {
		t.Errorf("bitmapData length: got %d, want %d", len(bitmapData), expectedBytes)
	}
}

func TestReadDeviceBitmapWithDirtyBits(t *testing.T) {
	bm := newMeta(t)

	bitIndexSpace := uint64(bm.header.BitIndexSpace)
	extSize := bitIndexSpace * 100 // 100 bits = 12.5 bytes
	extents := []DiskExtent{
		makeDiskExtent("disk0", 0, extSize),
	}
	addDevice(t, bm, DeviceTypeDisk, "disk-001", extents)

	// Manually set some dirty bits in the bitmap unit data
	// The device has 1 extent, and the bitmap unit is at BitmapUnitStart
	pds, _ := bm.ListValidProtectDevice()
	pd := pds[0]
	unitIdx := pd.Extents[0].BitmapUnitStart

	// Write known pattern to bitmap unit
	unitOff := bm.offset + int64(bm.header.BitmapDataOffset) + int64(unitIdx)*int64(bm.header.BitmapClusterSize)
	pattern := make([]byte, bm.header.BitmapClusterSize)
	pattern[0] = 0x55 // 01010101
	pattern[1] = 0xAA // 10101010
	if _, err := bm.file.WriteAt(pattern, unitOff); err != nil {
		t.Fatalf("write pattern failed: %v", err)
	}

	bitCount, bitmapData, err := bm.ReadDeviceBitmap(makeID("disk-001"))
	if err != nil {
		t.Fatalf("ReadDeviceBitmap failed: %v", err)
	}
	if bitCount != 100 {
		t.Errorf("bitCount: got %d, want 100", bitCount)
	}

	// Verify pattern
	// byte 0: 0x55 = bits 0,2,4,6 set
	if !bitTest(bitmapData, 0) {
		t.Error("bit 0 should be set (0x55)")
	}
	if bitTest(bitmapData, 1) {
		t.Error("bit 1 should be clear (0x55)")
	}
	if !bitTest(bitmapData, 2) {
		t.Error("bit 2 should be set (0x55)")
	}

	// byte 1: 0xAA = bits 8,10,12,14 set
	if bitTest(bitmapData, 8) {
		t.Error("bit 8 should be clear (0xAA)")
	}
	if !bitTest(bitmapData, 9) {
		t.Error("bit 9 should be set (0xAA)")
	}
}

// ============================================================================
// ValidateHeader
// ============================================================================

func TestValidateHeaderValid(t *testing.T) {
	bm := newMeta(t)
	// validateHeader is called during Load, so just verify it didn't error
	bm.file.Close()

	bm2, err := Load(bm.filePath, 0)
	if err != nil {
		t.Fatalf("Load of valid file failed: %v", err)
	}
	bm2.file.Close()
}

func TestValidateHeaderBadSignature(t *testing.T) {
	bm := newMeta(t)

	// Corrupt signature
	h := bm.header
	copy(h.Signature[:], "BADHEADER")
	h.HeaderCRC32 = calcCRC32(&h)
	writeHeader(bm.file, bm.offset, &h)
	bm.file.Close()

	_, err := Load(bm.filePath, 0)
	if err == nil {
		t.Error("expected error for bad signature")
	}
}

func TestValidateHeaderBadCRC(t *testing.T) {
	bm := newMeta(t)

	// Corrupt CRC
	h := bm.header
	h.HeaderCRC32 = 0xDEADBEEF
	writeHeader(bm.file, bm.offset, &h)
	bm.file.Close()

	_, err := Load(bm.filePath, 0)
	if err == nil {
		t.Error("expected error for bad CRC")
	}
}

// ============================================================================
// ValidateProtectedDevice
// ============================================================================

func TestValidateProtectedDeviceInvalidType(t *testing.T) {
	h := defaultHeader(0, 0)
	allocMap := make([]byte, 1024)

	pd := ProtectedDevice{
		Type: DeviceType(999),
	}
	copy(pd.DeviceID[:], "test")

	err := validateProtectedDevice(&pd, &h, allocMap)
	if err == nil {
		t.Error("expected error for invalid DeviceType")
	}
}

func TestValidateProtectedDeviceEmptyID(t *testing.T) {
	h := defaultHeader(0, 0)
	allocMap := make([]byte, 1024)

	pd := ProtectedDevice{
		Type: DeviceTypeDisk,
		// DeviceID is all zeros
	}

	err := validateProtectedDevice(&pd, &h, allocMap)
	if err == nil {
		t.Error("expected error for empty DeviceID")
	}
}

func TestValidateProtectedDeviceExtentSizeZero(t *testing.T) {
	h := defaultHeader(0, 0)
	h.TotalBitmapUnits = 100
	allocMap := make([]byte, 1024)
	// Mark unit 0 as allocated
	setBitmapUnitAllocated(allocMap, 0, true)

	pd := ProtectedDevice{
		Type:    DeviceTypeDisk,
		Extents: make([]ProtectedExtent, 1),
	}
	copy(pd.DeviceID[:], "test")
	pd.Extents[0] = ProtectedExtent{
		BitmapUnitStart: 0,
		BitmapUnitCount: 1,
		Extent:          makeDiskExtent("disk0", 0, 0), // Size = 0
	}

	err := validateProtectedDevice(&pd, &h, allocMap)
	if err == nil {
		t.Error("expected error for Size=0")
	}
}

func TestValidateProtectedDeviceBitmapUnitStartOutOfRange(t *testing.T) {
	h := defaultHeader(0, 0)
	h.TotalBitmapUnits = 10
	allocMap := make([]byte, 1024)

	pd := ProtectedDevice{
		Type:    DeviceTypeDisk,
		Extents: make([]ProtectedExtent, 1),
	}
	copy(pd.DeviceID[:], "test")
	pd.Extents[0] = ProtectedExtent{
		BitmapUnitStart: 10, // == TotalBitmapUnits (out of range)
		BitmapUnitCount: 1,
		Extent:          makeDiskExtent("disk0", 0, 4096),
	}

	err := validateProtectedDevice(&pd, &h, allocMap)
	if err == nil {
		t.Error("expected error for BitmapUnitStart out of range")
	}
}

func TestValidateProtectedDeviceUnallocatedBitmapUnit(t *testing.T) {
	h := defaultHeader(0, 0)
	h.TotalBitmapUnits = 100
	allocMap := make([]byte, 1024)
	// Unit 0 is NOT allocated

	pd := ProtectedDevice{
		Type:    DeviceTypeDisk,
		Extents: make([]ProtectedExtent, 1),
	}
	copy(pd.DeviceID[:], "test")
	pd.Extents[0] = ProtectedExtent{
		BitmapUnitStart: 0,
		BitmapUnitCount: 1,
		Extent:          makeDiskExtent("disk0", 0, 4096),
	}

	err := validateProtectedDevice(&pd, &h, allocMap)
	if err == nil {
		t.Error("expected error for unallocated bitmap unit")
	}
}

func TestValidateProtectedDeviceCapacityInsufficient(t *testing.T) {
	h := defaultHeader(0, 0)
	h.TotalBitmapUnits = 100
	h.BitmapClusterSize = 4096
	h.BitIndexSpace = 4 * 1024 * 1024 // 4 MiB
	allocMap := make([]byte, 1024)
	setBitmapUnitAllocated(allocMap, 0, true)

	// 1 bitmap unit covers 128 GiB, but extent claims 200 GiB
	pd := ProtectedDevice{
		Type:    DeviceTypeDisk,
		Extents: make([]ProtectedExtent, 1),
	}
	copy(pd.DeviceID[:], "test")
	pd.Extents[0] = ProtectedExtent{
		BitmapUnitStart: 0,
		BitmapUnitCount: 1,
		Extent:          makeDiskExtent("disk0", 0, 200*1024*1024*1024), // 200 GiB
	}

	err := validateProtectedDevice(&pd, &h, allocMap)
	if err == nil {
		t.Error("expected error for insufficient capacity")
	}
}

// ============================================================================
// ValidateBitmapUnitNoOverlap
// ============================================================================

func TestValidateBitmapUnitNoOverlapValid(t *testing.T) {
	devices := []ProtectedDevice{
		{
			Type: DeviceTypeDisk,
			Extents: []ProtectedExtent{
				{BitmapUnitStart: 0, BitmapUnitCount: 10},
				{BitmapUnitStart: 10, BitmapUnitCount: 5},
				{BitmapUnitStart: 20, BitmapUnitCount: 1},
			},
		},
	}
	copy(devices[0].DeviceID[:], "test1")

	if err := validateBitmapUnitNoOverlap(devices); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateBitmapUnitNoOverlapInvalid(t *testing.T) {
	devices := []ProtectedDevice{
		{
			Type: DeviceTypeDisk,
			Extents: []ProtectedExtent{
				{BitmapUnitStart: 0, BitmapUnitCount: 10},
				{BitmapUnitStart: 5, BitmapUnitCount: 10}, // overlaps [0,10)
			},
		},
	}
	copy(devices[0].DeviceID[:], "test1")

	if err := validateBitmapUnitNoOverlap(devices); err == nil {
		t.Error("expected error for overlapping bitmap units")
	}
}

func TestValidateBitmapUnitNoOverlapAdjacent(t *testing.T) {
	// Adjacent ranges should be OK
	devices := []ProtectedDevice{
		{
			Type: DeviceTypeDisk,
			Extents: []ProtectedExtent{
				{BitmapUnitStart: 0, BitmapUnitCount: 10},
				{BitmapUnitStart: 10, BitmapUnitCount: 10}, // adjacent
			},
		},
	}
	copy(devices[0].DeviceID[:], "test1")

	if err := validateBitmapUnitNoOverlap(devices); err != nil {
		t.Errorf("adjacent ranges should be valid: %v", err)
	}
}

// ============================================================================
// trimZeroString
// ============================================================================

func TestTrimZeroString(t *testing.T) {
	tests := []struct {
		input    []byte
		expected string
	}{
		{[]byte("hello"), "hello"},
		{[]byte("hello\x00\x00"), "hello"},
		{[]byte("\x00\x00hello"), "\x00\x00hello"}, // leading zeros NOT trimmed
		{[]byte{}, ""},
		{[]byte{0, 0, 0}, ""},
	}

	for _, tt := range tests {
		got := xutil.TrimZeroString(tt.input)
		if got != tt.expected {
			t.Errorf("xutil.TrimZeroString(%v) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

// ============================================================================
// findDevice
// ============================================================================

func TestFindDevice(t *testing.T) {
	bm := newMeta(t)

	addDevice(t, bm, DeviceTypeDisk, "disk-001", []DiskExtent{
		makeDiskExtent("disk0", 0, 128*1024*1024*1024),
	})
	addDevice(t, bm, DeviceTypeVolume, "volume-001", []DiskExtent{
		makeDiskExtent("disk1", 0, 128*1024*1024*1024),
	})

	if pd := bm.findDevice(makeID("disk-001")); pd == nil {
		t.Error("findDevice should return non-nil for existing device")
	}
	if pd := bm.findDevice(makeID("volume-001")); pd == nil {
		t.Error("findDevice should return non-nil for existing device")
	}
	if pd := bm.findDevice(makeID("nonexistent")); pd != nil {
		t.Error("findDevice should return nil for non-existent device")
	}
}

// ============================================================================
// Add/Remove cycle: bitmap unit allocation reuse
// ============================================================================

func TestBitmapUnitReuseAfterRemove(t *testing.T) {
	bm := newMeta(t)

	bitIndexSpace := uint64(bm.header.BitIndexSpace)
	extSize := bitIndexSpace * 5 // 5 bits, needs 1 unit

	extents := []DiskExtent{
		makeDiskExtent("disk0", 0, extSize),
	}
	addDevice(t, bm, DeviceTypeDisk, "disk-001", extents)

	// Record allocated bitmap unit
	pds, _ := bm.ListValidProtectDevice()
	firstUnit := pds[0].Extents[0].BitmapUnitStart

	// Remove device
	if err := bm.RemoveProtectDevice(makeID("disk-001")); err != nil {
		t.Fatalf("RemoveProtectDevice failed: %v", err)
	}

	// Add again — should reuse the same unit
	addDevice(t, bm, DeviceTypeDisk, "disk-002", extents)
	pds, _ = bm.ListValidProtectDevice()
	reusedUnit := pds[0].Extents[0].BitmapUnitStart

	if reusedUnit != firstUnit {
		t.Errorf("expected reused unit %d, got %d", firstUnit, reusedUnit)
	}
}

// ============================================================================
// ListValidProtectDevice on empty
// ============================================================================

func TestListValidProtectDeviceEmpty(t *testing.T) {
	bm := newMeta(t)

	pds, err := bm.ListValidProtectDevice()
	if err != nil {
		t.Fatalf("ListValidProtectDevice failed: %v", err)
	}
	if len(pds) != 0 {
		t.Errorf("expected 0 devices, got %d", len(pds))
	}
}

// ============================================================================
// DefaultHeader
// ============================================================================

func TestDefaultHeader(t *testing.T) {
	h := defaultHeader(0, 0)

	if string(h.Signature[:len(SignatureStr)]) != SignatureStr {
		t.Error("default signature mismatch")
	}
	if h.Version != Versionv1_0 {
		t.Error("default version mismatch")
	}
	if h.BitIndexSpace != DefaultBitIndexSpace {
		t.Error("default BitIndexSpace mismatch")
	}
	if h.BitmapClusterSize != DefaultBitmapClusterSize {
		t.Error("default BitmapClusterSize mismatch")
	}
	if h.TotalBitmapUnits != DefaultTotalBitmapUnits {
		t.Error("default TotalBitmapUnits mismatch")
	}
}

// ============================================================================
// WriteHeader / ReadHeader
// ============================================================================

func TestWriteReadHeader(t *testing.T) {
	path := tempFile(t)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		t.Fatalf("open temp file failed: %v", err)
	}
	defer f.Close()
	defer os.Remove(path)

	h := defaultHeader(0, 0)
	h.CDPStatus = 1
	h.ErrorCode = 42
	h.WorkMode = 1
	h.HeaderCRC32 = calcCRC32(&h)

	if err := writeHeader(f, 0, &h); err != nil {
		t.Fatalf("writeHeader failed: %v", err)
	}

	h2, err := readHeader(f, 0)
	if err != nil {
		t.Fatalf("readHeader failed: %v", err)
	}

	if h2.CDPStatus != h.CDPStatus {
		t.Error("CDPStatus mismatch")
	}
	if h2.ErrorCode != h.ErrorCode {
		t.Error("ErrorCode mismatch")
	}
	if h2.WorkMode != h.WorkMode {
		t.Error("WorkMode mismatch")
	}
	if h2.HeaderCRC32 != h.HeaderCRC32 {
		t.Error("HeaderCRC32 mismatch")
	}
}

// ============================================================================
// 变长记录区增长
// ============================================================================

// TestManyDevicesGrowRegion 验证设备数量没有格式上限，区域随记录增长，
// 全部设备都能正确落盘并重载。
func TestManyDevicesGrowRegion(t *testing.T) {
	path := tempFile(t)

	bm, err := Create(path, 0, 0, 0)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	// 只验证记录区增长，伪装成设备路径避免依赖卷句柄（普通文件需要管理员权限）
	bm.filePath = `\\.\PHYSICALDRIVE0`
	defer bm.file.Close()

	const numDevices = 200
	for i := 0; i < numDevices; i++ {
		id := fmt.Sprintf("disk-%03d", i)
		if err := bm.AddProtectDevice(DeviceTypeDisk, makeID(id), []DiskExtent{
			makeDiskExtent(fmt.Sprintf("disk-%03d", i), 0, 4*1024*1024),
		}); err != nil {
			t.Fatalf("AddProtectDevice #%d (%s) failed: %v", i, id, err)
		}
	}

	pds, err := bm.ListValidProtectDevice()
	if err != nil {
		t.Fatalf("ListValidProtectDevice failed: %v", err)
	}
	if len(pds) != numDevices {
		t.Fatalf("expected %d devices, got %d", numDevices, len(pds))
	}

	baseSize := bm.Size()
	if _, err := bm.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// 区域随记录增长，且增长量与记录总字节数一致
	var recordsSize int
	for i := range bm.devices {
		recordsSize += calcProtectedDeviceBinSize(&bm.devices[i])
	}
	if bm.header.ProtectedRegionSize != alignUp(uint64(recordsSize)) {
		t.Errorf("ProtectedRegionSize: got %d, want %d",
			bm.header.ProtectedRegionSize, alignUp(uint64(recordsSize)))
	}
	if bm.Size() != baseSize+int64(bm.header.ProtectedRegionSize) {
		t.Errorf("Size: got %d, want %d", bm.Size(), baseSize+int64(bm.header.ProtectedRegionSize))
	}

	bm.file.Close()

	bm2, err := Load(path, 0)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	defer bm2.file.Close()

	pds2, err := bm2.ListValidProtectDevice()
	if err != nil {
		t.Fatalf("ListValidProtectDevice after reload failed: %v", err)
	}
	if len(pds2) != numDevices {
		t.Fatalf("expected %d devices after reload, got %d", numDevices, len(pds2))
	}
	for i := range pds2 {
		want := fmt.Sprintf("disk-%03d", i)
		if got := xutil.TrimZeroString(pds2[i].DeviceID[:]); got != want {
			t.Fatalf("device %d: got %q, want %q", i, got, want)
		}
	}
}

// TestRecordGrowthDoesNotCorruptOtherRegions 是变长布局的回归测试。
//
// 旧布局把 Protected Region 放在 Allocation Map 之前，记录在 ResolveBitmapExtents
// 之后会变长，一旦超出预留区就会覆盖 Allocation Map 和 Bitmap Unit Data，
// 而且超出多少取决于文件系统碎片数，所以表现为"偶尔失败"。
//
// 现在记录区在所有区域之后，增长不可能踩到任何其他区域。
func TestRecordGrowthDoesNotCorruptOtherRegions(t *testing.T) {
	path := tempFile(t)

	bm, err := Create(path, 0, 0, 0)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	bm.filePath = `\\.\PHYSICALDRIVE0`
	defer bm.file.Close()

	// 结构前提：记录区必须位于位图数据区之后
	bitmapDataEnd := bm.header.BitmapDataOffset + bm.header.BitmapDataTotalSize()
	if bm.header.ProtectedRegionOffset < bitmapDataEnd {
		t.Fatalf("protected region %d overlaps bitmap data ending at %d",
			bm.header.ProtectedRegionOffset, bitmapDataEnd)
	}

	for i := 0; i < 50; i++ {
		if err := bm.AddProtectDevice(DeviceTypeDisk, makeID(fmt.Sprintf("dev-%03d", i)), []DiskExtent{
			makeDiskExtent(fmt.Sprintf("devdisk-%03d-a", i), 0, 4*1024*1024),
			makeDiskExtent(fmt.Sprintf("devdisk-%03d-b", i), 0, 8*1024*1024),
		}); err != nil {
			t.Fatalf("AddProtectDevice #%d failed: %v", i, err)
		}
	}

	// 记录 Flush 前的 Allocation Map 与位图数据区内容
	wantAllocMap := make([]byte, len(bm.allocMap))
	copy(wantAllocMap, bm.allocMap)
	bitmapData := make([]byte, bm.header.BitmapDataTotalSize())
	if _, err := bm.file.ReadAt(bitmapData, int64(bm.header.BitmapDataOffset)); err != nil {
		t.Fatalf("read bitmap data failed: %v", err)
	}

	// Flush 会解析 BitmapExtents，记录随之变长
	sizeBefore := bm.Size()
	if _, err := bm.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}
	if bm.Size() <= sizeBefore {
		t.Errorf("region did not grow: before %d, after %d", sizeBefore, bm.Size())
	}

	// Allocation Map 必须原封不动
	gotAllocMap := make([]byte, bm.header.BitmapAllocMapSize)
	if _, err := bm.file.ReadAt(gotAllocMap, int64(bm.header.BitmapAllocMapOffset)); err != nil {
		t.Fatalf("read allocation map failed: %v", err)
	}
	if !bytes.Equal(gotAllocMap, wantAllocMap) {
		t.Error("allocation map was corrupted by record growth")
	}

	// 位图数据区必须原封不动
	gotBitmapData := make([]byte, bm.header.BitmapDataTotalSize())
	if _, err := bm.file.ReadAt(gotBitmapData, int64(bm.header.BitmapDataOffset)); err != nil {
		t.Fatalf("read bitmap data failed: %v", err)
	}
	if !bytes.Equal(gotBitmapData, bitmapData) {
		t.Error("bitmap unit data was corrupted by record growth")
	}

	// Header 里除记录区大小/数量/CRC 之外的字段必须没变
	if bm.header.BitmapAllocMapOffset != HeaderSize {
		t.Error("BitmapAllocMapOffset changed")
	}
	if bm.header.BitmapDataOffset != HeaderSize+bm.header.BitmapAllocMapSize {
		t.Error("BitmapDataOffset changed")
	}
	if bm.header.ProtectedRegionOffset != bitmapDataEnd {
		t.Error("ProtectedRegionOffset changed")
	}

	bm.file.Close()

	reloaded, err := Load(path, 0)
	if err != nil {
		t.Fatalf("Load after growth failed: %v", err)
	}
	defer reloaded.file.Close()

	pds, err := reloaded.ListValidProtectDevice()
	if err != nil {
		t.Fatalf("ListValidProtectDevice failed: %v", err)
	}
	if len(pds) != 50 {
		t.Errorf("expected 50 devices after reload, got %d", len(pds))
	}
}

// TestRemoveDeviceShrinksRegionAndZeroesTail 验证设备移除后区域缩小，
// 且腾出来的尾部被写零，不会残留已删除设备的记录。
//
// 区域大小按 4096 对齐，所以要用足够多的设备让缩小量超过对齐粒度。
func TestRemoveDeviceShrinksRegionAndZeroesTail(t *testing.T) {
	path := tempFile(t)

	bm, err := Create(path, 0, 0, 0)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	bm.filePath = `\\.\PHYSICALDRIVE0`
	defer bm.file.Close()

	extents := []DiskExtent{makeDiskExtent("disk0", 0, 4*1024*1024)}
	_ = extents
	const numDevices = 10
	for i := 0; i < numDevices; i++ {
		addDevice(t, bm, DeviceTypeDisk, fmt.Sprintf("disk-%03d", i), []DiskExtent{
			makeDiskExtent(fmt.Sprintf("disk-%03d", i), 0, 4*1024*1024),
		})
	}

	if _, err := bm.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}
	sizeWithAll := bm.header.ProtectedRegionSize
	if sizeWithAll == 0 {
		t.Fatal("region size should be non-zero after adding devices")
	}

	// 移除后一半设备
	for i := numDevices / 2; i < numDevices; i++ {
		if err := bm.RemoveProtectDevice(makeID(fmt.Sprintf("disk-%03d", i))); err != nil {
			t.Fatalf("RemoveProtectDevice #%d failed: %v", i, err)
		}
	}
	if _, err := bm.Flush(); err != nil {
		t.Fatalf("Flush after remove failed: %v", err)
	}

	sizeWithHalf := bm.header.ProtectedRegionSize
	if sizeWithHalf >= sizeWithAll {
		t.Errorf("region did not shrink: %d -> %d", sizeWithAll, sizeWithHalf)
	}
	if sizeWithHalf%AlignSize != 0 {
		t.Errorf("shrunk region size %d is not aligned to %d", sizeWithHalf, AlignSize)
	}

	// [新末尾, 旧末尾) 必须被写零，不能残留已删除设备的记录
	tail := make([]byte, sizeWithAll-sizeWithHalf)
	if _, err := bm.file.ReadAt(tail, int64(bm.header.ProtectedRegionOffset+sizeWithHalf)); err != nil {
		t.Fatalf("read freed tail failed: %v", err)
	}
	if !bytes.Equal(tail, make([]byte, len(tail))) {
		t.Error("freed region tail is not all zero")
	}

	bm.file.Close()

	bm2, err := Load(path, 0)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	defer bm2.file.Close()

	pds, err := bm2.ListValidProtectDevice()
	if err != nil {
		t.Fatalf("ListValidProtectDevice failed: %v", err)
	}
	if len(pds) != numDevices/2 {
		t.Fatalf("expected %d devices after reload, got %d", numDevices/2, len(pds))
	}
	for i := range pds {
		want := fmt.Sprintf("disk-%03d", i)
		if got := xutil.TrimZeroString(pds[i].DeviceID[:]); got != want {
			t.Errorf("device %d: got %q, want %q", i, got, want)
		}
	}
}

// TestRemoveAllDevicesEmptiesRegion 验证清空所有设备后区域大小归零，
// 且历史上写过的字节全部被清零。
func TestRemoveAllDevicesEmptiesRegion(t *testing.T) {
	path := tempFile(t)

	bm, err := Create(path, 0, 0, 0)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	bm.filePath = `\\.\PHYSICALDRIVE0`
	defer bm.file.Close()

	extents := []DiskExtent{makeDiskExtent("disk0", 0, 4*1024*1024)}
	_ = extents
	addDevice(t, bm, DeviceTypeDisk, "disk-001", []DiskExtent{
		makeDiskExtent("disk0", 0, 4*1024*1024),
	})
	addDevice(t, bm, DeviceTypeDisk, "disk-002", []DiskExtent{
		makeDiskExtent("disk1", 0, 4*1024*1024),
	})
	if _, err := bm.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}
	highWater := bm.header.ProtectedRegionSize

	if err := bm.RemoveAllProtectDevice(); err != nil {
		t.Fatalf("RemoveAllProtectDevice failed: %v", err)
	}
	if _, err := bm.Flush(); err != nil {
		t.Fatalf("Flush after remove all failed: %v", err)
	}

	if bm.header.ProtectedRegionSize != 0 {
		t.Errorf("ProtectedRegionSize: got %d, want 0", bm.header.ProtectedRegionSize)
	}
	if bm.Size() != int64(bm.header.ProtectedRegionOffset) {
		t.Errorf("Size: got %d, want %d", bm.Size(), bm.header.ProtectedRegionOffset)
	}

	// 历史高水位以下必须全零
	region := make([]byte, highWater)
	if _, err := bm.file.ReadAt(region, int64(bm.header.ProtectedRegionOffset)); err != nil {
		t.Fatalf("read region failed: %v", err)
	}
	if !bytes.Equal(region, make([]byte, highWater)) {
		t.Error("region is not all zero after removing all devices")
	}

	bm.file.Close()

	bm2, err := Load(path, 0)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	defer bm2.file.Close()

	pds, err := bm2.ListValidProtectDevice()
	if err != nil {
		t.Fatalf("ListValidProtectDevice failed: %v", err)
	}
	if len(pds) != 0 {
		t.Errorf("expected 0 devices, got %d", len(pds))
	}
}

// ============================================================================
// MD5 一致性：文件读取 vs PhysicalExtents 物理磁盘读取
// ============================================================================

// TestMD5Consistency 校验"通过文件句柄读到的字节"与"通过物理磁盘偏移读到的字节"完全一致。
//
// 驱动是按物理偏移直读元数据的，因此这份一致性是硬要求：
// Create 必须把整个区域写零并完整分配，否则 NTFS 有效数据长度之外的簇
// 在文件 API 下读出为 0、在物理磁盘上却是旧数据，两边就会不一致。
func TestMD5Consistency(t *testing.T) {
	// 使用 os.TempDir() 而非 t.TempDir()，确保文件系统支持 FSCTL_GET_RETRIEVAL_POINTERS
	f, err := os.CreateTemp(os.TempDir(), "biotrkmeta_md5_*.bin")
	if err != nil {
		t.Fatalf("CreateTemp failed: %v", err)
	}
	path := f.Name()
	f.Close()
	os.Remove(path)

	bm, err := Create(path, 0, 0, 0)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	defer func() {
		bm.file.Close()
		os.Remove(path)
	}()

	bitIndexSpace := uint64(bm.header.BitIndexSpace)
	extents := []DiskExtent{
		makeDiskExtent("diskA", 0, bitIndexSpace*100),
		makeDiskExtent("diskB", 1048576, bitIndexSpace*50),
	}
	addDevice(t, bm, DeviceTypeDisk, "disk-001", extents)
	addDevice(t, bm, DeviceTypeVolume, "volume-001", []DiskExtent{
		makeDiskExtent("diskC", 0, bitIndexSpace*30),
	})

	if _, err := bm.Flush(); err != nil {
		skipIfPermissionDenied(t, err)
		t.Fatalf("Flush failed: %v", err)
	}

	// 从文件句柄读取元数据区域并计算 MD5
	md5File, err := md5HashFromReader(bm.file, bm.offset, bm.Size())
	if err != nil {
		t.Fatalf("hash from file failed: %v", err)
	}
	t.Logf("File MD5: %s", md5File)

	// 从 PhysicalExtents 打开物理磁盘读取并计算 MD5
	phyExtents, err := bm.PhysicalExtents()
	if err != nil {
		skipIfPermissionDenied(t, err)
		t.Fatalf("PhysicalExtents failed: %v", err)
	}
	t.Logf("PhysicalExtents: %d extents", len(phyExtents))
	for i, e := range phyExtents {
		t.Logf("  [%d] disk=%s start=%d size=%d", i, e.DiskID.String(), e.Start, e.Size)
	}

	// 物理区间必须完整覆盖元数据区域，缺一段就意味着文件有空洞或未分配簇
	var covered int64
	for _, e := range phyExtents {
		covered += int64(e.Size)
	}
	if covered != bm.Size() {
		t.Fatalf("physical extents cover %d bytes, metadata region is %d bytes", covered, bm.Size())
	}

	md5Disk, err := md5HashFromExtents(phyExtents)
	if err != nil {
		skipIfPermissionDenied(t, err)
		t.Fatalf("hash from physical extents failed: %v", err)
	}
	t.Logf("Disk MD5: %s", md5Disk)

	if md5File != md5Disk {
		t.Fatalf("MD5 mismatch!\n  file: %s\n  disk: %s", md5File, md5Disk)
	}
	t.Log("MD5 file == disk: OK")
}

// skipIfPermissionDenied 在权限不足（打开卷句柄或裸设备需要管理员权限）时跳过用例。
// 其他错误一律不放行，仍然按失败处理。
func skipIfPermissionDenied(t *testing.T, err error) {
	t.Helper()
	if err != nil && errors.Is(err, fs.ErrPermission) {
		t.Skipf("requires administrator privileges: %v", err)
	}
}

// md5HashFromReader 从 io.ReaderAt 的指定偏移处读取 size 字节并计算 MD5 十六进制字符串。
func md5HashFromReader(r io.ReaderAt, offset int64, size int64) (string, error) {
	h := md5.New()
	buf := make([]byte, 64<<10)
	remain := size
	for remain > 0 {
		n := int64(len(buf))
		if n > remain {
			n = remain
		}
		if _, err := r.ReadAt(buf[:n], offset); err != nil {
			return "", fmt.Errorf("read at %d: %w", offset, err)
		}
		h.Write(buf[:n])
		offset += n
		remain -= n
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// md5HashFromExtents 从物理磁盘 extents 读取数据并计算 MD5 十六进制字符串。
func md5HashFromExtents(extents []DiskExtent) (string, error) {
	h := md5.New()
	buf := make([]byte, 64<<10)

	for _, e := range extents {
		diskPath, err := e.DiskID.DevicePath()
		if err != nil {
			return "", fmt.Errorf("resolve disk path for %s: %w", e.DiskID.String(), err)
		}
		f, err := os.OpenFile(diskPath, os.O_RDONLY, 0)
		if err != nil {
			return "", fmt.Errorf("open disk %s: %w", diskPath, err)
		}

		offset := int64(e.Start)
		remain := int64(e.Size)
		for remain > 0 {
			n := int64(len(buf))
			if n > remain {
				n = remain
			}
			if _, err := f.ReadAt(buf[:n], offset); err != nil && err != io.EOF {
				f.Close()
				return "", fmt.Errorf("read disk %s at %d: %w", diskPath, offset, err)
			}
			h.Write(buf[:n])
			offset += n
			remain -= n
		}
		f.Close()
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// ============================================================================
// ResolveBitmapExtents
// ============================================================================

func TestResolveBitmapExtentsDevicePath(t *testing.T) {
	path := tempFile(t)

	bm, err := Create(path, 0, 0, 0)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	defer bm.file.Close()

	bm.filePath = `\\.\PHYSICALDRIVE0`
	bm.offset = 4096

	extents := []DiskExtent{
		makeDiskExtent("disk0", 0, 128*1024*1024),
	}
	if err := bm.AddProtectDevice(DeviceTypeDisk, makeID("test-disk"), extents); err != nil {
		t.Fatalf("AddProtectDevice failed: %v", err)
	}

	if err := bm.ResolveBitmapExtents(); err != nil {
		t.Fatalf("ResolveBitmapExtents failed: %v", err)
	}

	pd := bm.findDevice(makeID("test-disk"))
	if pd == nil {
		t.Fatal("device not found")
	}
	pe := &pd.Extents[0]
	if len(pe.BitmapExtents) != 1 {
		t.Fatalf("expected 1 bitmap extent, got %d", len(pe.BitmapExtents))
	}

	be := pe.BitmapExtents[0]
	if be.DiskID.String() != `\\.\PHYSICALDRIVE0` {
		t.Errorf("DiskID mismatch: got %q", be.DiskID.String())
	}

	expectedStart := uint64(bm.offset) + bm.header.BitmapDataOffset + pe.BitmapUnitStart*uint64(bm.header.BitmapClusterSize)
	if be.Start != expectedStart {
		t.Errorf("Start mismatch: got %d, want %d", be.Start, expectedStart)
	}

	expectedSize := pe.BitmapUnitCount * uint64(bm.header.BitmapClusterSize)
	if be.Size != expectedSize {
		t.Errorf("Size mismatch: got %d, want %d", be.Size, expectedSize)
	}
}

func TestResolveBitmapExtentsMultipleDevices(t *testing.T) {
	path := tempFile(t)

	bm, err := Create(path, 0, 0, 0)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	defer bm.file.Close()

	bm.filePath = `\\.\PHYSICALDRIVE0`
	bm.offset = 8192

	extents1 := []DiskExtent{makeDiskExtent("disk0", 0, 4*1024*1024)}
	extents2 := []DiskExtent{makeDiskExtent("disk1", 0, 64*1024*1024)}

	if err := bm.AddProtectDevice(DeviceTypeDisk, makeID("dev1"), extents1); err != nil {
		t.Fatalf("AddProtectDevice dev1 failed: %v", err)
	}
	if err := bm.AddProtectDevice(DeviceTypeDisk, makeID("dev2"), extents2); err != nil {
		t.Fatalf("AddProtectDevice dev2 failed: %v", err)
	}

	if err := bm.ResolveBitmapExtents(); err != nil {
		t.Fatalf("ResolveBitmapExtents failed: %v", err)
	}

	pd1 := bm.findDevice(makeID("dev1"))
	pd2 := bm.findDevice(makeID("dev2"))

	pe1 := &pd1.Extents[0]
	if len(pe1.BitmapExtents) != 1 {
		t.Fatalf("dev1: expected 1 bitmap extent, got %d", len(pe1.BitmapExtents))
	}
	if pe1.BitmapExtents[0].Size != uint64(bm.header.BitmapClusterSize) {
		t.Errorf("dev1: size mismatch")
	}

	pe2 := &pd2.Extents[0]
	if len(pe2.BitmapExtents) != 1 {
		t.Fatalf("dev2: expected 1 bitmap extent, got %d", len(pe2.BitmapExtents))
	}
	expectedSize2 := pe2.BitmapUnitCount * uint64(bm.header.BitmapClusterSize)
	if pe2.BitmapExtents[0].Size != expectedSize2 {
		t.Errorf("dev2: size mismatch: got %d, want %d", pe2.BitmapExtents[0].Size, expectedSize2)
	}
}

func TestResolveBitmapExtentsMultiExtentDevice(t *testing.T) {
	path := tempFile(t)

	bm, err := Create(path, 0, 0, 0)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	defer bm.file.Close()

	bm.filePath = `\\.\PHYSICALDRIVE1`
	bm.offset = 0

	extents := []DiskExtent{
		makeDiskExtent("disk0", 0, 4*1024*1024),
		makeDiskExtent("disk0", 8*1024*1024, 8*1024*1024),
	}
	if err := bm.AddProtectDevice(DeviceTypeDisk, makeID("multi-ext"), extents); err != nil {
		t.Fatalf("AddProtectDevice failed: %v", err)
	}

	if err := bm.ResolveBitmapExtents(); err != nil {
		t.Fatalf("ResolveBitmapExtents failed: %v", err)
	}

	pd := bm.findDevice(makeID("multi-ext"))
	if len(pd.Extents) != 2 {
		t.Fatalf("expected 2 extents, got %d", len(pd.Extents))
	}

	for i, pe := range pd.Extents {
		if len(pe.BitmapExtents) == 0 {
			t.Errorf("extent %d: no bitmap extents resolved", i)
			continue
		}
		if pe.BitmapExtentCount != uint32(len(pe.BitmapExtents)) {
			t.Errorf("extent %d: BitmapExtentCount %d != len %d",
				i, pe.BitmapExtentCount, len(pe.BitmapExtents))
		}
		totalSize := uint64(0)
		for _, be := range pe.BitmapExtents {
			totalSize += be.Size
		}
		expectedSize := pe.BitmapUnitCount * uint64(bm.header.BitmapClusterSize)
		if totalSize != expectedSize {
			t.Errorf("extent %d: total bitmap size %d != expected %d", i, totalSize, expectedSize)
		}
	}
}

func TestResolveBitmapExtentsFlushRoundTrip(t *testing.T) {
	path := tempFile(t)

	bm, err := Create(path, 0, 0, 0)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// 模拟设备路径（offset=0，文件已按 Size() 预分配）
	bm.filePath = `\\.\PHYSICALDRIVE0`
	defer bm.file.Close()

	extents := []DiskExtent{
		makeDiskExtent("disk0", 0, 16*1024*1024),
	}
	if err := bm.AddProtectDevice(DeviceTypeDisk, makeID("roundtrip"), extents); err != nil {
		t.Fatalf("AddProtectDevice failed: %v", err)
	}

	if _, err := bm.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	pd := bm.findDevice(makeID("roundtrip"))
	if len(pd.Extents[0].BitmapExtents) == 0 {
		t.Fatal("BitmapExtents not resolved after Flush")
	}

	bm.file.Close()

	bm2, err := Load(path, 0)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	defer bm2.file.Close()

	pd2 := bm2.findDevice(makeID("roundtrip"))
	if pd2 == nil {
		t.Fatal("device not found after reload")
	}

	pe2 := &pd2.Extents[0]
	if len(pe2.BitmapExtents) == 0 {
		t.Fatal("BitmapExtents lost after reload")
	}
	if pe2.BitmapExtentCount != uint32(len(pe2.BitmapExtents)) {
		t.Errorf("BitmapExtentCount mismatch: %d != %d", pe2.BitmapExtentCount, len(pe2.BitmapExtents))
	}

	be := pe2.BitmapExtents[0]
	if be.DiskID.String() != `\\.\PHYSICALDRIVE0` {
		t.Errorf("DiskID mismatch after reload: got %q", be.DiskID.String())
	}
	expectedStart := bm2.header.BitmapDataOffset + pe2.BitmapUnitStart*uint64(bm2.header.BitmapClusterSize)
	if be.Start != expectedStart {
		t.Errorf("Start mismatch after reload: got %d, want %d", be.Start, expectedStart)
	}
	expectedSize := pe2.BitmapUnitCount * uint64(bm2.header.BitmapClusterSize)
	if be.Size != expectedSize {
		t.Errorf("Size mismatch after reload: got %d, want %d", be.Size, expectedSize)
	}
}

func TestResolveBitmapExtentsBoundary(t *testing.T) {
	path := tempFile(t)

	bm, err := Create(path, 0, 0, 0)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	defer bm.file.Close()

	bm.filePath = `\\.\PHYSICALDRIVE0`
	bm.offset = 0

	clusterSize := uint64(bm.header.BitmapClusterSize)
	totalUnits := uint64(bm.header.TotalBitmapUnits)
	bitmapDataStart := bm.header.BitmapDataOffset
	bitmapDataEnd := bitmapDataStart + clusterSize*totalUnits

	extents := []DiskExtent{
		makeDiskExtent("disk0", 0, clusterSize*8),
	}
	if err := bm.AddProtectDevice(DeviceTypeDisk, makeID("first-unit"), extents); err != nil {
		t.Fatalf("AddProtectDevice failed: %v", err)
	}

	if err := bm.ResolveBitmapExtents(); err != nil {
		t.Fatalf("ResolveBitmapExtents failed: %v", err)
	}

	pd := bm.findDevice(makeID("first-unit"))
	pe := &pd.Extents[0]

	if pe.BitmapUnitStart != 0 {
		t.Errorf("expected BitmapUnitStart=0, got %d", pe.BitmapUnitStart)
	}
	if pe.BitmapExtents[0].Start != bitmapDataStart {
		t.Errorf("first unit start: got %d, want %d", pe.BitmapExtents[0].Start, bitmapDataStart)
	}

	for _, be := range pe.BitmapExtents {
		if be.Start < bitmapDataStart || be.Start+be.Size > bitmapDataEnd {
			t.Errorf("bitmap extent out of range: [%d, %d) not in [%d, %d)",
				be.Start, be.Start+be.Size, bitmapDataStart, bitmapDataEnd)
		}
	}
}

func TestResolveBitmapExtentsAllUnitsInRange(t *testing.T) {
	path := tempFile(t)

	bm, err := Create(path, 0, 0, 0)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	defer bm.file.Close()

	bm.filePath = `\\.\PHYSICALDRIVE0`
	bm.offset = 0

	clusterSize := uint64(bm.header.BitmapClusterSize)
	totalUnits := uint64(bm.header.TotalBitmapUnits)
	bitmapDataStart := bm.header.BitmapDataOffset
	bitmapDataEnd := bitmapDataStart + clusterSize*totalUnits

	for i := 0; i < 5; i++ {
		extents := []DiskExtent{
			makeDiskExtent("disk0", uint64(i)*clusterSize*8, clusterSize*8),
		}
		id := fmt.Sprintf("dev-%d", i)
		if err := bm.AddProtectDevice(DeviceTypeDisk, makeID(id), extents); err != nil {
			t.Fatalf("AddProtectDevice %s failed: %v", id, err)
		}
	}

	if err := bm.ResolveBitmapExtents(); err != nil {
		t.Fatalf("ResolveBitmapExtents failed: %v", err)
	}

	for _, pd := range bm.devices {
		for ei, pe := range pd.Extents {
			if len(pe.BitmapExtents) == 0 {
				t.Errorf("device %s extent %d: no bitmap extents", xutil.TrimZeroString(pd.DeviceID[:]), ei)
				continue
			}

			totalBitmapSize := uint64(0)
			for _, be := range pe.BitmapExtents {
				if be.Start < bitmapDataStart || be.Start+be.Size > bitmapDataEnd {
					t.Errorf("device %s extent %d: bitmap extent [%d, %d) out of data range [%d, %d)",
						xutil.TrimZeroString(pd.DeviceID[:]), ei,
						be.Start, be.Start+be.Size,
						bitmapDataStart, bitmapDataEnd)
				}
				totalBitmapSize += be.Size
			}

			expectedSize := pe.BitmapUnitCount * clusterSize
			if totalBitmapSize != expectedSize {
				t.Errorf("device %s extent %d: total bitmap size %d != expected %d",
					xutil.TrimZeroString(pd.DeviceID[:]), ei, totalBitmapSize, expectedSize)
			}
		}
	}
}

func TestResolveBitmapExtentsNoDevices(t *testing.T) {
	path := tempFile(t)

	bm, err := Create(path, 0, 0, 0)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	defer bm.file.Close()

	bm.filePath = `\\.\PHYSICALDRIVE0`

	if err := bm.ResolveBitmapExtents(); err != nil {
		t.Fatalf("ResolveBitmapExtents with no devices failed: %v", err)
	}
}
