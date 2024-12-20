package ioctl

import (
	"bytes"
	"context"
	"fmt"
	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
	"net"
	"os/exec"
	"strconv"
	"strings"
)

func GetRoutingTable(_ context.Context) ([]*Route, error) {
	table, err := winipcfg.GetIPForwardTable2(windows.AF_UNSPEC)
	if err != nil {
		return nil, fmt.Errorf("unable to get routing table: %w", err)
	}
	var routes []*Route
	for _, row := range table {
		dst := row.DestinationPrefix.Prefix()
		if !dst.IsValid() {
			continue
		}
		gw := row.NextHop.Addr()
		if !gw.IsValid() {
			continue
		}
		ifaceIdx := int(row.InterfaceIndex)
		iface, err := net.InterfaceByIndex(ifaceIdx)
		if err != nil {
			return nil, fmt.Errorf("unable to get interface at index %d: %w", ifaceIdx, err)
		}
		localIP, err := interfaceLocalIP(iface, dst.Addr().Is4())
		if err != nil {
			return nil, err
		}
		if localIP == nil {
			continue
		}
		gwc := gw.AsSlice()
		ip := dst.Addr().AsSlice()
		var mask net.IPMask
		if dst.Addr().Is4() {
			mask = net.CIDRMask(dst.Bits(), 32)
		} else {
			mask = net.CIDRMask(dst.Bits(), 128)
		}
		var dflt bool
		if len(gwc) == 4 {
			dflt = !bytes.Equal(gwc, []byte{0, 0, 0, 0})
		} else {
			dflt = !bytes.Equal(gwc, []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
		}
		routes = append(routes, &Route{
			LocalIP: localIP,
			Gateway: gwc,
			RoutedNet: &net.IPNet{
				IP:   ip,
				Mask: mask,
			},
			Interface: iface,
			Default:   dflt,
		})
	}
	return routes, nil
}

func GetRoute(ctx context.Context, routedNet *net.IPNet) (*Route, error) {
	ip := routedNet.IP
	pshScript := fmt.Sprintf(`
$job = Find-NetRoute -RemoteIPAddress "%s" -AsJob | Wait-Job -Timeout 30
if ($job.State -ne 'Completed') {
	throw "timed out getting route after 30 seconds."
}
$obj = $job | Receive-Job
$obj.IPAddress
$obj.NextHop
$obj.InterfaceIndex[0]
`, ip)
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", pshScript)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("unable to run 'Find-Netroute -RemoteIPAddress %s': %w", ip, err)
	}
	lines := strings.Split(string(out), "\n")
	localIP := net.ParseIP(strings.TrimSpace(lines[0])).To4()
	if localIP == nil {
		return nil, fmt.Errorf("unable to parse IP from %s", lines[0])
	}
	gatewayIP := net.ParseIP(strings.TrimSpace(lines[1])).To4()
	if gatewayIP == nil {
		return nil, fmt.Errorf("unable to parse gateway IP from %s", lines[1])
	}
	interfaceIndex, err := strconv.Atoi(strings.TrimSpace(lines[2]))
	if err != nil {
		return nil, fmt.Errorf("unable to parse interface index from %s: %w", lines[2], err)
	}
	iface, err := net.InterfaceByIndex(interfaceIndex)
	if err != nil {
		return nil, fmt.Errorf("unable to get interface for index %d: %w", interfaceIndex, err)
	}
	return &Route{
		LocalIP:   localIP,
		Gateway:   gatewayIP,
		Interface: iface,
		RoutedNet: routedNet,
	}, nil
}

func maskToIP(mask net.IPMask) (ip net.IP) {
	ip = make(net.IP, len(mask))
	copy(ip[:], mask)
	return ip
}

func (r *Route) addStatic(ctx context.Context) error {
	mask := maskToIP(r.RoutedNet.Mask)
	cmd := exec.CommandContext(ctx,
		"route",
		"ADD",
		r.RoutedNet.IP.String(),
		"MASK",
		mask.String(),
		r.Gateway.String(),
	)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to create route %s: %w", r, err)
	}
	if !strings.Contains(string(out), "OK!") {
		return fmt.Errorf("failed to create route %s: %s", r, strings.TrimSpace(string(out)))
	}
	return nil
}

func (r *Route) removeStatic(ctx context.Context) error {
	cmd := exec.CommandContext(ctx,
		"route",
		"DELETE",
		r.RoutedNet.IP.String(),
	)
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to delete route %s: %w", r, err)
	}
	return nil
}
