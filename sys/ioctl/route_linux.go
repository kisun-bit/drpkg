package ioctl

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"syscall"
	"unsafe"
)

const findInterfaceRegex = `^(local\s)?[0-9.]+(\s+via\s+(?P<gw>[0-9.]+))?\s+dev\s+(?P<dev>[a-z0-9-]+)\s+src\s+(?P<src>[0-9.]+)`

var (
	findInterfaceRe = regexp.MustCompile(findInterfaceRegex)
	gwidx           = findInterfaceRe.SubexpIndex("gw")
	devIdx          = findInterfaceRe.SubexpIndex("dev")
	srcIdx          = findInterfaceRe.SubexpIndex("src")
)

type rtmsg struct {
	// 参考 https://man7.org/linux/man-pages/man7/rtnetlink.7.html 中对于rtmsg的描述.
	Family   byte // Address family of route
	DstLen   byte // Length of destination
	SrcLen   byte // Length of source
	TOS      byte // TOS filter
	Table    byte // Routing table ID
	Protocol byte // Routing protocol
	Scope    byte
	Type     byte

	Flags uint32
}

func GetRoutingTable(_ context.Context) ([]*Route, error) {
	// 参考 https://github.com/google/gopacket/blob/master/routing/routing.go
	tab, err := syscall.NetlinkRIB(syscall.RTM_GETROUTE, syscall.AF_UNSPEC)
	if err != nil {
		return nil, fmt.Errorf("unable to call netlink for route table: %w", err)
	}
	msgs, err := syscall.ParseNetlinkMessage(tab)
	if err != nil {
		return nil, fmt.Errorf("unable to parse netlink messages: %w", err)
	}
	var routes []*Route
msgLoop:
	for _, msg := range msgs {
		switch msg.Header.Type {
		case syscall.NLMSG_DONE:
			break msgLoop
		case syscall.RTM_NEWROUTE:
			// 参考net库下的处理代码, 此rtmsg主要用于获取目标网络的掩码.
			rt := (*rtmsg)(unsafe.Pointer(&msg.Data[0]))
			var (
				gw       net.IP
				dstNet   *net.IPNet
				ifaceIdx = -1
				ipv4     bool
				dfltGw   bool
			)
			switch rt.Family {
			case syscall.AF_INET:
				ipv4 = true
			case syscall.AF_INET6:
				ipv4 = false
			default:
				continue msgLoop
			}
			// 参考：https://github.com/solidsnack/docker/blob/ea0cce6270471686a41a67b84c63edfd09f8adb8/pkg/netlink/netlink_linux.go#L645
			attrs, err := syscall.ParseNetlinkRouteAttr(&msg)
			if err != nil {
				return nil, fmt.Errorf("failed to parse netlink route attributes: %w", err)
			}
			for _, attr := range attrs {
				switch attr.Attr.Type {
				case syscall.RTA_DST:
					dstNet = &net.IPNet{
						IP:   attr.Value,
						Mask: net.CIDRMask(int(rt.DstLen), len(attr.Value)*8),
					}
				case syscall.RTA_GATEWAY:
					gw = attr.Value
				case syscall.RTA_OIF:
					ifaceIdx = int(*(*uint32)(unsafe.Pointer(&attr.Value[0])))
				}
			}
			if dstNet == nil {
				dfltGw = true
			}
			if dstNet == nil {
				if ipv4 {
					dstNet = &net.IPNet{
						IP:   net.IP{0, 0, 0, 0},
						Mask: net.CIDRMask(0, 32),
					}
				} else {
					dstNet = &net.IPNet{
						IP:   net.ParseIP("::"),
						Mask: net.CIDRMask(0, 128),
					}
				}
			}
			if gw == nil {
				if ipv4 {
					gw = net.ParseIP("0.0.0.0").To4()
				} else {
					gw = net.ParseIP("::")
				}
			}
			if gw != nil && dstNet != nil && ifaceIdx > 0 {
				iface, err := net.InterfaceByIndex(ifaceIdx)
				if err != nil {
					return nil, fmt.Errorf("unable to get interface at index %d: %w", ifaceIdx, err)
				}
				srcIP, err := interfaceLocalIP(iface, ipv4)
				if err != nil {
					return nil, err
				}
				if srcIP == nil {
					continue
				}
				routes = append(routes, &Route{
					LocalIP:   srcIP,
					RoutedNet: dstNet,
					Interface: iface,
					Gateway:   gw,
					Default:   dfltGw,
				})
			}
		}
	}
	return routes, nil
}

func GetRoute(ctx context.Context, routedNet *net.IPNet) (*Route, error) {
	ip := routedNet.IP
	cmd := exec.CommandContext(ctx, "ip", "route", "get", ip.String())
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get route for %s: %w", ip, err)
	}
	match := findInterfaceRe.FindStringSubmatch(string(out))
	if match == nil {
		return nil, fmt.Errorf("output of ip route did not match %s (output: %s)", findInterfaceRegex, out)
	}
	var gatewayIP net.IP
	gw := match[gwidx]
	if gw != "" {
		gatewayIP = net.ParseIP(gw).To4()
		if gatewayIP == nil {
			return nil, fmt.Errorf("unable to parse gateway IP %s", gw)
		}
	}
	iface, err := net.InterfaceByName(match[devIdx])
	if err != nil {
		return nil, fmt.Errorf("unable to get interface %s: %w", match[devIdx], err)
	}
	localIP := net.ParseIP(match[srcIdx]).To4()
	if localIP == nil {
		return nil, fmt.Errorf("unable to parse local IP %s", match[srcIdx])
	}
	return &Route{
		Gateway:   gatewayIP,
		Interface: iface,
		RoutedNet: routedNet,
		LocalIP:   localIP,
	}, nil
}

func (r *Route) addStatic(ctx context.Context) error {
	return exec.CommandContext(ctx, "ip", "route", "add", r.RoutedNet.String(), "via", r.Gateway.String(), "dev", r.Interface.Name).Run()
}

func (r *Route) removeStatic(ctx context.Context) error {
	return exec.CommandContext(ctx, "ip", "route", "del", r.RoutedNet.String(), "via", r.Gateway.String(), "dev", r.Interface.Name).Run()
}
