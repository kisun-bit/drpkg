package x2xcore

type NetworkConfig struct {
	Interfaces []InterfaceConfig `json:"interfaces"`

	// 全局DNS
	GlobalDNS []string `json:"global_dns,omitempty"`

	// 全局路由
	Routes []RouteConfig `json:"routes,omitempty"`
}

type InterfaceConfig struct {
	// 匹配网卡
	MAC string `json:"mac"`

	// 重命名后的网卡名
	Name string `json:"name,omitempty"`

	// 是否启用
	Enabled bool `json:"enabled"`

	MTU int `json:"mtu,omitempty"`

	// DHCP
	DHCP bool `json:"dhcp,omitempty"`

	// 静态地址
	IPAddr []IPConfig `json:"ipAddr,omitempty"`

	// 接口级DNS
	DNS []string `json:"dns,omitempty"`

	// 默认网关
	Gateway string `json:"gateway,omitempty"`
}

type IPConfig struct {
	// 192.168.1.10/24
	// 2001:db8::10/64
	Address string `json:"address"`
}

type RouteConfig struct {
	// 目标网段
	// 0.0.0.0/0
	// ::/0
	Destination string `json:"destination"`

	// 下一跳
	Gateway string `json:"gateway,omitempty"`

	// 绑定哪个接口
	InterfaceMAC string `json:"interface_mac,omitempty"`

	// 可选
	Metric int `json:"metric,omitempty"`

	// 策略路由预留
	Table int `json:"table,omitempty"`
}

type NetworkInjector interface {
	Inject() error
}
