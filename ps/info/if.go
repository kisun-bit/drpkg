package info

import (
	"github.com/shirou/gopsutil/v3/net"
)

type IF struct {
	net.InterfaceStat
	IFExtra
}

type IFExtra struct {
	Linked   bool `json:"is_linked"`
	Physical bool `json:"is_physical"`

	//IPv4BootProto   string   `json:"ipv4_boot_proto"`
	//IPv4GatewayList []string `json:"ipv4_gateway_list"`
	//IPv4DnsList     []string `json:"ipv4_dns_list"`
	//IPv6BootProto   string   `json:"ipv6_boot_proto"`
	//IPv6GatewayList []string `json:"ipv6_gateway_list"`
	//IPv6DnsList     []string `json:"ipv6_dns_list"`
	//
	//// IfCfgPath 网卡配置文件，Linux特有
	//IfCfgPath string `json:"ifcfg_path"`
}
