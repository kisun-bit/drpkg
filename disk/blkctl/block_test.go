package blkctl

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/pkg/errors"
)

// mockReaderAt 模拟带坏扇区、EOF、慢速等行为的 ReaderAt。
type mockReaderAt struct {
	mu          sync.Mutex
	data        []byte
	crcOffsets  map[int64]bool          // 坏扇区偏移（扇区对齐）
	eofOffset   int64                   // -1 表示永不返回 EOF
	errOffsets  map[int64]error         // 返回特定错误的偏移
	slowOffsets map[int64]time.Duration // 慢速偏移（模拟超时）
}

func newMockReaderAt(data []byte) *mockReaderAt {
	return &mockReaderAt{
		data:        data,
		crcOffsets:  make(map[int64]bool),
		eofOffset:   -1,
		errOffsets:  make(map[int64]error),
		slowOffsets: make(map[int64]time.Duration),
	}
}

// ReadAt 实现 io.ReaderAt。
func (m *mockReaderAt) ReadAt(p []byte, off int64) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	end := off + int64(len(p))

	// 1. 检查范围内是否有 CRC 坏扇区
	for crcOff := range m.crcOffsets {
		if crcOff >= off && crcOff < end {
			return 0, crcError()
		}
	}

	// 2. 检查范围内是否有慢速扇区（模拟阻塞）
	for slowOff, dur := range m.slowOffsets {
		if slowOff >= off && slowOff < end {
			m.mu.Unlock()
			time.Sleep(dur)
			m.mu.Lock()
			break
		}
	}

	// 3. 检查范围内是否有自定义错误
	for errOff, e := range m.errOffsets {
		if errOff >= off && errOff < end {
			return 0, e
		}
	}

	// 4. 检查 EOF
	if m.eofOffset >= 0 {
		if off >= m.eofOffset {
			return 0, io.EOF
		}
		if end > m.eofOffset {
			n = int(m.eofOffset - off)
			copy(p, m.data[off:m.eofOffset])
			return n, io.EOF
		}
	}

	// 5. 正常读取
	if off >= int64(len(m.data)) {
		return 0, io.EOF
	}
	if end > int64(len(m.data)) {
		n = int(int64(len(m.data)) - off)
		copy(p, m.data[off:])
		return n, io.EOF
	}
	copy(p, m.data[off:end])
	return len(p), nil
}

// crcError 返回一个能被 IsDataCrcError 识别的 CRC 错误。
func crcError() error {
	return errCRC
}

// =============================================================================
// 测试用例
// =============================================================================

func TestReadFileSkipBadSector_NormalRead(t *testing.T) {
	sectorSize := int64(512)
	data := make([]byte, sectorSize*4)
	for i := range data {
		data[i] = byte(i % 256)
	}
	mock := newMockReaderAt(data)

	buf := make([]byte, sectorSize*4)
	n, hasBad, err := ReadFileSkipBadSector(mock, 0, buf, sectorSize, 0)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasBad {
		t.Fatal("expected no bad sectors")
	}
	if n != len(buf) {
		t.Fatalf("n=%d, want %d", n, len(buf))
	}
	if !bytes.Equal(buf, data) {
		t.Fatal("data mismatch")
	}
}

func TestReadFileSkipBadSector_OffsetNotAligned(t *testing.T) {
	sectorSize := int64(512)
	mock := newMockReaderAt(make([]byte, sectorSize*2))

	buf := make([]byte, sectorSize)
	_, _, err := ReadFileSkipBadSector(mock, 1, buf, sectorSize, 0)

	if err == nil {
		t.Fatal("expected alignment error")
	}
}

func TestReadFileSkipBadSector_SizeNotAligned(t *testing.T) {
	sectorSize := int64(512)
	mock := newMockReaderAt(make([]byte, sectorSize*2))

	buf := make([]byte, 100)
	_, _, err := ReadFileSkipBadSector(mock, 0, buf, sectorSize, 0)

	if err == nil {
		t.Fatal("expected alignment error")
	}
}

func TestReadFileSkipBadSector_SingleBadSector(t *testing.T) {
	sectorSize := int64(512)
	data := make([]byte, sectorSize*4)
	for i := range data {
		data[i] = 0xAA
	}
	mock := newMockReaderAt(data)
	mock.crcOffsets[512] = true

	buf := make([]byte, sectorSize*4)
	n, hasBad, err := ReadFileSkipBadSector(mock, 0, buf, sectorSize, 0)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasBad {
		t.Fatal("expected containsBadSector=true")
	}
	if n != len(buf) {
		t.Fatalf("n=%d, want %d", n, len(buf))
	}

	if !bytes.Equal(buf[0:512], data[0:512]) {
		t.Fatal("sector 0 should be intact")
	}
	if !bytes.Equal(buf[512:1024], make([]byte, 512)) {
		t.Fatal("sector 1 should be zeroed")
	}
	if !bytes.Equal(buf[1024:2048], data[1024:2048]) {
		t.Fatal("sectors 2-3 should be intact")
	}
}

func TestReadFileSkipBadSector_MultipleBadSectors(t *testing.T) {
	sectorSize := int64(512)
	data := make([]byte, sectorSize*6)
	for i := range data {
		data[i] = 0xBB
	}
	mock := newMockReaderAt(data)
	mock.crcOffsets[0] = true
	mock.crcOffsets[1024] = true
	mock.crcOffsets[2560] = true

	buf := make([]byte, sectorSize*6)
	n, hasBad, err := ReadFileSkipBadSector(mock, 0, buf, sectorSize, 0)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasBad {
		t.Fatal("expected containsBadSector=true")
	}
	if n != len(buf) {
		t.Fatalf("n=%d, want %d", n, len(buf))
	}

	for _, off := range []int{0, 1024, 2560} {
		if !bytes.Equal(buf[off:off+512], make([]byte, 512)) {
			t.Fatalf("sector at offset %d should be zeroed", off)
		}
	}
	for _, off := range []int{512, 1536, 2048} {
		if !bytes.Equal(buf[off:off+512], data[off:off+512]) {
			t.Fatalf("sector at offset %d should be intact", off)
		}
	}
}

func TestReadFileSkipBadSector_BadSectorAtStart(t *testing.T) {
	sectorSize := int64(512)
	data := make([]byte, sectorSize*3)
	for i := range data {
		data[i] = 0xCC
	}
	mock := newMockReaderAt(data)
	mock.crcOffsets[0] = true

	buf := make([]byte, sectorSize*3)
	n, hasBad, err := ReadFileSkipBadSector(mock, 0, buf, sectorSize, 0)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasBad {
		t.Fatal("expected containsBadSector=true")
	}
	if n != len(buf) {
		t.Fatalf("n=%d, want %d", n, len(buf))
	}

	if !bytes.Equal(buf[0:512], make([]byte, 512)) {
		t.Fatal("first sector should be zeroed")
	}
	if !bytes.Equal(buf[512:], data[512:]) {
		t.Fatal("remaining sectors should be intact")
	}
}

func TestReadFileSkipBadSector_BadSectorAtEnd(t *testing.T) {
	sectorSize := int64(512)
	data := make([]byte, sectorSize*3)
	for i := range data {
		data[i] = 0xDD
	}
	mock := newMockReaderAt(data)
	mock.crcOffsets[1024] = true

	buf := make([]byte, sectorSize*3)
	n, hasBad, err := ReadFileSkipBadSector(mock, 0, buf, sectorSize, 0)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasBad {
		t.Fatal("expected containsBadSector=true")
	}
	if n != len(buf) {
		t.Fatalf("n=%d, want %d", n, len(buf))
	}

	if !bytes.Equal(buf[0:1024], data[0:1024]) {
		t.Fatal("first two sectors should be intact")
	}
	if !bytes.Equal(buf[1024:1536], make([]byte, 512)) {
		t.Fatal("last sector should be zeroed")
	}
}

func TestReadFileSkipBadSector_EOFDuringRead(t *testing.T) {
	sectorSize := int64(512)
	data := make([]byte, sectorSize*2)
	for i := range data {
		data[i] = 0xEE
	}
	mock := newMockReaderAt(data)
	mock.eofOffset = 512

	buf := make([]byte, sectorSize*4)
	n, hasBad, err := ReadFileSkipBadSector(mock, 0, buf, sectorSize, 0)

	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
	if hasBad {
		t.Fatal("expected no bad sectors")
	}
	if n != 512 {
		t.Fatalf("n=%d, want 512", n)
	}

	if !bytes.Equal(buf[0:512], data[0:512]) {
		t.Fatal("first sector should be intact")
	}
}

func TestReadFileSkipBadSector_EOFWithBadSector(t *testing.T) {
	sectorSize := int64(512)
	data := make([]byte, sectorSize*3)
	for i := range data {
		data[i] = 0xFF
	}
	mock := newMockReaderAt(data)
	mock.crcOffsets[0] = true
	mock.eofOffset = 1024

	buf := make([]byte, sectorSize*4)
	n, hasBad, err := ReadFileSkipBadSector(mock, 0, buf, sectorSize, 0)

	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
	if !hasBad {
		t.Fatal("expected containsBadSector=true")
	}
	if n != 1024 {
		t.Fatalf("n=%d, want 1024", n)
	}

	if !bytes.Equal(buf[0:512], make([]byte, 512)) {
		t.Fatal("first sector should be zeroed")
	}
	if !bytes.Equal(buf[512:1024], data[512:1024]) {
		t.Fatal("second sector should be intact")
	}
}

func TestReadFileSkipBadSector_LessThanOneSector(t *testing.T) {
	sectorSize := int64(512)
	data := make([]byte, sectorSize)
	mock := newMockReaderAt(data)
	mock.crcOffsets[0] = true

	buf := make([]byte, 0)
	n, hasBad, err := ReadFileSkipBadSector(mock, 0, buf, sectorSize, 0)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasBad {
		t.Fatal("expected no bad sectors for empty buffer")
	}
	if n != 0 {
		t.Fatalf("n=%d, want 0", n)
	}
}

func TestReadFileSkipBadSector_OtherError(t *testing.T) {
	sectorSize := int64(512)
	data := make([]byte, sectorSize*4)
	mock := newMockReaderAt(data)
	mock.errOffsets[1024] = errors.New("disk ejected")

	buf := make([]byte, sectorSize*4)
	n, hasBad, err := ReadFileSkipBadSector(mock, 0, buf, sectorSize, 0)

	if err == nil {
		t.Fatal("expected error")
	}
	if hasBad {
		t.Fatal("expected no bad sectors (non-CRC error)")
	}
	if n != 0 {
		t.Fatalf("n=%d, want 0", n)
	}
}

func TestReadFileSkipBadSector_TimeoutOnSector(t *testing.T) {
	sectorSize := int64(512)
	data := make([]byte, sectorSize*4)
	for i := range data {
		data[i] = 0x77
	}
	mock := newMockReaderAt(data)
	mock.slowOffsets[512] = 500 * time.Millisecond

	buf := make([]byte, sectorSize*4)
	n, hasBad, err := ReadFileSkipBadSector(mock, 0, buf, sectorSize, 100*time.Millisecond)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasBad {
		t.Fatal("expected containsBadSector=true (timeout)")
	}
	if n != len(buf) {
		t.Fatalf("n=%d, want %d", n, len(buf))
	}

	if !bytes.Equal(buf[0:512], data[0:512]) {
		t.Fatal("sector 0 should be intact")
	}
	if !bytes.Equal(buf[512:1024], make([]byte, 512)) {
		t.Fatal("sector 1 should be zeroed (timeout)")
	}
	if !bytes.Equal(buf[1024:2048], data[1024:2048]) {
		t.Fatal("sectors 2-3 should be intact")
	}
}

func TestReadFileSkipBadSector_TimeoutFirstRead(t *testing.T) {
	sectorSize := int64(512)
	data := make([]byte, sectorSize)
	for i := range data {
		data[i] = 0x88
	}
	mock := newMockReaderAt(data)
	mock.slowOffsets[0] = 500 * time.Millisecond

	buf := make([]byte, sectorSize)
	n, hasBad, err := ReadFileSkipBadSector(mock, 0, buf, sectorSize, 100*time.Millisecond)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasBad {
		t.Fatal("expected containsBadSector=true (timeout on first read)")
	}
	if n != int(sectorSize) {
		t.Fatalf("n=%d, want %d", n, sectorSize)
	}
	if !bytes.Equal(buf, make([]byte, int(sectorSize))) {
		t.Fatal("buffer should be zeroed")
	}
}

func TestReadFileSkipBadSector_NonZeroOffset(t *testing.T) {
	sectorSize := int64(512)
	data := make([]byte, sectorSize*6)
	for i := range data {
		data[i] = byte(i % 256)
	}
	mock := newMockReaderAt(data)
	mock.crcOffsets[1536] = true

	buf := make([]byte, sectorSize*3)
	n, hasBad, err := ReadFileSkipBadSector(mock, 1024, buf, sectorSize, 0)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasBad {
		t.Fatal("expected containsBadSector=true")
	}
	if n != len(buf) {
		t.Fatalf("n=%d, want %d", n, len(buf))
	}

	if !bytes.Equal(buf[0:512], data[1024:1536]) {
		t.Fatal("sector at offset 1024 should be intact")
	}
	if !bytes.Equal(buf[512:1024], make([]byte, 512)) {
		t.Fatal("sector at offset 1536 should be zeroed")
	}
	if !bytes.Equal(buf[1024:1536], data[2048:2560]) {
		t.Fatal("sector at offset 2048 should be intact")
	}
}

func TestReadFileSkipBadSector_MixedBadAndTimeout(t *testing.T) {
	sectorSize := int64(512)
	data := make([]byte, sectorSize*5)
	for i := range data {
		data[i] = 0x99
	}
	mock := newMockReaderAt(data)
	mock.crcOffsets[0] = true
	mock.slowOffsets[1024] = 500 * time.Millisecond
	mock.crcOffsets[2048] = true

	buf := make([]byte, sectorSize*5)
	n, hasBad, err := ReadFileSkipBadSector(mock, 0, buf, sectorSize, 100*time.Millisecond)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasBad {
		t.Fatal("expected containsBadSector=true")
	}
	if n != len(buf) {
		t.Fatalf("n=%d, want %d", n, len(buf))
	}

	if !bytes.Equal(buf[0:512], make([]byte, 512)) {
		t.Fatal("sector 0 should be zeroed (CRC)")
	}
	if !bytes.Equal(buf[512:1024], data[512:1024]) {
		t.Fatal("sector 1 should be intact")
	}
	if !bytes.Equal(buf[1024:1536], make([]byte, 512)) {
		t.Fatal("sector 2 should be zeroed (timeout)")
	}
	if !bytes.Equal(buf[1536:2048], data[1536:2048]) {
		t.Fatal("sector 3 should be intact")
	}
	if !bytes.Equal(buf[2048:2560], make([]byte, 512)) {
		t.Fatal("sector 4 should be zeroed (CRC)")
	}
}

func TestIsTimeoutError(t *testing.T) {
	if isTimeoutError(nil) {
		t.Fatal("nil should not be timeout error")
	}
	if isTimeoutError(io.EOF) {
		t.Fatal("io.EOF should not be timeout error")
	}

	te := &timeoutError{offset: 1024, duration: time.Second}
	if !isTimeoutError(te) {
		t.Fatal("timeoutError should be detected")
	}
	if te.Error() == "" {
		t.Fatal("timeoutError should have non-empty message")
	}
}

func TestReadFileSkipBadSector_EmptyBuffer(t *testing.T) {
	sectorSize := int64(512)
	mock := newMockReaderAt(make([]byte, sectorSize))

	buf := make([]byte, 0)
	n, hasBad, err := ReadFileSkipBadSector(mock, 0, buf, sectorSize, 0)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasBad {
		t.Fatal("expected no bad sectors")
	}
	if n != 0 {
		t.Fatalf("n=%d, want 0", n)
	}
}

func TestReadFileSkipBadSector_NegativeTimeout(t *testing.T) {
	sectorSize := int64(512)
	data := make([]byte, sectorSize)
	for i := range data {
		data[i] = 0x66
	}
	mock := newMockReaderAt(data)

	buf := make([]byte, sectorSize)
	n, hasBad, err := ReadFileSkipBadSector(mock, 0, buf, sectorSize, -1)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasBad {
		t.Fatal("expected no bad sectors")
	}
	if n != int(sectorSize) {
		t.Fatalf("n=%d, want %d", n, sectorSize)
	}
	if !bytes.Equal(buf, data) {
		t.Fatal("data mismatch")
	}
}
