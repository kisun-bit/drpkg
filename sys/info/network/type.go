package network

import "github.com/shirou/gopsutil/v3/net"

type FixedInterfaceStat struct {
	Index        int                   `json:"index"`
	MTU          int                   `json:"mtu"`           // maximum transmission unit
	Name         string                `json:"name"`          // e.g., "en0", "lo0", "eth0.100"
	HardwareAddr string                `json:"hardware_addr"` // IEEE MAC-48, EUI-48 and EUI-64 form
	Flags        []string              `json:"flags"`         // e.g., FlagUp, FlagLoopback, FlagMulticast
	Addrs        net.InterfaceAddrList `json:"addrs"`
}

type Ethernet struct {
	FixedInterfaceStat
	Physical        bool     `json:"physical"`
	IPv4BootProto   string   `json:"ipv4_boot_proto"`
	IPv4GatewayList []string `json:"ipv4_gateway_list"`
	IPv4DnsList     []string `json:"ipv4_dns_list"`
	IPv6BootProto   string   `json:"ipv6_boot_proto"`
	IPv6GatewayList []string `json:"ipv6_gateway_list"`
	IPv6DnsList     []string `json:"ipv6_dns_list"`
	IfCfgPath       string   `json:"ifcfg_path"`
}
