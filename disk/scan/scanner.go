package scan

import (
	"io"
	"os"
	"runtime"
	"sync"

	"github.com/kisun-bit/drpkg/extend"
	"github.com/pkg/errors"
)

type Range struct {
	Offset int64
	Data   []byte
}

type Scanner struct {
	Path        string
	Size        int64
	BlockSize   int64 // 推荐 4MB
	Concurrency int
	OnData      func(r Range) // 非0数据回调
}

// =========================
// 对外入口
// =========================

func (s *Scanner) Run() error {
	if s.BlockSize == 0 {
		s.BlockSize = 4 * 1024 * 1024 // 4MB
	}
	if s.Concurrency == 0 {
		s.Concurrency = runtime.NumCPU()
	}

	fd, err := os.Open(s.Path)
	if err != nil {
		return err
	}
	defer fd.Close()

	// 获取设备大小（如果没传）
	if s.Size == 0 {
		size, err := extend.FileSize(s.Path)
		if err != nil {
			return err
		}
		s.Size = int64(size)
	}

	// Linux 优化
	fadvise(fd)

	// 分段
	chunks := splitRange(s.Size, int64(s.Concurrency))

	var wg sync.WaitGroup
	wg.Add(len(chunks))

	errCh := make(chan error, len(chunks))

	for _, c := range chunks {
		go func(start, end int64) {
			defer wg.Done()
			if e := s.scanRange(fd, start, end); e != nil {
				errCh <- e
			}
		}(c[0], c[1])
	}

	wg.Wait()

	close(errCh)
	for e := range errCh {
		if e != nil && err == nil {
			err = e
		}
	}

	return err
}

// =========================
// 核心扫描逻辑
// =========================

func (s *Scanner) scanRange(fd *os.File, start, end int64) error {
	buf := make([]byte, s.BlockSize)

	offset := start

	for offset < end {
		toRead := s.BlockSize
		if offset+toRead > end {
			toRead = end - offset
		}

		n, err := fd.ReadAt(buf[:toRead], offset)
		if err != nil && err != io.EOF {
			return errors.Wrapf(err, "failed to read range at offset %d (%s)", offset, s.Path)
		}
		if n == 0 {
			return nil
		}

		if !extend.IsAllZero(buf[:n]) {
			if s.OnData != nil {
				dataCopy := make([]byte, n)
				copy(dataCopy, buf[:n])
				s.OnData(Range{
					Offset: offset,
					Data:   dataCopy,
				})
			}
		}

		offset += int64(n)
	}

	return nil
}

// =========================
// 工具函数
// =========================

func splitRange(size int64, parts int64) [][2]int64 {
	var res [][2]int64

	chunk := size / parts

	for i := int64(0); i < parts; i++ {
		start := i * chunk
		end := start + chunk

		if i == parts-1 {
			end = size
		}

		res = append(res, [2]int64{start, end})
	}
	return res
}
