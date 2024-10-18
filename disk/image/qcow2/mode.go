//go:build linux

package qcow2

func (mode *QemuIOCacheMode) String() string {
	switch *mode {
	case Direct:
		return "direct"
	case DirectWithAio:
		return "direct&aio"
	case Writeback:
		return "writeback"
	case WritebackWithAio:
		return "writeback&aio"
	default:
		return "unknown"
	}
}
