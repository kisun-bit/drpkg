package bitmap

import (
	"github.com/pkg/errors"
)

type xfsBitmapParser struct {
	fr *fsRegionReader
}

func NewXfsBitmapParser(device string, fsStart int64, fsSize int64) (FsBitmapParser, error) {
	fr, err := newFsRegionReader(device, fsStart, fsSize)
	if err != nil {
		return nil, err
	}
	return &xfsBitmapParser{fr: fr}, nil
}

func (b *xfsBitmapParser) String() string {
	return "<xfsBitmapParser(uuid=%v)>"
}

// Dump 导出位图数据
func (b *xfsBitmapParser) Dump() (bitmap *FsBitmap, err error) {
	return nil, errors.New("xfs bitmap parser is not implemented yet")
}
