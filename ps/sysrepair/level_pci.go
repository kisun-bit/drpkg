package sysrepair

// PciCompat Pci硬件兼容性
type PciCompat struct {
	// UniPci PCI设备统一标识
	UniPci string

	// IsSupported 是否支持此硬件
	// 判定标准：有匹配的驱动、且驱动文件存在时为true
	IsSupported bool

	// Driver 驱动名称
	// Linux: 驱动模块名
	// Windows: 内核服务名
	Driver string

	// IsBootCapable 驱动是否支持引导阶段加载（boot-start）
	IsBootCapable bool
}
