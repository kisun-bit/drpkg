// Package block 提供磁盘块级别的读取操作，支持跳过坏扇区。
package blkctl

import (
	"fmt"
	"io"
	"time"

	"github.com/kisun-bit/drpkg/xutil/errutil"
	"github.com/pkg/errors"
)

// DefaultSectorReadTimeout 是读取单个扇区的默认超时时间。
// 当磁盘存在物理坏道时，syscall.Read 可能长时间阻塞（数分钟），
// 设置此超时可避免无限等待，超时后该扇区被当作坏块处理。
var DefaultSectorReadTimeout = 30 * time.Second

// ReadFileSkipBadSector 以跳过坏块的方式读取数据。
//
// timeout 控制每个扇区的读取超时：<=0 使用 DefaultSectorReadTimeout，<0 表示无超时。
// 当读取某个扇区超时时，该扇区被视为坏块（用 0 填充），继续读取后续扇区。
func ReadFileSkipBadSector(
	reader io.ReaderAt,
	offset int64,
	data []byte,
	sectorSize int64,
	timeout time.Duration,
) (n int, containsBadSector bool, err error) {

	if offset%sectorSize != 0 {
		return 0, containsBadSector, errors.Errorf("offset %d is not sector aligned", offset)
	}

	if int64(len(data))%sectorSize != 0 {
		return 0, containsBadSector, errors.Errorf("size %d is not sector aligned", len(data))
	}

	if timeout <= 0 {
		timeout = DefaultSectorReadTimeout
	}

	// 先尝试正常读取
	n, err = readAtWithTimeout(reader, data, offset, timeout)
	if err == nil {
		return n, containsBadSector, nil
	}

	if errutil.IsEOF(err) {
		return n, containsBadSector, io.EOF
	}

	if errutil.IsDataCrcError(err) || isTimeoutError(err) {
		containsBadSector = true

		if len(data) < int(sectorSize) {
			// 小于一个块，直接跳过整个坏块
			for i := range data {
				data[i] = 0
			}
			return len(data), containsBadSector, nil
		}

		// 分成若干个sectorSize读取到buf中，若某个sectorSize区间报DataCrc则跳过
		total := len(data)
		n = 0 // 重新计数，不复用上面整体读取失败时的n

		for start := 0; start < total; start += int(sectorSize) {
			end := start + int(sectorSize)
			if end > total {
				end = total
			}

			chunk := data[start:end]
			curOff := offset + int64(start)

			nn, cerr := readAtWithTimeout(reader, chunk, curOff, timeout)

			switch {
			case cerr == nil:
				n += nn

			case errutil.IsEOF(cerr):
				// 读到文件末尾，之前累计的数据仍然有效
				n += nn
				return n, containsBadSector, io.EOF

			case errutil.IsDataCrcError(cerr) || isTimeoutError(cerr):
				// 该扇区是坏块或读取超时，跳过，清零占位（避免残留脏数据）
				for i := range chunk {
					chunk[i] = 0
				}
				n += len(chunk)

			default:
				// 其他不可恢复错误，直接返回，n为已成功处理的字节数
				return n, containsBadSector, cerr
			}
		}

		return n, containsBadSector, nil
	}

	// 其它未知错误，原样返回
	return n, false, err
}

// timeoutError 用于标记读取超时，与系统级 CRC 错误区分。
type timeoutError struct {
	offset   int64
	duration time.Duration
}

func (e *timeoutError) Error() string {
	return fmt.Sprintf("read timeout at offset %d after %v", e.offset, e.duration)
}

func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	_, ok := err.(*timeoutError)
	return ok
}
