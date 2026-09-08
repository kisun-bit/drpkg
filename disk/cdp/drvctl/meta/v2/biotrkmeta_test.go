package biotrkmeta

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
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
	bm, err := Create(tempFile(t), 0)
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
	h := defaultHeader()
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
// Binary Encoding Round-Trip
// ============================================================================

func TestDiskIDPackUnpack(t *testing.T) {
	orig := DiskID{
		Major: 1,
		Minor: 2,
	}
	copy(orig.ID[:], "test-disk-id")

	buf := new(bytes.Buffer)
	packDiskID(buf, &orig)

	if buf.Len() != DiskIDBinSize {
		t.Errorf("packed size: got %d, want %d", buf.Len(), DiskIDBinSize)
	}

	var decoded DiskID
	if err := unpackDiskID(bytes.NewReader(buf.Bytes()), &decoded); err != nil {
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

	buf := new(bytes.Buffer)
	packDiskExtent(buf, &orig)

	if buf.Len() != DiskExtentBinSize {
		t.Errorf("packed size: got %d, want %d", buf.Len(), DiskExtentBinSize)
	}

	var decoded DiskExtent
	if err := unpackDiskExtent(bytes.NewReader(buf.Bytes()), &decoded); err != nil {
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
		IsValid:         true,
		BitmapUnitStart: 100,
		BitmapUnitCount: 6,
	}
	orig.Extent.Start = 4096
	orig.Extent.Size = 8192
	copy(orig.Extent.DiskID.ID[:], "extent-disk")

	buf := new(bytes.Buffer)
	packProtectedExtent(buf, &orig)

	if buf.Len() != ProtectedExtentBinSize {
		t.Errorf("packed size: got %d, want %d", buf.Len(), ProtectedExtentBinSize)
	}

	var decoded ProtectedExtent
	if err := unpackProtectedExtent(bytes.NewReader(buf.Bytes()), &decoded); err != nil {
		t.Fatalf("unpackProtectedExtent failed: %v", err)
	}

	if decoded.IsValid != orig.IsValid {
		t.Error("IsValid mismatch")
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

	// Test IsValid = false
	orig2 := orig
	orig2.IsValid = false
	buf2 := new(bytes.Buffer)
	packProtectedExtent(buf2, &orig2)

	var decoded2 ProtectedExtent
	if err := unpackProtectedExtent(bytes.NewReader(buf2.Bytes()), &decoded2); err != nil {
		t.Fatalf("unpackProtectedExtent failed: %v", err)
	}
	if decoded2.IsValid {
		t.Error("IsValid should be false")
	}
}

func TestProtectedDevicePackUnpack(t *testing.T) {
	orig := ProtectedDevice{
		Type: DeviceTypeVolume,
	}
	copy(orig.DeviceID[:], "volume-001")
	orig.Extents[0] = ProtectedExtent{
		IsValid:         true,
		BitmapUnitStart: 0,
		BitmapUnitCount: 4,
	}
	orig.Extents[0].Extent = makeDiskExtent("diskA", 0, 4096)
	orig.Extents[1] = ProtectedExtent{
		IsValid:         true,
		BitmapUnitStart: 4,
		BitmapUnitCount: 2,
	}
	orig.Extents[1].Extent = makeDiskExtent("diskB", 1048576, 2048)

	buf := new(bytes.Buffer)
	packProtectedDevice(buf, &orig)

	if buf.Len() != ProtectedDeviceRecordSize {
		t.Errorf("packed size: got %d, want %d", buf.Len(), ProtectedDeviceRecordSize)
	}

	var decoded ProtectedDevice
	if err := unpackProtectedDevice(bytes.NewReader(buf.Bytes()), &decoded); err != nil {
		t.Fatalf("unpackProtectedDevice failed: %v", err)
	}

	if decoded.Type != orig.Type {
		t.Error("Type mismatch")
	}
	if !bytes.Equal(decoded.DeviceID[:], orig.DeviceID[:]) {
		t.Error("DeviceID mismatch")
	}
	for i := range orig.Extents {
		if decoded.Extents[i].IsValid != orig.Extents[i].IsValid {
			t.Errorf("Extents[%d].IsValid mismatch", i)
		}
		if decoded.Extents[i].BitmapUnitStart != orig.Extents[i].BitmapUnitStart {
			t.Errorf("Extents[%d].BitmapUnitStart mismatch", i)
		}
		if decoded.Extents[i].BitmapUnitCount != orig.Extents[i].BitmapUnitCount {
			t.Errorf("Extents[%d].BitmapUnitCount mismatch", i)
		}
	}
}

// ============================================================================
// Create / Load
// ============================================================================

func TestCreateAndLoad(t *testing.T) {
	path := tempFile(t)

	bm, err := Create(path, 0)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify header
	h := bm.header
	if string(h.Signature[:len(SignatureStr)]) != SignatureStr {
		t.Error("signature mismatch")
	}
	if h.Version != VersionV1_0 {
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
	if h.ProtectedRegionSize != 0 {
		t.Error("ProtectedRegionSize should be 0")
	}
	if h.BitmapAllocMapOffset != HeaderSize+uint64(DefaultMaxProtectedDevices)*ProtectedDeviceRecordSize {
		t.Error("BitmapAllocMapOffset mismatch")
	}

	bm.file.Close()

	// Reload
	bm2, err := Load(path, 0)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	defer bm2.file.Close()

	if bm2.header.Version != VersionV1_0 {
		t.Error("reloaded version mismatch")
	}
	if bm2.header.ProtectedDeviceCount != 0 {
		t.Error("reloaded device count should be 0")
	}
}

func TestCreateWithOffset(t *testing.T) {
	path := tempFile(t)
	const offset int64 = 4096

	bm, err := Create(path, offset)
	if err != nil {
		t.Fatalf("Create with offset failed: %v", err)
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
	if trimZeroString(pd.DeviceID[:]) != "disk-001" {
		t.Errorf("device ID mismatch: got %q", trimZeroString(pd.DeviceID[:]))
	}

	// Verify extents
	validExtents := 0
	for _, pe := range pd.Extents {
		if pe.IsValid {
			validExtents++
			if pe.BitmapUnitCount == 0 {
				t.Error("BitmapUnitCount should not be 0")
			}
		}
	}
	if validExtents != 1 {
		t.Errorf("expected 1 valid extent, got %d", validExtents)
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

func TestAddProtectDeviceTooManyExtents(t *testing.T) {
	bm := newMeta(t)

	extents := make([]DiskExtent, MaxExtentsPerDevice+1)
	for i := range extents {
		extents[i] = makeDiskExtent("disk0", 0, 1)
	}

	err := bm.AddProtectDevice(DeviceTypeDisk, makeID("disk-001"), extents)
	if err == nil {
		t.Error("expected error for too many extents, got nil")
	}
}

func TestAddProtectDeviceMultipleDevices(t *testing.T) {
	bm := newMeta(t)

	extents := []DiskExtent{
		makeDiskExtent("disk0", 0, 128*1024*1024*1024),
	}
	addDevice(t, bm, DeviceTypeDisk, "disk-001", extents)
	addDevice(t, bm, DeviceTypeVolume, "volume-001", extents)

	pds, err := bm.ListValidProtectDevice()
	if err != nil {
		t.Fatalf("ListValidProtectDevice failed: %v", err)
	}
	if len(pds) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(pds))
	}
}

// ============================================================================
// RemoveProtectDevice
// ============================================================================

func TestRemoveProtectDevice(t *testing.T) {
	bm := newMeta(t)

	extents := []DiskExtent{
		makeDiskExtent("disk0", 0, 128*1024*1024*1024),
	}
	addDevice(t, bm, DeviceTypeDisk, "disk-001", extents)
	addDevice(t, bm, DeviceTypeDisk, "disk-002", extents)

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
	if trimZeroString(pds[0].DeviceID[:]) != "disk-002" {
		t.Errorf("remaining device should be disk-002, got %q", trimZeroString(pds[0].DeviceID[:]))
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

	extents := []DiskExtent{
		makeDiskExtent("disk0", 0, 128*1024*1024*1024),
	}
	addDevice(t, bm, DeviceTypeDisk, "disk-001", extents)
	addDevice(t, bm, DeviceTypeDisk, "disk-002", extents)

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

	bm, err := Create(path, 0)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	extents := []DiskExtent{
		makeDiskExtent("diskA", 0, 128*1024*1024*1024),
		makeDiskExtent("diskB", 1048576, 256*1024*1024*1024),
	}
	if err := bm.AddProtectDevice(DeviceTypeDisk, makeID("disk-001"), extents); err != nil {
		t.Fatalf("AddProtectDevice failed: %v", err)
	}

	if err := bm.Flush(); err != nil {
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
	if trimZeroString(pd.DeviceID[:]) != "disk-001" {
		t.Errorf("device ID mismatch: got %q", trimZeroString(pd.DeviceID[:]))
	}

	// Verify extents
	validCount := 0
	for _, pe := range pd.Extents {
		if pe.IsValid {
			validCount++
			if pe.Extent.Size == 0 {
				t.Error("extent size should not be 0")
			}
		}
	}
	if validCount != 2 {
		t.Errorf("expected 2 valid extents, got %d", validCount)
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
	h := defaultHeader()
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
	h := defaultHeader()
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
	h := defaultHeader()
	h.TotalBitmapUnits = 100
	allocMap := make([]byte, 1024)
	// Mark unit 0 as allocated
	setBitmapUnitAllocated(allocMap, 0, true)

	pd := ProtectedDevice{
		Type: DeviceTypeDisk,
	}
	copy(pd.DeviceID[:], "test")
	pd.Extents[0] = ProtectedExtent{
		IsValid:         true,
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
	h := defaultHeader()
	h.TotalBitmapUnits = 10
	allocMap := make([]byte, 1024)

	pd := ProtectedDevice{
		Type: DeviceTypeDisk,
	}
	copy(pd.DeviceID[:], "test")
	pd.Extents[0] = ProtectedExtent{
		IsValid:         true,
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
	h := defaultHeader()
	h.TotalBitmapUnits = 100
	allocMap := make([]byte, 1024)
	// Unit 0 is NOT allocated

	pd := ProtectedDevice{
		Type: DeviceTypeDisk,
	}
	copy(pd.DeviceID[:], "test")
	pd.Extents[0] = ProtectedExtent{
		IsValid:         true,
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
	h := defaultHeader()
	h.TotalBitmapUnits = 100
	h.BitmapClusterSize = 4096
	h.BitIndexSpace = 4 * 1024 * 1024 // 4 MiB
	allocMap := make([]byte, 1024)
	setBitmapUnitAllocated(allocMap, 0, true)

	// 1 bitmap unit covers 128 GiB, but extent claims 200 GiB
	pd := ProtectedDevice{
		Type: DeviceTypeDisk,
	}
	copy(pd.DeviceID[:], "test")
	pd.Extents[0] = ProtectedExtent{
		IsValid:         true,
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
			Extents: [128]ProtectedExtent{
				{IsValid: true, BitmapUnitStart: 0, BitmapUnitCount: 10},
				{IsValid: true, BitmapUnitStart: 10, BitmapUnitCount: 5},
				{IsValid: true, BitmapUnitStart: 20, BitmapUnitCount: 1},
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
			Extents: [128]ProtectedExtent{
				{IsValid: true, BitmapUnitStart: 0, BitmapUnitCount: 10},
				{IsValid: true, BitmapUnitStart: 5, BitmapUnitCount: 10}, // overlaps [0,10)
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
			Extents: [128]ProtectedExtent{
				{IsValid: true, BitmapUnitStart: 0, BitmapUnitCount: 10},
				{IsValid: true, BitmapUnitStart: 10, BitmapUnitCount: 10}, // adjacent
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
		got := trimZeroString(tt.input)
		if got != tt.expected {
			t.Errorf("trimZeroString(%v) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

// ============================================================================
// findDevice
// ============================================================================

func TestFindDevice(t *testing.T) {
	bm := newMeta(t)

	extents := []DiskExtent{
		makeDiskExtent("disk0", 0, 128*1024*1024*1024),
	}
	addDevice(t, bm, DeviceTypeDisk, "disk-001", extents)
	addDevice(t, bm, DeviceTypeVolume, "volume-001", extents)

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
	h := defaultHeader()

	if string(h.Signature[:len(SignatureStr)]) != SignatureStr {
		t.Error("default signature mismatch")
	}
	if h.Version != VersionV1_0 {
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

	h := defaultHeader()
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
// Max devices limit
// ============================================================================

func TestExceedMaxDevices(t *testing.T) {
	bm := newMeta(t)

	extents := []DiskExtent{
		makeDiskExtent("disk0", 0, 4*1024*1024), // 1 bit
	}

	// Add devices until we hit the limit
	maxDevices := int((bm.header.BitmapAllocMapOffset - bm.header.ProtectedRegionOffset) / ProtectedDeviceRecordSize)
	for i := 0; i < maxDevices; i++ {
		err := bm.AddProtectDevice(DeviceTypeDisk, makeID("disk-"+string(rune('A'+i%26))+"-"+string(rune('0'+i/26))), extents)
		if err != nil {
			t.Fatalf("AddProtectDevice %d failed: %v", i, err)
		}
	}

	// Next should fail
	// Use a unique ID
	err := bm.AddProtectDevice(DeviceTypeDisk, makeID("overflow"), extents)
	if err == nil {
		t.Error("expected error when exceeding max devices")
	}
}

// ============================================================================
// MD5 一致性：文件读取 vs PhysicalExtents 物理磁盘读取
// ============================================================================

func TestMD5Consistency(t *testing.T) {
	// 使用 os.TempDir() 而非 t.TempDir()，确保文件系统支持 FSCTL_GET_RETRIEVAL_POINTERS
	f, err := os.CreateTemp(os.TempDir(), "biotrkmeta_md5_*.bin")
	if err != nil {
		t.Fatalf("CreateTemp failed: %v", err)
	}
	path := f.Name()
	f.Close()
	os.Remove(path)

	bm, err := Create(path, 0)
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

	if err := bm.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// 从文件句柄读取元数据区域并计算 MD5
	md5File := md5HashFromReader(bm.file, bm.offset, bm.Size())
	t.Logf("File MD5: %s", md5File)

	// 从 PhysicalExtents 打开物理磁盘读取并计算 MD5
	phyExtents, err := bm.PhysicalExtents()
	if err != nil {
		t.Fatalf("PhysicalExtents failed: %v", err)
	}
	t.Logf("PhysicalExtents: %d extents", len(phyExtents))
	for i, e := range phyExtents {
		t.Logf("  [%d] disk=%s start=%d size=%d", i, e.Disk, e.Start, e.Size)
	}

	md5Disk := md5HashFromExtents(phyExtents)
	t.Logf("Disk MD5: %s", md5Disk)

	if md5File != md5Disk {
		t.Fatalf("MD5 mismatch!\n  file: %s\n  disk: %s", md5File, md5Disk)
	}
	t.Log("MD5 file == disk: OK")
}

// md5HashFromReader 从 io.ReaderAt 的指定偏移处读取 size 字节并计算 MD5 十六进制字符串。
func md5HashFromReader(r io.ReaderAt, offset int64, size int64) string {
	h := md5.New()
	buf := make([]byte, 64<<10)
	remain := size
	for remain > 0 {
		n := int64(len(buf))
		if n > remain {
			n = remain
		}
		if _, err := r.ReadAt(buf[:n], offset); err != nil {
			panic(fmt.Sprintf("read at %d: %v", offset, err))
		}
		h.Write(buf[:n])
		offset += n
		remain -= n
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// md5HashFromExtents 从物理磁盘 extents 读取数据并计算 MD5 十六进制字符串。
func md5HashFromExtents(extents []xutil.FileDiskExtentSegment) string {
	h := md5.New()
	buf := make([]byte, 64<<10)

	for _, e := range extents {
		f, err := os.OpenFile(e.Disk, os.O_RDONLY, 0)
		if err != nil {
			panic(fmt.Sprintf("open disk %s: %v", e.Disk, err))
		}

		offset := e.Start
		remain := e.Size
		for remain > 0 {
			n := int64(len(buf))
			if n > remain {
				n = remain
			}
			if _, err := f.ReadAt(buf[:n], offset); err != nil && err != io.EOF {
				f.Close()
				panic(fmt.Sprintf("read disk %s at %d: %v", e.Disk, offset, err))
			}
			h.Write(buf[:n])
			offset += n
			remain -= n
		}
		f.Close()
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
