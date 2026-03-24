package bitmap

import (
	"github.com/kisun-bit/drpkg/disk/filesystem/types"
)

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

// Type 返回文件系统类型
func (b *xfsBitmapParser) Type() types.FsType {
	return types.FsTypeXfs
}

// Bits 返回位图的总 bit 数
func (b *xfsBitmapParser) Bits() int64 {
	return 0
}

// Dump 导出位图数据
// data: 位图字节数据
// bytesPerBit: 每个 bit 表示的磁盘字节数（例如 4096）
// kind: 位图来源类型
func (b *xfsBitmapParser) Dump() (data []byte, bytesPerBit int, kind BitmapKind, err error) {
	return nil, 0, BitmapFromFS, err
}
