package x2xcore

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
	"gopkg.in/yaml.v3"
)

type linuxNetworkInjector struct {
	root string
	cfg  *NetworkConfig

	backend NetworkBackend
}

func NewNetworkInjector(root string, cfg *NetworkConfig) (NetworkInjector, error) {
	logger.Debugf("newLinuxNetworkInjector(%q) ++", root)
	defer logger.Debugf("newLinuxNetworkInjector(%q) --", root)

	if !extend.IsDir(root) {
		return nil, errors.New("root is not a directory")
	}
	if cfg == nil {
		return nil, errors.New("network configuration is nil")
	}

	lni := &linuxNetworkInjector{
		root:    root,
		cfg:     cfg,
		backend: detectNetworkBackend(root),
	}

	logger.Debugf("newLinuxNetworkInjector: config=\n%s", extend.Pretty(cfg))

	if err := lni.reset(); err != nil {
		return nil, err
	}

	logger.Debugf("%s initialized", lni)

	return lni, nil
}

func (n *linuxNetworkInjector) String() string {
	return fmt.Sprintf("NetworkInjector(%q,%s)", n.root, n.backend)
}

func (n *linuxNetworkInjector) reset() error {
	logger.Debugf("%s.reset: ++", n)
	defer logger.Debugf("%s.reset: --", n)

	macs := collectMACs(n.cfg)

	steps := []struct {
		name string
		fn   func() error
	}{
		{
			name: "cleanup old network config",
			fn: func() error {
				return renameAllFileIfContainsMac(
					macs,
					buildCleanupPaths(n.root),
				)
			},
		},
		{
			name: "disable persistent net rules",
			fn: func() error {
				return disableLegacyPersistentNetRules(n.root)
			},
		},
		{
			name: "generate rename rules",
			fn: func() error {
				return generateNetworkRenameRules(n.root, n.cfg)
			},
		},
	}

	for _, step := range steps {
		if err := step.fn(); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
	}

	return nil
}

func (n *linuxNetworkInjector) Inject() error {
	logger.Debugf("%s.Inject: ++", n)
	defer logger.Debugf("%s.Inject: --", n)

	var injectFun func() error = nil
	switch n.backend {
	case BackendIfcfg:
		injectFun = n.injectNetworkByIfCfg
	case BackendNetplan:
		injectFun = n.injectNetworkByNetplan
	case BackendInterfaces:
		injectFun = n.injectNetworkByInterfaces
	case BackendNMKeyfile:
		injectFun = n.injectNetworkByNetworkManager
	case BackendWicked:
		injectFun = n.injectNetworkBySuseWicked
	default:
		return errors.Errorf("unknown backend %q", n.backend)
	}

	if err := injectFun(); err != nil {
		return err
	}

	return nil
}

func (n *linuxNetworkInjector) injectNetworkByIfCfg() error {
	logger.Debugf("%s.injectNetworkByIfCfg: ++", n)
	defer logger.Debugf("%s.injectNetworkByIfCfg: --", n)

	if len(n.cfg.Interfaces) == 0 {
		return nil
	}

	baseDir := filepath.Join(
		n.root,
		"etc/sysconfig/network-scripts",
	)

	logger.Debugf(
		"%s.injectNetworkByIfCfg: base dir: %s",
		n,
		baseDir,
	)

	for _, iface := range n.cfg.Interfaces {

		var sb strings.Builder

		sb.WriteString("TYPE=Ethernet\n")

		sb.WriteString(
			fmt.Sprintf("DEVICE=%s\n", iface.Name),
		)

		sb.WriteString(
			fmt.Sprintf("NAME=%s\n", iface.Name),
		)

		if iface.MAC != "" {
			sb.WriteString(
				fmt.Sprintf(
					"HWADDR=%s\n",
					iface.MAC,
				),
			)
		}

		if iface.Enabled {
			sb.WriteString("ONBOOT=yes\n")
		} else {
			sb.WriteString("ONBOOT=no\n")
		}

		sb.WriteString("NM_CONTROLLED=no\n")

		if iface.MTU > 0 {
			sb.WriteString(
				fmt.Sprintf(
					"MTU=%d\n",
					iface.MTU,
				),
			)
		}

		if iface.DHCP {
			sb.WriteString("BOOTPROTO=dhcp\n")
		} else {
			sb.WriteString("BOOTPROTO=none\n")
		}

		sb.WriteString("ARPCHECK=no\n")

		//
		// IP地址
		//

		var (
			ipv4Index int
			ipv6List  []string
		)

		for _, ipcfg := range iface.IPAddr {

			ip, prefix, err := parseCIDR(
				ipcfg.Address,
			)
			if err != nil {

				logger.Warnf(
					"invalid ip %s: %v",
					ipcfg.Address,
					err,
				)

				continue
			}

			//
			// IPv6
			//

			if strings.Contains(ip, ":") {

				ipv6List = append(
					ipv6List,
					fmt.Sprintf(
						"%s/%d",
						ip,
						prefix,
					),
				)

				continue
			}

			//
			// IPv4
			//

			if ipv4Index == 0 {

				sb.WriteString(
					fmt.Sprintf(
						"IPADDR=%s\n",
						ip,
					),
				)

				sb.WriteString(
					fmt.Sprintf(
						"PREFIX=%d\n",
						prefix,
					),
				)

			} else {

				sb.WriteString(
					fmt.Sprintf(
						"IPADDR%d=%s\n",
						ipv4Index,
						ip,
					),
				)

				sb.WriteString(
					fmt.Sprintf(
						"PREFIX%d=%d\n",
						ipv4Index,
						prefix,
					),
				)
			}

			ipv4Index++
		}

		//
		// IPv6
		//

		if len(ipv6List) > 0 {

			sb.WriteString("IPV6INIT=yes\n")
			sb.WriteString("IPV6_AUTOCONF=no\n")

			sb.WriteString(
				fmt.Sprintf(
					"IPV6ADDR=%s\n",
					ipv6List[0],
				),
			)

			if len(ipv6List) > 1 {

				sb.WriteString(
					fmt.Sprintf(
						"IPV6ADDR_SECONDARIES=\"%s\"\n",
						strings.Join(
							ipv6List[1:],
							" ",
						),
					),
				)
			}
		}

		//
		// 默认网关
		//

		if iface.Gateway != "" {

			sb.WriteString("DEFROUTE=yes\n")

			if strings.Contains(
				iface.Gateway,
				":",
			) {

				sb.WriteString(
					fmt.Sprintf(
						"IPV6_DEFAULTGW=%s\n",
						iface.Gateway,
					),
				)

			} else {

				sb.WriteString(
					fmt.Sprintf(
						"GATEWAY=%s\n",
						iface.Gateway,
					),
				)
			}
		}

		//
		// DNS
		//

		dnsList := mergeDNS(
			iface.DNS,
			n.cfg.GlobalDNS,
		)

		if len(dnsList) > 0 {
			sb.WriteString("PEERDNS=no\n")
		}

		for i, dns := range dnsList {

			sb.WriteString(
				fmt.Sprintf(
					"DNS%d=%s\n",
					i+1,
					dns,
				),
			)
		}

		path := filepath.Join(
			baseDir,
			"ifcfg-"+iface.Name,
		)

		if err := backupIfExists(path); err != nil {
			return err
		}

		logger.Debugf(
			"ifcfgWriter: %s:\n%s",
			path,
			sb.String(),
		)

		if err := os.WriteFile(
			path,
			[]byte(sb.String()),
			0644,
		); err != nil {
			return err
		}

		//
		// route文件
		//

		if err := n.writeIfcfgRoutes(
			baseDir,
			iface,
		); err != nil {
			return err
		}
	}

	return nil
}

func (n *linuxNetworkInjector) writeIfcfgRoutes(
	baseDir string,
	iface InterfaceConfig,
) error {

	var (
		route4 strings.Builder
		route6 strings.Builder
	)

	for _, route := range n.cfg.Routes {

		if route.InterfaceMAC != "" &&
			!equalMAC(
				route.InterfaceMAC,
				iface.MAC,
			) {
			continue
		}

		line := route.Destination

		if route.Gateway != "" {
			line += " via " + route.Gateway
		}

		if route.Metric > 0 {
			line += fmt.Sprintf(
				" metric %d",
				route.Metric,
			)
		}

		line += "\n"

		if strings.Contains(
			route.Destination,
			":",
		) {
			route6.WriteString(line)
		} else {
			route4.WriteString(line)
		}
	}

	if route4.Len() > 0 {

		path := filepath.Join(
			baseDir,
			"route-"+iface.Name,
		)

		if err := backupIfExists(path); err != nil {
			return err
		}

		logger.Debugf(
			"routeWriter: %s:\n%s",
			path,
			route4.String(),
		)

		if err := os.WriteFile(
			path,
			[]byte(route4.String()),
			0644,
		); err != nil {
			return err
		}
	}

	if route6.Len() > 0 {

		path := filepath.Join(
			baseDir,
			"route6-"+iface.Name,
		)

		if err := backupIfExists(path); err != nil {
			return err
		}

		logger.Debugf(
			"route6Writer: %s:\n%s",
			path,
			route6.String(),
		)

		if err := os.WriteFile(
			path,
			[]byte(route6.String()),
			0644,
		); err != nil {
			return err
		}
	}

	return nil
}

func (n *linuxNetworkInjector) injectNetworkByNetplan() error {
	logger.Debugf("%s.injectNetworkByNetplan: ++", n)
	defer logger.Debugf("%s.injectNetworkByNetplan: --", n)

	if len(n.cfg.Interfaces) == 0 {
		return nil
	}

	ethernets := make(map[string]interface{})

	for _, iface := range n.cfg.Interfaces {

		name := iface.Name
		if name == "" {
			return errors.Errorf(
				"empty interface name for %s",
				iface.MAC,
			)
		}

		eth := map[string]interface{}{
			"match": map[string]interface{}{
				"macaddress": iface.MAC,
			},
			"set-name": name,
		}

		//
		// DHCP
		//

		if iface.DHCP {
			eth["dhcp4"] = true
			eth["dhcp6"] = false
		} else {
			eth["dhcp4"] = false
			eth["dhcp6"] = false
		}

		//
		// IPv6 RA
		//

		eth["accept-ra"] = false

		//
		// MTU
		//

		if iface.MTU > 0 {
			eth["mtu"] = iface.MTU
		}

		//
		// IP地址
		//

		if len(iface.IPAddr) > 0 {

			addresses := make(
				[]string,
				0,
				len(iface.IPAddr),
			)

			for _, ip := range iface.IPAddr {

				if ip.Address == "" {
					continue
				}

				addresses = append(
					addresses,
					ip.Address,
				)
			}

			if len(addresses) > 0 {
				eth["addresses"] = addresses
			}
		}

		//
		// DNS
		//

		dns := mergeDNS(
			iface.DNS,
			n.cfg.GlobalDNS,
		)

		if len(dns) > 0 {

			eth["nameservers"] = map[string]interface{}{
				"addresses": funk.UniqString(dns),
			}
		}

		//
		// 路由
		//

		var routes []map[string]interface{}

		// 默认路由
		if iface.Gateway != "" {

			routes = append(
				routes,
				map[string]interface{}{
					"to":  "default",
					"via": iface.Gateway,
				},
			)
		}

		// 静态路由
		for _, route := range n.cfg.Routes {

			if route.InterfaceMAC != "" &&
				!equalMAC(
					route.InterfaceMAC,
					iface.MAC,
				) {
				continue
			}

			r := map[string]interface{}{
				"to": route.Destination,
			}

			if route.Gateway != "" {
				r["via"] = route.Gateway
			}

			if route.Metric > 0 {
				r["metric"] = route.Metric
			}

			if route.Table > 0 {
				r["table"] = route.Table
			}

			routes = append(
				routes,
				r,
			)
		}

		if len(routes) > 0 {
			eth["routes"] = routes
		}

		ethernets[name] = eth
	}

	netplan := map[string]interface{}{
		"network": map[string]interface{}{
			"version":   2,
			"renderer":  "networkd",
			"ethernets": ethernets,
		},
	}

	data, err := yaml.Marshal(netplan)
	if err != nil {
		return err
	}

	file := filepath.Join(
		n.root,
		"etc/netplan/99-drfbtk.yaml",
	)

	if err = backupIfExists(file); err != nil {
		return err
	}

	logger.Debugf(
		"netplanWriter: %s:\n%s",
		file,
		string(data),
	)

	return os.WriteFile(
		file,
		data,
		0644,
	)
}

func (n *linuxNetworkInjector) injectNetworkByNetworkManager() error {
	logger.Debugf("%s.injectNetworkByNetworkManager: ++", n)
	defer logger.Debugf("%s.injectNetworkByNetworkManager: --", n)

	if len(n.cfg.Interfaces) == 0 {
		return nil
	}

	dir := filepath.Join(
		n.root,
		"etc/NetworkManager/system-connections",
	)

	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	for _, iface := range n.cfg.Interfaces {

		name := iface.Name
		if name == "" {
			return errors.Errorf(
				"empty interface name for %s",
				iface.MAC,
			)
		}

		var sb strings.Builder

		//
		// connection
		//

		sb.WriteString("[connection]\n")
		sb.WriteString(fmt.Sprintf("id=%s\n", name))
		sb.WriteString("type=ethernet\n")
		sb.WriteString(fmt.Sprintf("interface-name=%s\n", name))

		if iface.Enabled {
			sb.WriteString("autoconnect=true\n")
		} else {
			sb.WriteString("autoconnect=false\n")
		}

		//
		// ethernet
		//

		sb.WriteString("\n[ethernet]\n")

		if iface.MAC != "" {
			sb.WriteString(
				fmt.Sprintf(
					"mac-address=%s\n",
					iface.MAC,
				),
			)
		}

		if iface.MTU > 0 {
			sb.WriteString(
				fmt.Sprintf(
					"mtu=%d\n",
					iface.MTU,
				),
			)
		}

		//
		// IPv4
		//

		var ipv4Addrs []string

		for _, ip := range iface.IPAddr {

			if strings.Contains(
				ip.Address,
				":",
			) {
				continue
			}

			ipv4Addrs = append(
				ipv4Addrs,
				ip.Address,
			)
		}

		sb.WriteString("\n[ipv4]\n")

		if iface.DHCP {

			sb.WriteString("method=auto\n")

		} else if len(ipv4Addrs) > 0 {

			sb.WriteString("method=manual\n")

			for i, addr := range ipv4Addrs {

				sb.WriteString(
					fmt.Sprintf(
						"address%d=%s\n",
						i+1,
						addr,
					),
				)
			}

			if iface.Gateway != "" &&
				!strings.Contains(
					iface.Gateway,
					":",
				) {

				sb.WriteString(
					fmt.Sprintf(
						"gateway=%s\n",
						iface.Gateway,
					),
				)
			}

		} else {

			sb.WriteString("method=disabled\n")
		}

		//
		// DNS
		//

		dnsList := mergeDNS(
			iface.DNS,
			n.cfg.GlobalDNS,
		)

		if len(dnsList) > 0 {

			sb.WriteString(
				fmt.Sprintf(
					"dns=%s;\n",
					strings.Join(
						dnsList,
						";",
					),
				),
			)

			sb.WriteString("ignore-auto-dns=true\n")
		}

		//
		// IPv4 Route
		//

		routeIndex := 1

		for _, route := range n.cfg.Routes {

			if route.InterfaceMAC != "" &&
				!equalMAC(
					route.InterfaceMAC,
					iface.MAC,
				) {
				continue
			}

			if strings.Contains(
				route.Destination,
				":",
			) {
				continue
			}

			var parts []string

			parts = append(
				parts,
				route.Destination,
			)

			if route.Gateway != "" {
				parts = append(
					parts,
					route.Gateway,
				)
			}

			if route.Metric > 0 {
				parts = append(
					parts,
					strconv.Itoa(route.Metric),
				)
			}

			sb.WriteString(
				fmt.Sprintf(
					"route%d=%s\n",
					routeIndex,
					strings.Join(
						parts,
						",",
					),
				),
			)

			routeIndex++
		}

		//
		// IPv6
		//

		var ipv6Addrs []string

		for _, ip := range iface.IPAddr {

			if !strings.Contains(
				ip.Address,
				":",
			) {
				continue
			}

			ipv6Addrs = append(
				ipv6Addrs,
				ip.Address,
			)
		}

		sb.WriteString("\n[ipv6]\n")

		if len(ipv6Addrs) > 0 {

			sb.WriteString("method=manual\n")
			sb.WriteString("addr-gen-mode=stable-privacy\n")

			for i, addr := range ipv6Addrs {

				sb.WriteString(
					fmt.Sprintf(
						"address%d=%s\n",
						i+1,
						addr,
					),
				)
			}

			if iface.Gateway != "" &&
				strings.Contains(
					iface.Gateway,
					":",
				) {

				sb.WriteString(
					fmt.Sprintf(
						"gateway=%s\n",
						iface.Gateway,
					),
				)
			}

		} else {

			sb.WriteString("method=ignore\n")
		}

		//
		// IPv6 Route
		//

		ipv6RouteIndex := 1

		for _, route := range n.cfg.Routes {

			if route.InterfaceMAC != "" &&
				!equalMAC(
					route.InterfaceMAC,
					iface.MAC,
				) {
				continue
			}

			if !strings.Contains(
				route.Destination,
				":",
			) {
				continue
			}

			var parts []string

			parts = append(
				parts,
				route.Destination,
			)

			if route.Gateway != "" {
				parts = append(
					parts,
					route.Gateway,
				)
			}

			if route.Metric > 0 {
				parts = append(
					parts,
					strconv.Itoa(route.Metric),
				)
			}

			sb.WriteString(
				fmt.Sprintf(
					"route%d=%s\n",
					ipv6RouteIndex,
					strings.Join(
						parts,
						",",
					),
				),
			)

			ipv6RouteIndex++
		}

		file := filepath.Join(
			dir,
			name+".nmconnection",
		)

		if err := backupIfExists(file); err != nil {
			return err
		}

		logger.Debugf(
			"nmWriter: %s:\n%s",
			file,
			sb.String(),
		)

		if err := os.WriteFile(
			file,
			[]byte(sb.String()),
			0600,
		); err != nil {
			return err
		}
	}

	return nil
}

func (n *linuxNetworkInjector) injectNetworkByInterfaces() error {
	logger.Debugf("%s.injectNetworkByInterfaces: ++", n)
	defer logger.Debugf("%s.injectNetworkByInterfaces: --", n)

	if len(n.cfg.Interfaces) == 0 {
		return nil
	}

	dir := filepath.Join(
		n.root,
		"etc/network/interfaces.d",
	)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	mainFile := filepath.Join(
		n.root,
		"etc/network/interfaces",
	)

	if !extend.IsExisted(mainFile) {

		_ = os.WriteFile(
			mainFile,
			[]byte(
				"source /etc/network/interfaces.d/*\n",
			),
			0644,
		)
	}

	for _, iface := range n.cfg.Interfaces {

		var sb strings.Builder

		if iface.Enabled {
			sb.WriteString(
				fmt.Sprintf(
					"auto %s\n\n",
					iface.Name,
				),
			)
		}

		//
		// IPv4
		//

		var ipv4Addrs []string
		var ipv6Addrs []string

		for _, ip := range iface.IPAddr {

			if strings.Contains(
				ip.Address,
				":",
			) {
				ipv6Addrs = append(
					ipv6Addrs,
					ip.Address,
				)
			} else {
				ipv4Addrs = append(
					ipv4Addrs,
					ip.Address,
				)
			}
		}

		if iface.DHCP {

			sb.WriteString(
				fmt.Sprintf(
					"iface %s inet dhcp\n",
					iface.Name,
				),
			)

		} else {

			sb.WriteString(
				fmt.Sprintf(
					"iface %s inet static\n",
					iface.Name,
				),
			)

			if len(ipv4Addrs) > 0 {

				sb.WriteString(
					fmt.Sprintf(
						"    address %s\n",
						ipv4Addrs[0],
					),
				)

				for _, addr := range ipv4Addrs[1:] {

					sb.WriteString(
						fmt.Sprintf(
							"    up ip addr add %s dev %s\n",
							addr,
							iface.Name,
						),
					)
				}
			}

			if iface.Gateway != "" &&
				!strings.Contains(
					iface.Gateway,
					":",
				) {

				sb.WriteString(
					fmt.Sprintf(
						"    gateway %s\n",
						iface.Gateway,
					),
				)
			}
		}

		//
		// DNS
		//

		dnsList := mergeDNS(
			iface.DNS,
			n.cfg.GlobalDNS,
		)

		if len(dnsList) > 0 {

			sb.WriteString(
				fmt.Sprintf(
					"    dns-nameservers %s\n",
					strings.Join(
						dnsList,
						" ",
					),
				),
			)
		}

		//
		// IPv4 Route
		//

		for _, route := range n.cfg.Routes {

			if route.InterfaceMAC != "" &&
				!equalMAC(
					route.InterfaceMAC,
					iface.MAC,
				) {
				continue
			}

			if strings.Contains(
				route.Destination,
				":",
			) {
				continue
			}

			cmd := fmt.Sprintf(
				"    up ip route add %s",
				route.Destination,
			)

			if route.Gateway != "" {
				cmd += " via " + route.Gateway
			}

			if route.Metric > 0 {
				cmd += fmt.Sprintf(
					" metric %d",
					route.Metric,
				)
			}

			sb.WriteString(cmd + "\n")
		}

		//
		// IPv6
		//

		if len(ipv6Addrs) > 0 {

			sb.WriteString("\n")

			sb.WriteString(
				fmt.Sprintf(
					"iface %s inet6 static\n",
					iface.Name,
				),
			)

			sb.WriteString(
				fmt.Sprintf(
					"    address %s\n",
					ipv6Addrs[0],
				),
			)

			for _, addr := range ipv6Addrs[1:] {

				sb.WriteString(
					fmt.Sprintf(
						"    up ip -6 addr add %s dev %s\n",
						addr,
						iface.Name,
					),
				)
			}

			if iface.Gateway != "" &&
				strings.Contains(
					iface.Gateway,
					":",
				) {

				sb.WriteString(
					fmt.Sprintf(
						"    gateway %s\n",
						iface.Gateway,
					),
				)
			}

			for _, route := range n.cfg.Routes {

				if route.InterfaceMAC != "" &&
					!equalMAC(
						route.InterfaceMAC,
						iface.MAC,
					) {
					continue
				}

				if !strings.Contains(
					route.Destination,
					":",
				) {
					continue
				}

				cmd := fmt.Sprintf(
					"    up ip -6 route add %s",
					route.Destination,
				)

				if route.Gateway != "" {
					cmd += " via " + route.Gateway
				}

				if route.Metric > 0 {
					cmd += fmt.Sprintf(
						" metric %d",
						route.Metric,
					)
				}

				sb.WriteString(cmd + "\n")
			}
		}

		file := filepath.Join(
			dir,
			iface.Name,
		)

		if err := backupIfExists(file); err != nil {
			return err
		}

		logger.Debugf(
			"interfacesWriter: %s:\n%s",
			file,
			sb.String(),
		)

		if err := os.WriteFile(
			file,
			[]byte(sb.String()),
			0644,
		); err != nil {
			return err
		}
	}

	return nil
}

func (n *linuxNetworkInjector) injectNetworkBySuseWicked() error {
	logger.Debugf("%s.injectNetworkBySuseWicked: ++", n)
	defer logger.Debugf("%s.injectNetworkBySuseWicked: --", n)

	if len(n.cfg.Interfaces) == 0 {
		return nil
	}

	dir := filepath.Join(
		n.root,
		"etc/sysconfig/network",
	)

	for _, iface := range n.cfg.Interfaces {

		var sb strings.Builder

		if iface.DHCP {
			sb.WriteString("BOOTPROTO='dhcp'\n")
		} else {
			sb.WriteString("BOOTPROTO='static'\n")
		}

		if iface.Enabled {
			sb.WriteString("STARTMODE='auto'\n")
		} else {
			sb.WriteString("STARTMODE='manual'\n")
		}

		if iface.MTU > 0 {
			sb.WriteString(
				fmt.Sprintf(
					"MTU='%d'\n",
					iface.MTU,
				),
			)
		}

		var ipIndex int

		for _, ip := range iface.IPAddr {

			if ipIndex == 0 {

				sb.WriteString(
					fmt.Sprintf(
						"IPADDR='%s'\n",
						ip.Address,
					),
				)

			} else {

				sb.WriteString(
					fmt.Sprintf(
						"IPADDR_%d='%s'\n",
						ipIndex,
						ip.Address,
					),
				)
			}

			ipIndex++
		}

		if iface.Gateway != "" {

			sb.WriteString(
				fmt.Sprintf(
					"GATEWAY='%s'\n",
					iface.Gateway,
				),
			)
		}

		sb.WriteString(
			"CHECK_DUPLICATE_IP='no'\n",
		)

		file := filepath.Join(
			dir,
			"ifcfg-"+iface.Name,
		)

		if err := backupIfExists(file); err != nil {
			return err
		}

		logger.Debugf(
			"wickedWriter: %s:\n%s",
			file,
			sb.String(),
		)

		if err := os.WriteFile(
			file,
			[]byte(sb.String()),
			0644,
		); err != nil {
			return err
		}
	}

	if err := n.writeWickedRoutes(dir); err != nil {
		return err
	}

	if err := n.writeWickedDNS(); err != nil {
		return err
	}

	return nil
}

func (n *linuxNetworkInjector) writeWickedRoutes(
	dir string,
) error {

	var sb strings.Builder

	for _, route := range n.cfg.Routes {

		if route.InterfaceMAC == "" {
			continue
		}

		var ifaceName string

		for _, iface := range n.cfg.Interfaces {

			if equalMAC(
				iface.MAC,
				route.InterfaceMAC,
			) {
				ifaceName = iface.Name
				break
			}
		}

		if ifaceName == "" {
			continue
		}

		gw := "-"

		if route.Gateway != "" {
			gw = route.Gateway
		}

		sb.WriteString(
			fmt.Sprintf(
				"%s %s - %s\n",
				route.Destination,
				gw,
				ifaceName,
			),
		)
	}

	if sb.Len() == 0 {
		return nil
	}

	file := filepath.Join(
		dir,
		"routes",
	)

	if err := backupIfExists(file); err != nil {
		return err
	}

	logger.Debugf(
		"wickedRouteWriter: %s:\n%s",
		file,
		sb.String(),
	)

	return os.WriteFile(
		file,
		[]byte(sb.String()),
		0644,
	)
}

func (n *linuxNetworkInjector) writeWickedDNS() error {

	dnsList := make(
		[]string,
		0,
	)

	dnsList = append(
		dnsList,
		n.cfg.GlobalDNS...,
	)

	if len(dnsList) == 0 {
		return nil
	}

	file := filepath.Join(
		n.root,
		"etc/sysconfig/network/config",
	)

	content := fmt.Sprintf(
		"NETCONFIG_DNS_STATIC_SERVERS=\"%s\"\n",
		strings.Join(
			funk.UniqString(dnsList),
			" ",
		),
	)

	logger.Debugf(
		"wickedDNSWriter: %s:\n%s",
		file,
		content,
	)

	return os.WriteFile(
		file,
		[]byte(content),
		0644,
	)
}
