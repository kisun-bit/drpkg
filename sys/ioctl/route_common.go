package ioctl

import (
	"context"
	"fmt"
	"github.com/pkg/errors"
	"net"
)

type RouteGeneral struct {
	IsDefault       bool   `json:"is_default"`
	LocalIP         string `json:"local_ip"`
	Gateway         string `json:"gateway"`
	InterfaceIdx    int    `json:"interface_idx"`
	InterfaceName   string `json:"interface_name"`
	TargetNetCIDRIP string `json:"target_net_cidr_ip"`
}

type Route struct {
	LocalIP   net.IP
	RoutedNet *net.IPNet
	Interface *net.Interface
	Gateway   net.IP
	Default   bool
}

func DefaultRoute(ctx context.Context) (*Route, error) {
	rt, err := GetRoutingTable(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range rt {
		if r.Default {
			return r, nil
		}
	}
	return nil, errors.New("unable to find a default route")
}

func Subnets(routes []*Route) []*net.IPNet {
	ns := make([]*net.IPNet, len(routes))
	for i, r := range routes {
		ns[i] = r.RoutedNet
	}
	return ns
}

func Routes(c context.Context, ms []*net.IPNet) []*Route {
	rs := make([]*Route, 0, len(ms))
	for _, n := range ms {
		r, err := GetRoute(c, n)
		if err != nil {
			continue
		}
		rs = append(rs, r)
	}
	return rs
}

func (rg *RouteGeneral) String() string {
	if rg.IsDefault {
		return fmt.Sprintf("[Default] VIA %s DEV %s, GW %s", rg.LocalIP, rg.InterfaceName, rg.Gateway)
	}
	return fmt.Sprintf("%s VIA %s DEV %s, GW %s", rg.TargetNetCIDRIP, rg.LocalIP, rg.InterfaceName, rg.Gateway)
}

func (r *Route) Routes(ip net.IP) bool {
	return r.RoutedNet.Contains(ip)
}

func (r *Route) String() string {
	if r.Default {
		return fmt.Sprintf("default via %s dev %s, gw %s", r.LocalIP, r.Interface.Name, r.Gateway)
	}
	return fmt.Sprintf("%s via %s dev %s, gw %s", r.RoutedNet, r.LocalIP, r.Interface.Name, r.Gateway)
}

func (r *Route) AddStatic(ctx context.Context) (err error) {
	return r.addStatic(ctx)
}

func (r *Route) RemoveStatic(ctx context.Context) (err error) {
	return r.removeStatic(ctx)
}

func (r *Route) Convert2RouteGeneral() RouteGeneral {
	rg := RouteGeneral{}
	rg.LocalIP = r.LocalIP.String()
	rg.Gateway = r.Gateway.String()
	rg.InterfaceIdx = r.Interface.Index
	rg.InterfaceName = r.Interface.Name
	rg.TargetNetCIDRIP = r.RoutedNet.String()
	rg.IsDefault = r.Default
	return rg
}

func interfaceLocalIP(iface *net.Interface, ipv4 bool) (net.IP, error) {
	addrList, err := iface.Addrs()
	if err != nil {
		return net.IP{}, fmt.Errorf("unable to get interface addresses for interface %s: %w", iface.Name, err)
	}
	for _, addr := range addrList {
		ip, _, err := net.ParseCIDR(addr.String())
		if err != nil {
			return net.IP{}, fmt.Errorf("unable to parse address %s: %v", addr.String(), err)
		}
		if ip4 := ip.To4(); ip4 != nil {
			if !ipv4 {
				continue
			}
			return ip4, nil
		} else if ipv4 {
			continue
		}
		return ip, nil
	}
	return nil, nil
}
