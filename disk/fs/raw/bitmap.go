package raw

import (
	"fmt"

	"github.com/kisun-bit/drpkg/defs"
	"github.com/kisun-bit/drpkg/disk/fs/bitmap"
	"github.com/kisun-bit/drpkg/xutil"
	"github.com/kisun-bit/drpkg/logger"
)

type BitmapParser struct {
	dev   string
	start int64
	size  int64
	fr    *xutil.FsRegionReader
}

func NewBitmapParser(dev string, start int64, size int64) (bitmap.FsBitmapParser, error) {
	fr, e := xutil.NewFsRegionReader(dev, start, size)
	if e != nil {
		return nil, e
	}
	return &BitmapParser{dev: dev, start: start, size: size, fr: fr}, nil
}

func (p *BitmapParser) String() string {
	return fmt.Sprintf("<RAWBitmapParser(dev=%s,start=%d,size=%d)>",
		p.dev, p.start, p.size)
}

func (p *BitmapParser) Dump() (*bitmap.FsBitmap, error) {
	defer func() {
		if p.fr != nil {
			_ = p.fr.Close()
		}
	}()

	const blockSize = 4 << 10
	bits := (p.size + (blockSize - 1)) / blockSize

	fb := bitmap.NewFsBitmap(defs.FsTypeUnknown, bitmap.BitmapRaw, bits, blockSize)

	// 整字节填充 0xFF，即所有 bit = 1（used）
	fb.SetAll()

	logger.Debugf("%s.Dump() bits=%d (all used, raw mode)", p, bits)

	return fb, nil
}
