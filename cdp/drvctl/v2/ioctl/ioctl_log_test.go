package ioctl

import (
	"testing"

	"github.com/kisun-bit/drpkg/xutil"
)

// ============================================================================
// LogStatus 常量
// ============================================================================

func TestLogStatusValues(t *testing.T) {
	if LogSuccess != 0 {
		t.Error("LogSuccess should be 0")
	}
	if LogError != 1 {
		t.Error("LogError should be 1")
	}
	if LogNoData != 2 {
		t.Error("LogNoData should be 2")
	}
}

// ============================================================================
// trimZeroString
// ============================================================================

func TestTrimZeroString(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want string
	}{
		{"empty", []byte{}, ""},
		{"no zeros", []byte("hello"), "hello"},
		{"trailing zeros", []byte("hello\x00\x00\x00"), "hello"},
		{"all zeros", []byte{0, 0, 0}, ""},
		{"middle zero kept", []byte("hel\x00lo\x00"), "hel\x00lo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := xutil.TrimZeroString(tt.in)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// ============================================================================
// 驱动相关测试（需要驱动存在）
// ============================================================================

func TestReadErrorStringNoDriver(t *testing.T) {
	if !driverAvailable() {
		t.Skip("driver not available")
	}

	str, err := ReadErrorString(0)
	if err != nil {
		t.Logf("ReadErrorString(0) error: %v", err)
		return
	}
	t.Logf("Error string for code 0: %q", str)
}

func TestReadLogNoDriver(t *testing.T) {
	if !driverAvailable() {
		t.Skip("driver not available")
	}

	entry, status, err := ReadLog()
	if err != nil {
		t.Logf("ReadLog error: %v", err)
		return
	}
	t.Logf("ReadLog status=%d entry=%+v", status, entry)
}

func TestLogEventLifecycle(t *testing.T) {
	if !driverAvailable() {
		t.Skip("driver not available")
	}

	event, err := NewLogEvent()
	if err != nil {
		t.Fatalf("NewLogEvent: %v", err)
	}
	defer event.Close()

	// 测试等待（立即超时）
	if err := event.Wait(0); err != nil {
		t.Errorf("Wait(0): %v", err)
	}

	// 测试句柄
	h := event.Handle()
	if h == 0 {
		t.Error("Handle should be non-zero")
	}
}

// ============================================================================
// normalizeLogTimestamp 行为验证
// ============================================================================

func TestNormalizeLogTimestampDoesNotPanic(t *testing.T) {
	entry := &LogEntry{Timestamp: 1234567890}
	normalizeLogTimestamp(entry)
	// 不 panic 即为通过
}
