package xutil

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/pkg/errors"
)

// mockReaderAt 模拟带坏扇区、EOF、慢速等行为的 ReaderAt。
// 与真实磁盘行为一致：当读取范围 [off, off+len(p)) 内包含坏扇区时，
// 整体 ReadAt 返回 CRC 错误。
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
// 优先级：CRC > slow > customError > EOF > normal。
// 范围检查：若读取区间 [off, off+len(p)) 包含坏扇区，整体返回 CRC 错误。
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
			// 慢速后返回数据（若超时则由上层 readAtWithTimeout 处理）
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
// 在 Windows 上为 windows.ERROR_CRC，在 Linux 上返回一个标记错误
// （Linux 上 IsDataCrcError 始终返回 false，坏扇区通过超时机制处理）。
func crcError() error {
	return errCRC
}

// =============================================================================
// 测试用例
// =============================================================================

func TestReadFileSkipBadSector_NormalRead(t *testing.T) {
	sectorSize := int64(512)
	// 4 个扇区的数据
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

	buf := make([]byte, 100) // 不是 sectorSize 的整数倍
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
	// 第 1 个扇区（offset=512）是坏扇区
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

	// 扇区 0: 正常数据
	if !bytes.Equal(buf[0:512], data[0:512]) {
		t.Fatal("sector 0 should be intact")
	}
	// 扇区 1: 全零（坏块）
	if !bytes.Equal(buf[512:1024], make([]byte, 512)) {
		t.Fatal("sector 1 should be zeroed")
	}
	// 扇区 2, 3: 正常数据
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
	// 扇区 0, 2, 5 是坏扇区
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

	// 坏扇区应全零
	for _, off := range []int{0, 1024, 2560} {
		if !bytes.Equal(buf[off:off+512], make([]byte, 512)) {
			t.Fatalf("sector at offset %d should be zeroed", off)
		}
	}
	// 好扇区应有数据
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

	// 第一个扇区全零
	if !bytes.Equal(buf[0:512], make([]byte, 512)) {
		t.Fatal("first sector should be zeroed")
	}
	// 其余正常
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
	mock.crcOffsets[1024] = true // 最后一个扇区

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

	// 前两个扇区正常
	if !bytes.Equal(buf[0:1024], data[0:1024]) {
		t.Fatal("first two sectors should be intact")
	}
	// 最后一个扇区全零
	if !bytes.Equal(buf[1024:1536], make([]byte, 512)) {
		t.Fatal("last sector should be zeroed")
	}
}

func TestReadFileSkipBadSector_EOFDuringRead(t *testing.T) {
	sectorSize := int64(512)
	data := make([]byte, sectorSize*2) // 只有 2 个扇区的数据
	for i := range data {
		data[i] = 0xEE
	}
	mock := newMockReaderAt(data)
	mock.eofOffset = 512 // 第 1 个扇区开始返回 EOF

	buf := make([]byte, sectorSize*4) // 请求 4 个扇区
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

	// 第一个扇区有数据
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
	mock.crcOffsets[0] = true // 第一个扇区坏
	mock.eofOffset = 1024     // 第 2 个扇区开始返回 EOF

	buf := make([]byte, sectorSize*4) // 请求 4 个扇区
	n, hasBad, err := ReadFileSkipBadSector(mock, 0, buf, sectorSize, 0)

	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
	if !hasBad {
		t.Fatal("expected containsBadSector=true")
	}
	// n 应该是扇区0(坏) + 扇区1(读到的部分) = 1024
	if n != 1024 {
		t.Fatalf("n=%d, want 1024", n)
	}

	// 第一个扇区全零
	if !bytes.Equal(buf[0:512], make([]byte, 512)) {
		t.Fatal("first sector should be zeroed")
	}
	// 第二个扇区有数据
	if !bytes.Equal(buf[512:1024], data[512:1024]) {
		t.Fatal("second sector should be intact")
	}
}

func TestReadFileSkipBadSector_LessThanOneSector(t *testing.T) {
	// 测试空缓冲区（唯一的"小于一个扇区"且对齐的情况）
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
	// 第 2 个扇区返回不可恢复错误，整体读取失败
	mock.errOffsets[1024] = errors.New("disk ejected")

	buf := make([]byte, sectorSize*4)
	n, hasBad, err := ReadFileSkipBadSector(mock, 0, buf, sectorSize, 0)

	if err == nil {
		t.Fatal("expected error")
	}
	if hasBad {
		t.Fatal("expected no bad sectors (non-CRC error)")
	}
	// 整体读取失败，未读取任何字节
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
	// 第 1 个扇区模拟慢速（触发超时）
	mock.slowOffsets[512] = 500 * time.Millisecond

	buf := make([]byte, sectorSize*4)
	// 设置 100ms 超时，扇区 1 应该超时
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

	// 扇区 0: 正常
	if !bytes.Equal(buf[0:512], data[0:512]) {
		t.Fatal("sector 0 should be intact")
	}
	// 扇区 1: 超时清零
	if !bytes.Equal(buf[512:1024], make([]byte, 512)) {
		t.Fatal("sector 1 should be zeroed (timeout)")
	}
	// 扇区 2, 3: 正常
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
	// 整体读取（1 个扇区）也慢速
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
	// 整个扇区清零
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
	// 从扇区 2 开始读取 3 个扇区，扇区 3 (offset=1536) 是坏扇区
	// 注意：crcOffsets 使用的是绝对偏移
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

	// 扇区 2 (buf[0:512]) 正常
	if !bytes.Equal(buf[0:512], data[1024:1536]) {
		t.Fatal("sector at offset 1024 should be intact")
	}
	// 扇区 3 (buf[512:1024]) 坏扇区清零
	if !bytes.Equal(buf[512:1024], make([]byte, 512)) {
		t.Fatal("sector at offset 1536 should be zeroed")
	}
	// 扇区 4 (buf[1024:1536]) 正常
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
	// 扇区 0: CRC 坏块
	mock.crcOffsets[0] = true
	// 扇区 2: 超时
	mock.slowOffsets[1024] = 500 * time.Millisecond
	// 扇区 4: CRC 坏块
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

	// 扇区 0: 清零
	if !bytes.Equal(buf[0:512], make([]byte, 512)) {
		t.Fatal("sector 0 should be zeroed (CRC)")
	}
	// 扇区 1: 正常
	if !bytes.Equal(buf[512:1024], data[512:1024]) {
		t.Fatal("sector 1 should be intact")
	}
	// 扇区 2: 清零（超时）
	if !bytes.Equal(buf[1024:1536], make([]byte, 512)) {
		t.Fatal("sector 2 should be zeroed (timeout)")
	}
	// 扇区 3: 正常
	if !bytes.Equal(buf[1536:2048], data[1536:2048]) {
		t.Fatal("sector 3 should be intact")
	}
	// 扇区 4: 清零
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
	// timeout <= 0 使用 DefaultSectorReadTimeout
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
