package ioctl

const (
	DriverSymbolName = "\\\\.\\biotrk"
)

const (
	METHOD_IN_DIRECT = 1 // input/output 缓冲区是否隔离

	FILE_ANY_ACCESS = 0 // 访问权限：任意访问

	FILE_DEVICE_UNKNOWN = 0x00000022 // 设备类型：未知设备
)

const (
	cdpConfigRegPath    = "SYSTEM\\CurrentControlSet\\Services\\biotrk\\Parameters"
	regKeyDrvParameters = "PolicyConfigData"
	regKeyCdpActive     = "PolicyActive"
)

func CtlCode(function uint32) uint32 {
	return (FILE_DEVICE_UNKNOWN << 16) | (FILE_ANY_ACCESS << 14) | (function << 2) | METHOD_IN_DIRECT
}

func GetIoctlCode(code uint) uint {
	return uint(CtlCode(uint32(code)))
}
