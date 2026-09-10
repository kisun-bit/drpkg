package ioctl

// deviceName 是 Linux 平台下 biotrk 驱动的设备文件路径。
const deviceName = "/dev/biotrk"

// IOC 宏的位宽与位移，与 Linux 内核头文件 <asm-generic/ioctl.h> 保持一致。
//
// _IOC(dir,type,nr,size) =
//
//	(((dir)  << _IOC_DIRSHIFT)  |
//	 ((type) << _IOC_TYPESHIFT) |
//	 ((nr)   << _IOC_NRSHIFT)   |
//	 ((size) << _IOC_SIZESHIFT))
const (
	iocNRBits   = 8
	iocTypeBits = 8
	iocSizeBits = 14
	iocDirBits  = 2

	iocNRShift   = 0
	iocTypeShift = iocNRShift + iocNRBits
	iocSizeShift = iocTypeShift + iocTypeBits
	iocDirShift  = iocSizeShift + iocSizeBits

	iocNone  = 0 // _IOC_NONE
	iocWrite = 1 // _IOC_WRITE
	iocRead  = 2 // _IOC_READ
)

// biotrkIoctlMagic 是 biotrk 驱动的 IOCTL 魔数，与内核驱动中 BIOTRK_IOCTL_MAGIC 一致。
const biotrkIoctlMagic = 'B'

// ioc 计算 IOCTL 命令码（等价于 _IOC 宏）。
func ioc(dir, typ, nr, size uint) uint {
	return (dir << iocDirShift) |
		(typ << iocTypeShift) |
		(nr << iocNRShift) |
		(size << iocSizeShift)
}

// getIoctlCode 将内部控制码常量转换为平台相关的 IOCTL 请求码。
//
// Linux 平台下，传输方向固定为 _IOC_READ|_IOC_WRITE，数据大小字段固定为 0，
// 因为驱动会从请求缓冲区的前 8 字节读取实际数据长度。
//
// code 参数来自 code.go 中定义的 IOCTL_BIOTRK_* 常量。
func getIoctlCode(code uint) uint {
	return ioc(iocRead|iocWrite, biotrkIoctlMagic, code, 0)
}
