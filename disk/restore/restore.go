// Package restore 提供带去重能力的磁盘恢复目标。
//
// 典型用法：恢复程序从备份中读出数据块（Block），逐块调用 Target.Restore 写入
// 目标磁盘；Target 内部维护"已恢复位图"（默认一个 bit 代表 512 字节），
// 自动跳过已经恢复过的区域，因此块与块之间重叠、甚至整块重复都不需要调用方处理。
//
//	target, err := restore.OpenPath("/dev/sdb", nil)
//	if err != nil { return err }
//	defer target.Close()
//
//	n, err := target.Restore(ctx, &restore.Block{Offset: off, Length: uint64(len(buf)), Data: buf})
//	// n 为本次真实写入的字节数（已恢复过的部分被跳过，不计入 n）
package restore

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/pkg/errors"
)

// DefaultGranularity 是位图的默认粒度：一个 bit 代表 512 字节磁盘空间。
const DefaultGranularity uint64 = 512

// Block 表示恢复程序产出的一个数据块。
type Block struct {
	// Offset 是该块在目标磁盘上的起始字节偏移，必须为粒度对齐。
	Offset uint64

	// Length 是该块声明的字节数（通常等于 len(Data)）。
	Length uint64

	// Data 是该块的数据内容。
	Data []byte
}

// Option 用于定制恢复目标的行为。零值字段使用默认值。
type Option struct {
	// Granularity 位图粒度（一个 bit 代表的字节数），必须 > 0。
	// 为 0 时使用 DefaultGranularity（512）。
	Granularity uint64

	// Capacity 目标磁盘的总字节数。为 0 时自动探测：
	// 普通文件取文件大小，块设备通过 ioctl/sysfs 获取容量。
	// 块与块之间可以有空洞，但任何块都不得超出 Capacity。
	Capacity uint64

	// NoSyncOnClose 为 true 时，Close 跳过刷写缓存（由调用方自行保证落盘时机）。
	// 默认 false，即 Close 时尽力 Sync。
	NoSyncOnClose bool
}

// Target 是一个带去重位图的恢复目标磁盘。
// 它既不是 io.Writer 也不是 io.ReaderAt：恢复入口是 Restore，
// 它按位图裁剪出尚未恢复的区间并逐段写入底层目标。
// Target 的所有方法都是并发安全的。
type Target struct {
	path        string
	granularity uint64
	capacity    uint64
	syncOnClose bool

	w io.WriteCloser

	bm *bitmap

	mu sync.Mutex
	// closed 之后所有方法都返回错误。
	closed bool

	// stats 由 mu 保护。
	totalRequested uint64 // 累计 Restore 请求的字节数
	totalWritten   uint64 // 累计真实写入的字节数
	totalSkipped   uint64 // 累计因重复而跳过的字节数
}

// OpenPath 打开目标磁盘（普通文件或块设备均可），返回可恢复的 Target。
// target 可以是 /dev/sdb 之类的块设备，也可以是一个磁盘镜像文件。
//
// 块设备要求调用方具有写权限；恢复位图只存在于内存中，
// Close 之后不保留，重复运行会重新恢复全部数据。
func OpenPath(target string, opt *Option) (*Target, error) {
	if target == "" {
		return nil, errors.New("restore: target path is empty")
	}

	o := *opt
	if o.Granularity == 0 {
		o.Granularity = DefaultGranularity
	}

	// 打开前探测容量（文件：stat；块设备：ioctl/sysfs 兜底）。
	if o.Capacity == 0 {
		size, err := extend.GetFileSize(target)
		if err != nil {
			return nil, errors.Wrapf(err, "restore: detect capacity of %s", target)
		}
		o.Capacity = size
	}
	if o.Capacity == 0 {
		return nil, errors.Errorf("restore: %s has zero capacity", target)
	}

	f, err := os.OpenFile(target, os.O_WRONLY, 0)
	if err != nil {
		return nil, errors.Wrapf(err, "restore: open %s", target)
	}

	bitCount := (o.Capacity + o.Granularity - 1) / o.Granularity
	t := &Target{
		path:        target,
		granularity: o.Granularity,
		capacity:    o.Capacity,
		syncOnClose: !o.NoSyncOnClose,
		w:           f,
		bm:          newBitmap(bitCount),
	}

	logger.Debugf("restore: target=%s capacity=%d granularity=%d bitmapBits=%d",
		target, o.Capacity, o.Granularity, bitCount)
	return t, nil
}

// Path 返回目标磁盘的路径。
func (t *Target) Path() string { return t.path }

// Granularity 返回位图粒度（字节/bit）。
func (t *Target) Granularity() uint64 { return t.granularity }

// Capacity 返回目标磁盘的总字节数。
func (t *Target) Capacity() uint64 { return t.capacity }

// Restore 把一个数据块写入目标磁盘。
//
// 语义：块中已经恢复过的部分（按粒度对齐的位图）会被跳过，
// 只有尚未恢复的部分才会真正写入并置位。返回值 written 是本次
// 真实写入的字节数：块完全重复时返回 0，块部分重复时返回新增部分。
//
// 对齐要求：blk.Offset 必须是粒度的整数倍；blk.Offset+blk.Length
// 不得超过目标容量。Length 与 len(Data) 取较小者作为有效数据长度。
//
// 并发：多个 goroutine 可以同时调用 Restore，内部有互斥保护。
// ctx 用于取消等待锁；实际写入不响应 ctx（写入中途无法安全中断）。
func (t *Target) Restore(ctx context.Context, blk *Block) (written uint64, err error) {
	if blk == nil {
		return 0, errors.New("restore: nil block")
	}
	if blk.Length == 0 || len(blk.Data) == 0 {
		return 0, nil // 空块不产生任何效果
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return 0, errors.New("restore: target is closed")
	}

	if err := ctx.Err(); err != nil {
		return 0, errors.Wrap(err, "restore: cancelled")
	}

	g := t.granularity
	if blk.Offset%g != 0 {
		return 0, errors.Errorf("restore: offset %d not aligned to granularity %d", blk.Offset, g)
	}

	// 有效长度 = min(Length, len(Data))，并裁剪到目标容量内。
	length := blk.Length
	if dataLen := uint64(len(blk.Data)); dataLen < length {
		length = dataLen
	}
	if blk.Offset+length > t.capacity {
		return 0, errors.Errorf("restore: block [%d, %d) exceeds capacity %d",
			blk.Offset, blk.Offset+length, t.capacity)
	}

	t.totalRequested += length

	startBit := blk.Offset / g
	endBit := (blk.Offset + length + g - 1) / g // 向上取整，覆盖尾部不完整粒度
	if endBit > t.bm.bits {
		endBit = t.bm.bits
	}

	// 快路径：整块已恢复过，直接跳过（一次全字比较即可判定）。
	if t.bm.isRangeSet(startBit, endBit-startBit) {
		t.totalSkipped += length
		return 0, nil
	}

	// 慢路径：找出未恢复的连续区间逐段写入。
	// 位图以粒度为单位，写入位置 = 位索引 * 粒度；
	// 数据切片位置 = 写入位置 - blk.Offset。
	pos := startBit
	for pos < endBit {
		// 跳过已置位的区间
		clearBit := t.bm.nextClear(pos, endBit)
		if clearBit >= endBit {
			break
		}
		// 找到从 clearBit 开始的连续未置位区间 [clearBit, setBit)
		setBit := t.bm.nextSet(clearBit, endBit)

		writeStart := clearBit * g
		writeEnd := setBit * g
		// 写入区间与块实际数据取交集（尾部不完整粒度时 writeEnd 可能超过块末尾）
		if writeEnd > blk.Offset+length {
			writeEnd = blk.Offset + length
		}
		if writeStart < blk.Offset {
			writeStart = blk.Offset
		}
		if writeEnd > writeStart {
			dataStart := writeStart - blk.Offset
			dataEnd := writeEnd - blk.Offset
			if _, err := t.w.Write(blk.Data[dataStart:dataEnd]); err != nil {
				return written, errors.Wrapf(err, "restore: write %s at %d", t.path, writeStart)
			}
			written += writeEnd - writeStart
		}

		// 置位：按声明区间置位（即使尾部不完整粒度也整粒度置位，
		// 避免下次 Restore 对同一粒度重复写入；超出 capacity 的尾部粒度由 bitmap 容量自然封顶）
		t.bm.setRange(clearBit, setBit-clearBit)
		pos = setBit
	}

	t.totalWritten += written
	t.totalSkipped += length - written
	return written, nil
}

// Close 关闭目标磁盘并释放资源。
// 默认会先尽力 Sync（刷写缓存），失败仅记录日志不返回错误。
// 重复 Close 是安全的，返回 nil。
func (t *Target) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}
	t.closed = true

	if t.syncOnClose {
		if f, ok := t.w.(*os.File); ok {
			if err := f.Sync(); err != nil {
				logger.Warnf("restore: sync %s on close: %v", t.path, err)
			}
		}
	}

	if err := t.w.Close(); err != nil {
		return errors.Wrapf(err, "restore: close %s", t.path)
	}

	logger.Debugf("restore: closed %s requested=%d written=%d skipped=%d",
		filepath.Clean(t.path), t.totalRequested, t.totalWritten, t.totalSkipped)
	return nil
}

// Stats 返回恢复统计信息。
// Requested：累计请求恢复的字节数（重复请求也计入）。
// Written：累计真实写入的字节数。
// Skipped：累计因已恢复而跳过的字节数（Requested - Written）。
func (t *Target) Stats() (requested, written, skipped uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.totalRequested, t.totalWritten, t.totalSkipped
}
