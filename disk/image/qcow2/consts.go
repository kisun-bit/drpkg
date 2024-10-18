//go:build linux

package qcow2

const (
	blockTypeRead uint8 = iota
	blockTypeWrite
)

const (
	// 直写.
	Direct QemuIOCacheMode = iota
	// 直写式异步IO.
	DirectWithAio
	// 回写.
	Writeback
	// 回写式异步IO.
	WritebackWithAio
)

const _MaxQueueSizeForEffectReader = 2000

const _DefaultQueueSizeForEffectReader = 1 << 9

const _MaxBlockSizeForEffectReader = 10 << 20

const _DefaultBlockSizeForEffectReader = 1 << 20
