package ioctl

import (
	"encoding/binary"
	"errors"
	"os"
	"testing"

	biotrkmeta "github.com/kisun-bit/drpkg/cdp/drvctl/v2/meta"
)

// ============================================================================
// 辅助函数
// ============================================================================

func makeDiskID(s string) biotrkmeta.DiskID {
	var d biotrkmeta.DiskID
	copy(d.ID[:], s)
	return d
}

func makeDiskExtent(diskID string, start, size uint64) biotrkmeta.DiskExtent {
	var dkID biotrkmeta.DiskID
	copy(dkID.ID[:], diskID)
	return biotrkmeta.DiskExtent{DiskID: dkID, Start: start, Size: size}
}

// diskIDStr 返回 DiskID 的字符串表示（避免直接调用指针方法 String 的非可寻址问题）。
func diskIDStr(d biotrkmeta.DiskID) string {
	return (&d).String()
}

// ============================================================================
// CTL_CODE 计算 (Windows)
// ============================================================================

func TestCtlCode(t *testing.T) {
	// CTL_CODE(FILE_DEVICE_UNKNOWN, 1, METHOD_IN_DIRECT, FILE_ANY_ACCESS)
	// = (0x22 << 16) | (0 << 14) | (1 << 2) | 1
	// = 0x00220000 | 0 | 0x00000004 | 0x00000001
	// = 0x00220005
	got := ctlCode(1)
	want := uint32((0x22 << 16) | (0 << 14) | (1 << 2) | 1)

	if got != want {
		t.Errorf("ctlCode(1) = 0x%08X, want 0x%08X", got, want)
	}
}

func TestGetIoctlCodeWindows(t *testing.T) {
	// getIoctlCode 在 Windows 上调用 ctlCode
	for _, code := range []uint{1, 2, 3, 15} {
		got := getIoctlCode(code)
		want := ctlCode(uint32(code))
		if got != want {
			t.Errorf("getIoctlCode(%d) = 0x%08X, want 0x%08X", code, got, want)
		}
	}
}

// ============================================================================
// IOC 宏计算 (Linux) - 交叉平台测试宏函数
// ============================================================================

// TestIocMacros 仅在 Linux 平台编译，见 ioctl_linux_test.go。

// ============================================================================
// pack / unpack 往返测试
// ============================================================================

func TestStartTaskRequestPackUnpack(t *testing.T) {
	orig := &StartTaskRequest{
		MetadataOffset: 4096,
		MetadataExtents: []biotrkmeta.DiskExtent{
			makeDiskExtent("disk0", 0, 4096),
			makeDiskExtent("disk1", 1048576, 8192),
		},
	}
	copy(orig.MetadataFile[:], `\\.\PHYSICALDRIVE0`)
	orig.MetadataExtentsLen = uint32(len(orig.MetadataExtents))

	packed, err := pack(orig)
	if err != nil {
		t.Fatalf("pack StartTaskRequest: %v", err)
	}

	var decoded StartTaskRequest
	if err := unpack(packed, &decoded); err != nil {
		t.Fatalf("unpack StartTaskRequest: %v", err)
	}

	if decoded.MetadataOffset != orig.MetadataOffset {
		t.Errorf("MetadataOffset: got %d, want %d", decoded.MetadataOffset, orig.MetadataOffset)
	}
	if string(decoded.MetadataFile[:len(`\\.\PHYSICALDRIVE0`)]) != `\\.\PHYSICALDRIVE0` {
		t.Error("MetadataFile mismatch")
	}
	if len(decoded.MetadataExtents) != len(orig.MetadataExtents) {
		t.Fatalf("MetadataExtents count: got %d, want %d", len(decoded.MetadataExtents), len(orig.MetadataExtents))
	}
	for i := range orig.MetadataExtents {
		if decoded.MetadataExtents[i].Start != orig.MetadataExtents[i].Start {
			t.Errorf("MetadataExtents[%d].Start mismatch", i)
		}
		if decoded.MetadataExtents[i].Size != orig.MetadataExtents[i].Size {
			t.Errorf("MetadataExtents[%d].Size mismatch", i)
		}
	}
}

func TestTaskStatusPackUnpack(t *testing.T) {
	orig := &TaskStatus{
		Status:    biotrkmeta.CDPStatusCDP,
		ErrorCode: 42,
	}

	packed, err := pack(orig)
	if err != nil {
		t.Fatalf("pack TaskStatus: %v", err)
	}

	var decoded TaskStatus
	if err := unpack(packed, &decoded); err != nil {
		t.Fatalf("unpack TaskStatus: %v", err)
	}

	if decoded.Status != orig.Status {
		t.Errorf("Status: got %d, want %d", decoded.Status, orig.Status)
	}
	if decoded.ErrorCode != orig.ErrorCode {
		t.Errorf("ErrorCode: got %d, want %d", decoded.ErrorCode, orig.ErrorCode)
	}
}

func TestTaskStatusSizeof(t *testing.T) {
	// TaskStatus: Status(4) + ErrorCode(8) = 12
	sz := sizeof(&TaskStatus{})
	if sz != 12 {
		t.Errorf("sizeof(TaskStatus): got %d, want 12", sz)
	}
}

func TestTaskErrorStringPackUnpack(t *testing.T) {
	orig := &TaskErrorString{ErrCode: 0xDEADBEEF}

	packed, err := pack(orig)
	if err != nil {
		t.Fatalf("pack TaskErrorString: %v", err)
	}

	var decoded TaskErrorString
	if err := unpack(packed, &decoded); err != nil {
		t.Fatalf("unpack TaskErrorString: %v", err)
	}

	if decoded.ErrCode != orig.ErrCode {
		t.Errorf("ErrCode: got %d, want %d", decoded.ErrCode, orig.ErrCode)
	}
}

func TestTaskErrorStringSizeof(t *testing.T) {
	// TaskErrorString: ErrCode(8) + ErrStr([512]byte) = 520
	sz := sizeof(&TaskErrorString{})
	if sz != 520 {
		t.Errorf("sizeof(TaskErrorString): got %d, want 520", sz)
	}
}

func TestShmConfigPackUnpack(t *testing.T) {
	orig := &ShmConfig{
		Size:    4096,
		Event:   0x12345678,
		Address: 0xABCDEF00,
	}

	packed, err := pack(orig)
	if err != nil {
		t.Fatalf("pack ShmConfig: %v", err)
	}

	var decoded ShmConfig
	if err := unpack(packed, &decoded); err != nil {
		t.Fatalf("unpack ShmConfig: %v", err)
	}

	if decoded.Size != orig.Size {
		t.Errorf("Size: got %d, want %d", decoded.Size, orig.Size)
	}
	if decoded.Event != orig.Event {
		t.Errorf("Event: got %d, want %d", decoded.Event, orig.Event)
	}
	if decoded.Address != orig.Address {
		t.Errorf("Address: got %d, want %d", decoded.Address, orig.Address)
	}
}

func TestShmConfigSizeof(t *testing.T) {
	// ShmConfig: Size(4) + Event(8) + Address(8) = 20
	sz := sizeof(&ShmConfig{})
	if sz != 20 {
		t.Errorf("sizeof(ShmConfig): got %d, want 20", sz)
	}
}

func TestLogEventSetRequestPackUnpack(t *testing.T) {
	orig := &LogEventSetRequest{Event: 0xDEADBEEFCAFE}

	packed, err := pack(orig)
	if err != nil {
		t.Fatalf("pack LogEventSetRequest: %v", err)
	}

	var decoded LogEventSetRequest
	if err := unpack(packed, &decoded); err != nil {
		t.Fatalf("unpack LogEventSetRequest: %v", err)
	}

	if decoded.Event != orig.Event {
		t.Errorf("Event: got %d, want %d", decoded.Event, orig.Event)
	}
}

func TestLogEntryPackUnpack(t *testing.T) {
	msg := "test log message"
	orig := &LogEntry{
		Level:     3,
		Timestamp: 1234567890,
		DataLen:   uint32(len(msg)),
	}
	copy(orig.Data[:], msg)

	packed, err := pack(orig)
	if err != nil {
		t.Fatalf("pack LogEntry: %v", err)
	}

	var decoded LogEntry
	if err := unpack(packed, &decoded); err != nil {
		t.Fatalf("unpack LogEntry: %v", err)
	}

	if decoded.Level != orig.Level {
		t.Errorf("Level: got %d, want %d", decoded.Level, orig.Level)
	}
	if decoded.Timestamp != orig.Timestamp {
		t.Errorf("Timestamp: got %d, want %d", decoded.Timestamp, orig.Timestamp)
	}
	if decoded.DataLen != orig.DataLen {
		t.Errorf("DataLen: got %d, want %d", decoded.DataLen, orig.DataLen)
	}
	if string(decoded.Data[:len(msg)]) != msg {
		t.Errorf("Data: got %q, want %q", string(decoded.Data[:len(msg)]), msg)
	}
}

func TestLogEntrySizeof(t *testing.T) {
	// LogEntry: Level(1) + Timestamp(8) + DataLen(4) + Data([512]byte) = 525
	sz := sizeof(&LogEntry{})
	if sz != 525 {
		t.Errorf("sizeof(LogEntry): got %d, want 525", sz)
	}
}

func TestBitmapDetailPackUnpack(t *testing.T) {
	orig := &BitmapDetail{
		DiskID:     makeDiskID("test-disk-001"),
		BitmapSize: 65536,
	}

	packed, err := pack(orig)
	if err != nil {
		t.Fatalf("pack BitmapDetail: %v", err)
	}

	var decoded BitmapDetail
	if err := unpack(packed, &decoded); err != nil {
		t.Fatalf("unpack BitmapDetail: %v", err)
	}

	if diskIDStr(decoded.DiskID) != diskIDStr(orig.DiskID) {
		t.Errorf("DiskID: got %q, want %q", diskIDStr(decoded.DiskID), diskIDStr(orig.DiskID))
	}
	if decoded.BitmapSize != orig.BitmapSize {
		t.Errorf("BitmapSize: got %d, want %d", decoded.BitmapSize, orig.BitmapSize)
	}
}

func TestBitmapDetailSizeof(t *testing.T) {
	// BitmapDetail: DiskID(520) + BitmapSize(4) = 524
	sz := sizeof(&BitmapDetail{})
	if sz != 524 {
		t.Errorf("sizeof(BitmapDetail): got %d, want 524", sz)
	}
}

func TestBitmapDataPackUnpack(t *testing.T) {
	data := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}
	orig := &BitmapData{
		DiskID:   makeDiskID("disk-bitmap"),
		Offset:   1024,
		Length:   6,
		DataSize: uint32(len(data)),
		Data:     data,
	}

	packed, err := pack(orig)
	if err != nil {
		t.Fatalf("pack BitmapData: %v", err)
	}

	var decoded BitmapData
	if err := unpack(packed, &decoded); err != nil {
		t.Fatalf("unpack BitmapData: %v", err)
	}

	if diskIDStr(decoded.DiskID) != diskIDStr(orig.DiskID) {
		t.Errorf("DiskID mismatch: %q vs %q", diskIDStr(decoded.DiskID), diskIDStr(orig.DiskID))
	}
	if decoded.Offset != orig.Offset {
		t.Errorf("Offset: got %d, want %d", decoded.Offset, orig.Offset)
	}
	if decoded.Length != orig.Length {
		t.Errorf("Length: got %d, want %d", decoded.Length, orig.Length)
	}
	if decoded.DataSize != orig.DataSize {
		t.Errorf("DataSize: got %d, want %d", decoded.DataSize, orig.DataSize)
	}
	if len(decoded.Data) != len(orig.Data) {
		t.Fatalf("Data len: got %d, want %d", len(decoded.Data), len(orig.Data))
	}
	for i := range data {
		if decoded.Data[i] != data[i] {
			t.Errorf("Data[%d]: got 0x%02X, want 0x%02X", i, decoded.Data[i], data[i])
		}
	}
}

func TestBitmapDataEmptyPackUnpack(t *testing.T) {
	// 请求场景：Data 为空，仅填充 DiskID/Offset/Length
	orig := &BitmapData{
		DiskID: makeDiskID("empty-request"),
		Offset: 0,
		Length: 4096,
	}

	packed, err := pack(orig)
	if err != nil {
		t.Fatalf("pack empty BitmapData: %v", err)
	}

	// 请求大小 = 532 (DiskID 520 + Offset 4 + Length 4 + DataSize 4, no Data)
	fixedSize := sizeof(&BitmapData{})
	if fixedSize != 532 {
		t.Errorf("sizeof empty BitmapData: got %d, want 532", fixedSize)
	}
	if len(packed) != 532 {
		t.Errorf("packed empty BitmapData: got %d, want 532", len(packed))
	}

	var decoded BitmapData
	if err := unpack(packed, &decoded); err != nil {
		t.Fatalf("unpack empty BitmapData: %v", err)
	}
	if decoded.Length != orig.Length {
		t.Errorf("Length: got %d, want %d", decoded.Length, orig.Length)
	}
	if decoded.DataSize != 0 {
		t.Errorf("DataSize: got %d, want 0", decoded.DataSize)
	}
}

func TestClearBitmapReferencePackUnpack(t *testing.T) {
	orig := &ClearBitmapReferenceRequest{
		RecordType: biotrkmeta.RecordFromDisk,
		DiskID:     makeDiskID("clear-disk"),
		Segments: []biotrkmeta.Segment{
			{Start: 0, Size: 4096},
			{Start: 1048576, Size: 8192},
		},
	}
	orig.SegmentsLen = uint32(len(orig.Segments))

	packed, err := pack(orig)
	if err != nil {
		t.Fatalf("pack ClearBitmapReferenceRequest: %v", err)
	}

	// 手动验证：删除原切片后重新设置
	var decoded ClearBitmapReferenceRequest
	orig.Segments = nil
	if err := unpack(packed, &decoded); err != nil {
		t.Fatalf("unpack ClearBitmapReferenceRequest: %v", err)
	}

	if decoded.RecordType != biotrkmeta.RecordFromDisk {
		t.Errorf("RecordType: got %d, want %d", decoded.RecordType, biotrkmeta.RecordFromDisk)
	}
	if diskIDStr(decoded.DiskID) != diskIDStr(makeDiskID("clear-disk")) {
		t.Error("DiskID mismatch")
	}
	if len(decoded.Segments) != 2 {
		t.Fatalf("Segments count: got %d, want 2", len(decoded.Segments))
	}
	if decoded.Segments[0].Start != 0 || decoded.Segments[0].Size != 4096 {
		t.Error("Segments[0] mismatch")
	}
	if decoded.Segments[1].Start != 1048576 || decoded.Segments[1].Size != 8192 {
		t.Error("Segments[1] mismatch")
	}
}

func TestAddProtectedDevicesRequestPackUnpack(t *testing.T) {
	pd := biotrkmeta.ProtectedDevice{
		Type:    biotrkmeta.DeviceTypeDisk,
		Extents: make([]biotrkmeta.DiskExtent, 1),
	}
	copy(pd.DeviceID[:], "test-device")
	pd.Extents[0] = makeDiskExtent("phys0", 0, 4096)
	pd.ExtentCount = uint32(len(pd.Extents))

	orig := &AddProtectedDevicesRequest{
		Devices: []biotrkmeta.ProtectedDevice{pd},
		NewMetadataExtents: []biotrkmeta.DiskExtent{
			makeDiskExtent("meta0", 65536, 4096),
		},
	}
	orig.DevicesLen = uint32(len(orig.Devices))
	orig.NewMetadataExtentsLen = uint32(len(orig.NewMetadataExtents))

	packed, err := pack(orig)
	if err != nil {
		t.Fatalf("pack AddProtectedDevicesRequest: %v", err)
	}

	var decoded AddProtectedDevicesRequest
	if err := unpack(packed, &decoded); err != nil {
		t.Fatalf("unpack AddProtectedDevicesRequest: %v", err)
	}

	if len(decoded.Devices) != 1 {
		t.Fatalf("Devices count: got %d, want 1", len(decoded.Devices))
	}
	if decoded.Devices[0].Type != biotrkmeta.DeviceTypeDisk {
		t.Error("Device type mismatch")
	}
	if len(decoded.NewMetadataExtents) != 1 {
		t.Fatalf("NewMetadataExtents count: got %d, want 1", len(decoded.NewMetadataExtents))
	}
	if decoded.NewMetadataExtents[0].Start != 65536 {
		t.Error("NewMetadataExtents[0].Start mismatch")
	}
}

func TestRemoveProtectedDevicesRequestPackUnpack(t *testing.T) {
	orig := &RemoveProtectedDevicesRequest{
		DiskIDs: []biotrkmeta.ID{
			makeDiskID("disk-001").ID,
		},
		DeviceIDs: []biotrkmeta.ID{
			makeDiskID("device-001").ID,
			makeDiskID("device-002").ID,
		},
		NewMetadataExtents: []biotrkmeta.DiskExtent{
			makeDiskExtent("meta0", 65536, 8192),
		},
	}
	orig.DiskIDsLen = uint32(len(orig.DiskIDs))
	orig.DeviceIDsLen = uint32(len(orig.DeviceIDs))
	orig.NewMetadataExtentsLen = uint32(len(orig.NewMetadataExtents))

	packed, err := pack(orig)
	if err != nil {
		t.Fatalf("pack RemoveProtectedDevicesRequest: %v", err)
	}

	var decoded RemoveProtectedDevicesRequest
	if err := unpack(packed, &decoded); err != nil {
		t.Fatalf("unpack RemoveProtectedDevicesRequest: %v", err)
	}

	if len(decoded.DiskIDs) != 1 {
		t.Fatalf("DiskIDs count: got %d, want 1", len(decoded.DiskIDs))
	}
	if string(decoded.DiskIDs[0][:len("disk-001")]) != "disk-001" {
		t.Error("DiskIDs[0] mismatch")
	}

	if len(decoded.DeviceIDs) != 2 {
		t.Fatalf("DeviceIDs count: got %d, want 2", len(decoded.DeviceIDs))
	}
	if string(decoded.DeviceIDs[0][:len("device-001")]) != "device-001" {
		t.Error("DeviceIDs[0] mismatch")
	}
	if string(decoded.DeviceIDs[1][:len("device-002")]) != "device-002" {
		t.Error("DeviceIDs[1] mismatch")
	}
	if len(decoded.NewMetadataExtents) != 1 {
		t.Fatalf("NewMetadataExtents count: got %d, want 1", len(decoded.NewMetadataExtents))
	}
}

func TestListProtectedDevicesPackUnpack(t *testing.T) {
	pd := biotrkmeta.ProtectedDevice{
		Type:    biotrkmeta.DeviceTypeVolume,
		Extents: make([]biotrkmeta.DiskExtent, 1),
	}
	copy(pd.DeviceID[:], "volume-001")
	pd.Extents[0] = makeDiskExtent("diskA", 0, 4096)
	pd.ExtentCount = uint32(len(pd.Extents))

	orig := &ListProtectedDevices{
		Devices:    []biotrkmeta.ProtectedDevice{pd},
		DevicesLen: 1,
	}

	packed, err := pack(orig)
	if err != nil {
		t.Fatalf("pack ListProtectedDevices: %v", err)
	}

	var decoded ListProtectedDevices
	if err := unpack(packed, &decoded); err != nil {
		t.Fatalf("unpack ListProtectedDevices: %v", err)
	}

	if decoded.DevicesLen != 1 {
		t.Errorf("DevicesLen: got %d, want 1", decoded.DevicesLen)
	}
	if len(decoded.Devices) != 1 {
		t.Fatalf("Devices count: got %d, want 1", len(decoded.Devices))
	}
	if decoded.Devices[0].Type != biotrkmeta.DeviceTypeVolume {
		t.Error("Device type mismatch")
	}
}

func TestListProtectedDevicesEmptyPackUnpack(t *testing.T) {
	orig := &ListProtectedDevices{}

	packed, err := pack(orig)
	if err != nil {
		t.Fatalf("pack empty ListProtectedDevices: %v", err)
	}

	var decoded ListProtectedDevices
	if err := unpack(packed, &decoded); err != nil {
		t.Fatalf("unpack empty ListProtectedDevices: %v", err)
	}

	if decoded.DevicesLen != 0 {
		t.Errorf("DevicesLen: got %d, want 0", decoded.DevicesLen)
	}
	if len(decoded.Devices) != 0 {
		t.Errorf("Devices count: got %d, want 0", len(decoded.Devices))
	}
}

// ============================================================================
// 控制码常量一致性
// ============================================================================

func TestIoctlCodesAreSequential(t *testing.T) {
	// 验证 IOCTL 控制码从 1 开始连续递增
	codes := []uint{
		IOCTL_BIOTRK_START_TASK,
		IOCTL_BIOTRK_RELEASE_TASK,
		IOCTL_BIOTRK_GET_TASK_STATUS,
		IOCTL_BIOTRK_SET_TASK_CONSISTENCY,
		IOCTL_BIOTRK_GET_TASK_ERROR_STRING,
		IOCTL_BIOTRK_LIST_PROTECTED_DEVICE,
		IOCTL_BIOTRK_ADD_PROTECTED_DEVICE,
		IOCTL_BIOTRK_REMOVE_PROTECTED_DEVICE,
		IOCTL_BIOTRK_GET_BITMAP_DETAIL,
		IOCTL_BIOTRK_GET_BITMAP_DATA,
		IOCTL_BIOTRK_CLEAR_BITMAP_REFERENCE,
		IOCTL_BIOTRK_CREATE_SHM,
		IOCTL_BIOTRK_DELETE_SHM,
		IOCTL_BIOTRK_SET_LOG_EVENT,
		IOCTL_BIOTRK_GET_LOG,
	}

	for i, code := range codes {
		want := uint(i + 1)
		if code != want {
			t.Errorf("IOCTL code at index %d: got %d, want %d", i, code, want)
		}
	}
}

// ============================================================================
// 错误类型测试
// ============================================================================

func TestErrorSentinelValues(t *testing.T) {
	// 验证哨兵错误变量非空且可被 errors.Is 检测
	if ErrorOverflow == nil {
		t.Error("ErrorOverflow should not be nil")
	}
	if ErrorInsufficientMemSpace == nil {
		t.Error("ErrorInsufficientMemSpace should not be nil")
	}

	if !errors.Is(ErrorOverflow, ErrorOverflow) {
		t.Error("ErrorOverflow should be identifiable by errors.Is")
	}
	if !errors.Is(ErrorInsufficientMemSpace, ErrorInsufficientMemSpace) {
		t.Error("ErrorInsufficientMemSpace should be identifiable by errors.Is")
	}
}

// ============================================================================
// 驱动通信测试 (需要驱动存在)
// ============================================================================

// driverAvailable 检查驱动设备是否可访问。
func driverAvailable() bool {
	_, err := os.Stat(deviceName)
	return err == nil
}

func TestStartTaskWithoutDriver(t *testing.T) {
	if !driverAvailable() {
		t.Skip("driver not available")
	}

	req := &StartTaskRequest{}
	err := StartTask(req)
	if err != nil {
		t.Logf("StartTask returned error (expected without valid config): %v", err)
	}
}

func TestReleaseTaskWithoutDriver(t *testing.T) {
	if !driverAvailable() {
		t.Skip("driver not available")
	}

	err := ReleaseTask()
	t.Logf("ReleaseTask returned: %v", err)
}

func TestGetTaskStatusWithoutDriver(t *testing.T) {
	if !driverAvailable() {
		t.Skip("driver not available")
	}

	status, err := GetTaskStatus()
	if err != nil {
		t.Logf("GetTaskStatus error: %v", err)
		return
	}
	t.Logf("Status: %v, ErrorCode: %d", status.Status, status.ErrorCode)
}

func TestGetProtectedDevicesWithoutDriver(t *testing.T) {
	if !driverAvailable() {
		t.Skip("driver not available")
	}

	devs, err := GetProtectedDevices()
	if err != nil {
		t.Logf("GetProtectedDevices error: %v", err)
		return
	}
	t.Logf("Devices count: %d", devs.DevicesLen)
}

func TestGetLogWithoutDriver(t *testing.T) {
	if !driverAvailable() {
		t.Skip("driver not available")
	}

	entry, err := GetLog()
	if err != nil {
		t.Logf("GetLog error: %v", err)
		return
	}
	t.Logf("LogLevel: %d, Timestamp: %d, Data: %s",
		entry.Level, entry.Timestamp, string(entry.Data[:entry.DataLen]))
}

// ============================================================================
// 结构体大小确定性测试
// ============================================================================

func TestStructSizeConsistency(t *testing.T) {
	// 验证关键结构体的 sizeof 与手工计算一致
	tests := []struct {
		name string
		v    interface{}
		want int
	}{
		{"TaskStatus", &TaskStatus{}, 12},
		{"TaskErrorString", &TaskErrorString{}, 520},
		{"ShmConfig", &ShmConfig{}, 20},
		{"LogEntry", &LogEntry{}, 525},
		{"LogEventSetRequest", &LogEventSetRequest{}, 8},
		{"BitmapDetail", &BitmapDetail{}, 524},
		{"BitmapData(empty)", &BitmapData{}, 532},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sizeof(tt.v)
			if got != tt.want {
				t.Errorf("sizeof: got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestPackSizeMatchesSizeof(t *testing.T) {
	// 验证 pack 后的数据长度与 sizeof 一致（对固定大小类型）
	tests := []interface{}{
		&TaskStatus{Status: biotrkmeta.CDPStatusCDP},
		&ShmConfig{Size: 4096},
		&LogEntry{Level: 1},
		&LogEventSetRequest{Event: 42},
		&BitmapDetail{DiskID: makeDiskID("x")},
	}

	for _, v := range tests {
		packed, err := pack(v)
		if err != nil {
			t.Fatalf("pack failed: %v", err)
		}
		sz := sizeof(v)
		if len(packed) != sz {
			t.Errorf("packed size %d != sizeof %d for %T", len(packed), sz, v)
		}
	}
}

// ============================================================================
// CDPStatus 常量验证
// ============================================================================

func TestCDPStatusValues(t *testing.T) {
	if biotrkmeta.CDPStatusIdle != 0 {
		t.Error("CDPStatusIdle should be 0")
	}
	if biotrkmeta.CDPStatusCDP != 1 {
		t.Error("CDPStatusCDP should be 1")
	}
	if biotrkmeta.CDPStatusCBT != 2 {
		t.Error("CDPStatusCBT should be 2")
	}
	if biotrkmeta.CDPStatusError != 3 {
		t.Error("CDPStatusError should be 3")
	}
}

// ============================================================================
// DiskID 往返测试 (struc 兼容性)
// ============================================================================

func TestDiskIDPackUnpackViaStruc(t *testing.T) {
	orig := biotrkmeta.DiskID{
		Major: 259,
		Minor: 7,
	}
	copy(orig.ID[:], "pci-0000:81:00.0-nvme-1")

	// 通过 struc 序列化/反序列化
	packed, err := pack(&orig)
	if err != nil {
		t.Fatalf("pack DiskID: %v", err)
	}

	if len(packed) != biotrkmeta.DiskIDBinSize {
		t.Errorf("DiskID packed size: got %d, want %d", len(packed), biotrkmeta.DiskIDBinSize)
	}

	var decoded biotrkmeta.DiskID
	if err := unpack(packed, &decoded); err != nil {
		t.Fatalf("unpack DiskID: %v", err)
	}

	if decoded.Major != orig.Major {
		t.Error("Major mismatch")
	}
	if decoded.Minor != orig.Minor {
		t.Error("Minor mismatch")
	}
	if decoded.String() != orig.String() {
		t.Errorf("ID mismatch: %q vs %q", decoded.String(), orig.String())
	}
}

func TestDiskExtentPackUnpackViaStruc(t *testing.T) {
	orig := biotrkmeta.DiskExtent{
		Start: 65536,
		Size:  4096,
	}
	copy(orig.DiskID.ID[:], "test-extent-disk")

	packed, err := pack(&orig)
	if err != nil {
		t.Fatalf("pack DiskExtent: %v", err)
	}

	if len(packed) != biotrkmeta.DiskExtentBinSize {
		t.Errorf("DiskExtent packed size: got %d, want %d", len(packed), biotrkmeta.DiskExtentBinSize)
	}

	var decoded biotrkmeta.DiskExtent
	if err := unpack(packed, &decoded); err != nil {
		t.Fatalf("unpack DiskExtent: %v", err)
	}

	if decoded.Start != orig.Start || decoded.Size != orig.Size {
		t.Error("Start/Size mismatch")
	}
	if diskIDStr(decoded.DiskID) != diskIDStr(orig.DiskID) {
		t.Error("DiskID mismatch")
	}
}

// ============================================================================
// 字节序验证
// ============================================================================

func TestLittleEndianEncoding(t *testing.T) {
	// 验证 pack 使用 LittleEndian
	req := &TaskStatus{Status: biotrkmeta.CDPStatusCDP, ErrorCode: 0x0102030405060708}
	packed, err := pack(req)
	if err != nil {
		t.Fatalf("pack failed: %v", err)
	}

	// Status (offset 0, 4 bytes) = CDPStatusCDP = 1
	statusVal := binary.LittleEndian.Uint32(packed[0:4])
	if statusVal != 1 {
		t.Errorf("Status LE: got %d, want 1", statusVal)
	}

	// ErrorCode (offset 4, 8 bytes) = 0x0102030405060708
	errVal := binary.LittleEndian.Uint64(packed[4:12])
	if errVal != 0x0102030405060708 {
		t.Errorf("ErrorCode LE: got 0x%016X, want 0x0102030405060708", errVal)
	}
}

// ============================================================================
// nil/空输入边界测试
// ============================================================================

func TestPackNilInterface(t *testing.T) {
	_, err := pack(nil)
	if err == nil {
		t.Error("pack(nil) should return error")
	}
}

func TestUnpackShortBuffer(t *testing.T) {
	err := unpack(make([]byte, 4), &TaskStatus{})
	if err == nil {
		t.Error("unpack with short buffer should return error")
	}
}

func TestPackZeroValueStructs(t *testing.T) {
	// 所有零值结构体都能正常打包
	for _, v := range []interface{}{
		&StartTaskRequest{},
		&TaskStatus{},
		&TaskErrorString{},
		&ShmConfig{},
		&LogEventSetRequest{},
		&LogEntry{},
		&BitmapDetail{},
		&BitmapData{},
		&ClearBitmapReferenceRequest{},
		&AddProtectedDevicesRequest{},
		&RemoveProtectedDevicesRequest{},
		&ListProtectedDevices{},
	} {
		_, err := pack(v)
		if err != nil {
			t.Errorf("pack zero %T: %v", v, err)
		}
	}
}
