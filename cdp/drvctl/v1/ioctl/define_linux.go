package ioctl

import (
	"cdpctl/utils"
	"path/filepath"
)

const RequestSize uint64 = 0

const DriverSymbolName = "/dev/biotrk"

// 与 Linux _IOC 宏相关的位宽/位移（与内核头一致）
const (
	IOC_NRBITS   = 8
	IOC_TYPEBITS = 8
	IOC_SIZEBITS = 14
	IOC_DIRBITS  = 2

	IOC_NRSHIFT   = 0
	IOC_TYPESHIFT = IOC_NRSHIFT + IOC_NRBITS
	IOC_SIZESHIFT = IOC_TYPESHIFT + IOC_TYPEBITS
	IOC_DIRSHIFT  = IOC_SIZESHIFT + IOC_SIZEBITS

	IOC_NONE  = 0
	IOC_WRITE = 1
	IOC_READ  = 2
)

var drvParameterPersistFile = filepath.Join(utils.ExecDir(), "biotrk.rUn5t0r.params")

// IOC 计算函数，返回与 C _IOC(...) 等价的请求值（uint）
func IOC(dir, typ, nr, size uint) uint {
	return (dir << IOC_DIRSHIFT) | (typ << IOC_TYPESHIFT) | (nr << IOC_NRSHIFT) | (size << IOC_SIZESHIFT)
}

// IO 方便宏：_IO / _IOR / _IOW / _IOWA
func IO(typ uint, nr uint) uint {
	return IOC(IOC_NONE, typ, nr, 0)
}
func IOR(typ uint, nr uint, size uint) uint {
	return IOC(IOC_READ, typ, nr, size)
}
func IOW(typ uint, nr uint, size uint) uint {
	return IOC(IOC_WRITE, typ, nr, size)
}
func IOWR(typ uint, nr uint, size uint) uint {
	return IOC(IOC_READ|IOC_WRITE, typ, nr, size)
}

// BIOTRK_IOCTL_MAGIC 示例：定义 magic byte（与 C 中 BIOTRK_IOCTL_MAGIC 相同）
const BIOTRK_IOCTL_MAGIC = 'B' // 或者用数字，例如 0xBE

func GetIoctlCode(code uint) uint {
	return IO(BIOTRK_IOCTL_MAGIC, code)
}
