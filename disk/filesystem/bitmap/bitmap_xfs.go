package bitmap

type xfsBitmapParser struct {
	fr *fsRegionReader
}

func NewXfsBitmapParser(device string, fsStart int64, fsSize int64) (FsBitmapParser, error) {
	fr, err := newFsRegionReader(device, fsStart, fsSize)
	if err != nil {
		return nil, err
	}
	_ = fr

	return nil, nil
}

func (b *xfsBitmapParser) String() string {
	return "<xfsBitmapParser(uuid=%v)>"
}

// Dump 导出位图数据
func (b *xfsBitmapParser) Dump() (bitmap *FsBitmap, err error) {
	return nil, err
}
