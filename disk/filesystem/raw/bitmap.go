package raw

import (
	"fmt"

	"github.com/kisun-bit/drpkg/define"
	"github.com/kisun-bit/drpkg/disk/filesystem/bitmap"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
)

type RawBitmapParser struct {
	dev   string
	start int64
	size  int64
	fr    *extend.FsRegionReader
}

func NewBitmapParser(dev string, start int64, size int64) (bitmap.FsBitmapParser, error) {
	fr, e := extend.NewFsRegionReader(dev, start, size)
	if e != nil {
		return nil, e
	}
	return &RawBitmapParser{dev: dev, start: start, size: size, fr: fr}, nil
}

func (p *RawBitmapParser) String() string {
	return fmt.Sprintf("<RawBitmapParser(dev=%s,start=%d,size=%d)>",
		p.dev, p.start, p.size)
}

func (p *RawBitmapParser) Dump() (*bitmap.FsBitmap, error) {
	defer func() {
		if p.fr != nil {
			_ = p.fr.Close()
		}
	}()

	// RAW 模式没有文件系统结构可解析，
	// 直接把整个区域视为全部占用（bit=1），不做实际读取。
	// blockSize 这里按 1 字节为单位，即 bits == size；
	// 如果你的 RAW 模式有自己的块大小定义，请替换成实际值。
	const blockSize = 1
	bits := p.size

	fb := bitmap.NewFsBitmap(define.FsTypeUnknown, bitmap.BitmapRaw, bits, blockSize)

	// 整字节填充 0xFF，即所有 bit = 1（used）
	fb.SetAll()

	logger.Debugf("%s.Dump() bits=%d (all used, raw mode)", p, bits)

	return fb, nil
}
