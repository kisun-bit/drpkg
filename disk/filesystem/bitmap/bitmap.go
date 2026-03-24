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

// FsBitmapParser 表示文件系统位图解析接口
type FsBitmapParser interface {
	fmt.Stringer

	// Type 返回文件系统类型
	Type() types.FsType

	// Bits 返回位图的总 bit 数
	Bits() int64

	// Dump 导出位图数据
	// data: 位图字节数据
	// bytesPerBit: 每个 bit 表示的磁盘字节数（例如 4096）
	// kind: 位图来源类型
	Dump() (data []byte, bytesPerBit int, kind BitmapKind, err error)
}
