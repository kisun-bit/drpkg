package bitmap

import (
	"fmt"

	"github.com/kisun-bit/drpkg/disk/filesystem/types"
)

// BitmapKind 表示位图的数据来源类型
type BitmapKind int

const (
	// BitmapRaw 表示未经过文件系统解析的原始位图
	BitmapRaw BitmapKind = iota

	// BitmapFromFS 表示经过文件系统解析得到的位图
	BitmapFromFS
)

type FsBitmap struct {
	// Type 文件系统类型
	Type types.FsType

	// BitmapKind: 位图来源类型
	BitmapKind BitmapKind

	// Bitmap 位图数据
	Bitmap []byte

	// BlockSize 数据块大小
	BlockSize int
}

// FsBitmapParser 表示文件系统位图解析接口
type FsBitmapParser interface {
	fmt.Stringer

	// Dump 导出位图数据
	Dump() (bitmap *FsBitmap, err error)
}
