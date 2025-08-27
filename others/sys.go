package others

// ########################################## Windows平台相关 ##########################################

// 关于Win32 IOCTL各个控制码使用及其意义, 请参考: https://learn.microsoft.com/en-us/windows/win32/api/winioctl
const (
	IOCTL_DISK_GET_LENGTH_INFO           = 0x0007405C
	IOCTL_DISK_GET_DISK_ATTRIBUTES       = 0x000700f0
	IOCTL_STORAGE_QUERY_PROPERTY         = 0x002d1400
	IOCTL_DISK_GET_PARTITION_INFO_EX     = 0x70048
	IOCTL_VOLUME_GET_VOLUME_DISK_EXTENTS = 0x00560000
)
