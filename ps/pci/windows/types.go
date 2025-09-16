package windows

type Filter func(p *PCI) bool

type PCI struct {
	FriendlyName  string   // 显示名称.
	BusNumber     uint32   // 总线号, 例如PCI硬件的0x00002表示PCI 总线2.
	Address       uint32   // 地址, 如PCI硬件的0x70007即00070007, 表示PCI 设备 7、功能 7
	InstancePath  string   // 设备实例路径.
	HardwareIDs   []string // 硬件ID集合.
	CompatibleIDs []string // 兼容ID集合.
	BusClassGUID  string   // 总线类型GUID. 以GUID_BUS_TYPE开头的常量ID.
	Service       string   // 内核驱动服务名称, 一般为驱动文件名(除文件后缀).
	Status        uint32   // 硬件状态.
	Problem       uint32   // 状态问题代码.

	Vendor uint16
	Device uint16
	Class  uint32

	VendorName      string
	DeviceName      string
	ClassDetailName string
}
