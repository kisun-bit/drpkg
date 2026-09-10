package ioctl

// deviceName 是 Windows 平台下 biotrk 驱动的符号链接名称。
const deviceName = `\\.\biotrk`

// CTL_CODE 宏的各字段位置与掩码，遵循 Windows 驱动开发规范。
const (
	deviceUnknown = 0x00000022 // FILE_DEVICE_UNKNOWN

	methodInDirect  = 1 // METHOD_IN_DIRECT
	methodOutDirect = 2 // METHOD_OUT_DIRECT

	fileAnyAccess = 0 // FILE_ANY_ACCESS
)

// ctlCode 根据功能码构造 IOCTL 控制码。
//
// Windows 内核通过 CTL_CODE 宏组合设备类型、功能号、传输方式和访问权限。
// 详见 WDK 文档：CTL_CODE(DeviceType, Function, Method, Access)。
func ctlCode(function uint32) uint32 {
	return (deviceUnknown << 16) | (fileAnyAccess << 14) | (function << 2) | methodInDirect
}

// getIoctlCode 将内部控制码常量转换为平台相关的 IOCTL 请求码。
//
// code 参数来自 code.go 中定义的 IOCTL_BIOTRK_* 常量。
func getIoctlCode(code uint) uint32 {
	return ctlCode(uint32(code))
}
