package x2xcore

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
	"gopkg.in/yaml.v3"
)

const (
	systemdNetworkDir = "etc/systemd/network"
	udevRulesDir      = "etc/udev/rules.d"

	drfbtkLinkPrefix = "10-drfbtk-"
	drfbtkUdevRule   = "80-drfbtk-net.rules"
)

var cleanupDirs = []string{
	"etc/systemd/network",
	"etc/udev/rules.d",
	"lib/udev/rules.d",
	"etc/netplan",
	"etc/sysconfig/network-scripts",
	"etc/NetworkManager/system-connections",
	"etc/network/interfaces.d",
	"etc/sysconfig/network",
}

type netplanWriter struct{}
type nmWriter struct{}
type ifcfgWriter struct{}
type interfacesWriter struct{}
type wickedWriter struct{}

func NetworkInject(
	root string,
	cfg *NetworkConfig,
) error {

	logger.Debugf("LinuxNetworkInjector.Inject: ++")
	defer logger.Debugf("LinuxNetworkInjector.Inject: -")

	if cfg == nil {
		return nil
	}

	logger.Debugf(
		"LinuxNetworkInjector.Inject: NetworkConfig:\n%s",
		extend.Pretty(cfg),
	)

	macs := collectMACs(cfg)

	steps := []struct {
		name string
		fn   func() error
	}{
		{
			name: "cleanup old network config",
			fn: func() error {
				return renameAllFileIfContainsMac(
					macs,
					buildCleanupPaths(root),
				)
			},
		},
		{
			name: "disable persistent net rules",
			fn: func() error {
				return disableLegacyPersistentNetRules(root)
			},
		},
		{
			name: "generate rename rules",
			fn: func() error {
				return generateNetworkRenameRules(root, cfg)
			},
		},
		{
			name: "inject network config",
			fn: func() error {
				return injectNetworkConfig(root, cfg)
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

func collectMACs(cfg *NetworkConfig) []string {

	seen := make(map[string]struct{})
	macs := make([]string, 0, len(cfg.Interfaces))

	for _, nic := range cfg.Interfaces {

		mac := strings.TrimSpace(
			strings.ToLower(nic.MAC),
		)

		if mac == "" {
			continue
		}

		if _, ok := seen[mac]; ok {
			continue
		}

		seen[mac] = struct{}{}
		macs = append(macs, mac)
	}

	return macs
}

func buildCleanupPaths(root string) []string {

	paths := make([]string, 0, len(cleanupDirs))

	for _, dir := range cleanupDirs {
		paths = append(
			paths,
			filepath.Join(root, dir),
		)
	}

	return paths
}

func disableLegacyPersistentNetRules(
	root string,
) error {

	files := []string{
		"etc/udev/rules.d/70-persistent-net.rules",
		"lib/udev/rules.d/75-persistent-net-generator.rules",
	}

	for _, f := range files {

		src := filepath.Join(root, f)

		if _, err := os.Stat(src); err != nil {
			continue
		}

		dst := src + ".drfbtk.disabled"

		logger.Debugf("disableLegacyPersistentNetRules: moving %s to %s", src, dst)

		_ = os.Remove(dst)

		if err := os.Rename(src, dst); err != nil {
			return err
		}
	}

	return nil
}

func generateNetworkRenameRules(
	root string,
	cfg *NetworkConfig,
) error {

	if err := cleanupNetworkRenameRules(root); err != nil {
		return err
	}

	if err := generateLinkFiles(
		root,
		cfg,
	); err != nil {
		return err
	}

	if err := generateUdevRules(
		root,
		cfg,
	); err != nil {
		return err
	}

	return nil
}

func cleanupNetworkRenameRules(root string) error {

	//
	// *.link
	//

	pattern := filepath.Join(
		root,
		systemdNetworkDir,
		drfbtkLinkPrefix+"*.link",
	)

	files, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}

	for _, f := range files {
		_ = os.Remove(f)
		logger.Debugf("cleanupNetworkRenameRules: remove %s", f)
	}

	//
	// udev
	//

	_ = os.Remove(
		filepath.Join(
			root,
			udevRulesDir,
			drfbtkUdevRule,
		),
	)

	return nil
}

func renameAllFileIfContainsMac(macs []string, dirs []string) error {
	if len(macs) == 0 {
		return nil
	}

	for _, dir := range dirs {
		files, err := os.ReadDir(dir)
		if err != nil {
			logger.Debugf("renameAllFileIfContainsMac: ignore %s: %v", dir, err)
			continue
		}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			path := filepath.Join(dir, f.Name())
			fi, e := f.Info()
			if e != nil {
				logger.Warnf("renameAllFileIfContainsMac: file info %s failed: %v", path, err)
				continue
			}
			if fi.Size() > 1<<20 {
				logger.Debugf("renameAllFileIfContainsMac: file %s is too large", path)
				continue
			}

			data, err := os.ReadFile(path)
			if err != nil {
				logger.Warnf("renameAllFileIfContainsMac: read file %s failed: %v", path, err)
				continue
			}
			logger.Debugf("renameAllFileIfContainsMac: detecing %s", path)

			for _, mac := range macs {
				if bytes.Contains(data, []byte(strings.ToLower(mac))) ||
					bytes.Contains(data, []byte(strings.ToUpper(mac))) {

					//logger.Debugf("renameAllFileIfContainsMac: file %s is renamed. contains mac(%s)", path, mac)
					//if err = backupIfExists(path); err != nil {
					//	return err
					//}

					logger.Debugf("renameAllFileIfContainsMac: file %s is delete. contains mac(%s)", path, mac)
					if err = os.RemoveAll(path); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func generateLinkFiles(
	root string,
	cfg *NetworkConfig,
) error {

	dir := filepath.Join(
		root,
		systemdNetworkDir,
	)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	for _, nic := range cfg.Interfaces {

		if nic.Name == "" {
			continue
		}

		content := fmt.Sprintf(
			`[Match]
MACAddress=%s

[Link]
Name=%s
NamePolicy=
MACAddressPolicy=none
`,
			strings.ToLower(nic.MAC),
			nic.Name,
		)

		file := filepath.Join(
			dir,
			fmt.Sprintf(
				"%s%s.link",
				drfbtkLinkPrefix,
				nic.Name,
			),
		)

		logger.Debugf("generateLinkFiles: %s:\n%s", file, content)

		if err := os.WriteFile(
			file,
			[]byte(content),
			0644,
		); err != nil {
			return err
		}
	}

	return nil
}

func generateUdevRules(
	root string,
	cfg *NetworkConfig,
) error {

	dir := filepath.Join(
		root,
		udevRulesDir,
	)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	var buf bytes.Buffer

	buf.WriteString(
		"# generated by drfbtk\n",
	)

	for _, nic := range cfg.Interfaces {

		if nic.Name == "" {
			continue
		}

		_, _ = fmt.Fprintf(
			&buf,
			`SUBSYSTEM=="net", ACTION=="add", ATTR{address}=="%s", NAME="%s"`+"\n",
			strings.ToLower(nic.MAC),
			nic.Name,
		)
	}

	logger.Debugf("generateUdevRules: %s:\n%s", udevRulesDir, buf.String())

	return os.WriteFile(
		filepath.Join(
			dir,
			drfbtkUdevRule,
		),
		buf.Bytes(),
		0644,
	)
}

func detectNetworkBackend(root string) NetworkBackend {

	// Ubuntu 18+
	if extend.IsExisted(
		filepath.Join(root, "etc/netplan"),
	) {
		return BackendNetplan
	}

	// Debian
	if extend.IsExisted(
		filepath.Join(root, "etc/network/interfaces"),
	) {
		return BackendInterfaces
	}

	// SUSE
	if extend.IsExisted(
		filepath.Join(root, "etc/sysconfig/network"),
	) &&
		extend.IsExisted(
			filepath.Join(root, "usr/sbin/wicked"),
		) {
		return BackendWicked
	}

	if hasNMConnection(root) {
		return BackendNMKeyfile
	}

	if hasIfcfg(root) {
		return BackendIfcfg
	}

	return BackendUnknown
}

func hasNMConnection(root string) bool {
	dir := filepath.Join(
		root,
		"etc/NetworkManager/system-connections",
	)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}

	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".nmconnection") {
			return true
		}
	}

	return false
}

func hasIfcfg(root string) bool {
	dir := filepath.Join(
		root,
		"etc/sysconfig/network-scripts",
	)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}

	for _, e := range entries {

		name := e.Name()

		if !strings.HasPrefix(name, "ifcfg-") {
			continue
		}

		//if name == "ifcfg-lo" {
		//	continue
		//}

		return true
	}

	return false
}

func injectNetworkConfig(
	root string,
	cfg *NetworkConfig,
) error {

	if cfg == nil || len(cfg.Interfaces) == 0 {
		return nil
	}

	backend := detectNetworkBackend(root)
	logger.Debugf("injectNetworkConfig: backend: %s", backend)

	switch backend {

	case BackendNetplan:
		return (&netplanWriter{}).Write(root, cfg)

	case BackendNMKeyfile:
		return (&nmWriter{}).Write(root, cfg)

	case BackendIfcfg:
		return (&ifcfgWriter{}).Write(root, cfg)

	case BackendInterfaces:
		return (&interfacesWriter{}).Write(root, cfg)

	case BackendWicked:
		return (&wickedWriter{}).Write(root, cfg)

	default:
		return errors.New("unsupported network backend")
	}
}

func (w *netplanWriter) Write(
	root string,
	cfg *NetworkConfig,
) error {

	ethernets := make(map[string]interface{})

	for _, iface := range cfg.Interfaces {

		name := iface.Name
		if name == "" {
			name = iface.MAC
		}

		// 基础配置
		eth := map[string]interface{}{
			"match": map[string]interface{}{
				"macaddress": iface.MAC,
			},
			"set-name": name,
		}

		// DHCP 控制，只要 DHCP=true 就启用 IPv4 DHCP
		if iface.DHCP {
			eth["dhcp4"] = true
		}

		// MTU
		if iface.MTU > 0 {
			eth["mtu"] = iface.MTU
		}

		// 静态地址
		if len(iface.IPAddr) > 0 {
			var addresses []string
			for _, ip := range iface.IPAddr {
				addresses = append(addresses, ip.Address)
			}
			eth["addresses"] = addresses
		}

		// DNS
		var dns []string
		dns = append(dns, iface.DNS...)
		dns = append(dns, cfg.GlobalDNS...)
		if len(dns) > 0 {
			eth["nameservers"] = map[string]interface{}{
				"addresses": funk.UniqString(dns),
			}
		}

		// 路由
		var routes []map[string]interface{}
		if iface.Gateway != "" {
			routes = append(routes, map[string]interface{}{
				"to":  "default",
				"via": iface.Gateway,
			})
		}

		for _, route := range cfg.Routes {
			if route.InterfaceMAC != "" && !equalMAC(route.InterfaceMAC, iface.MAC) {
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

			routes = append(routes, r)
		}

		if len(routes) > 0 {
			eth["routes"] = routes
		}

		ethernets[name] = eth
	}

	netplan := map[string]interface{}{
		"network": map[string]interface{}{
			"version":   2,
			"ethernets": ethernets,
		},
	}

	data, err := yaml.Marshal(netplan)
	if err != nil {
		return err
	}

	file := filepath.Join(root, "etc/netplan/99-drfbtk.yaml")
	if err = backupIfExists(file); err != nil {
		return err
	}

	logger.Debugf("netplanWriter: %s:\n%s", file, string(data))

	return os.WriteFile(file, data, 0644)
}

func (w *ifcfgWriter) Write(
	root string,
	cfg *NetworkConfig,
) error {

	baseDir := filepath.Join(
		root,
		"etc/sysconfig/network-scripts",
	)

	for _, iface := range cfg.Interfaces {

		var sb strings.Builder

		sb.WriteString(
			fmt.Sprintf(
				"DEVICE=%s\n",
				iface.Name,
			),
		)

		sb.WriteString(
			fmt.Sprintf(
				"NAME=%s\n",
				iface.Name,
			),
		)

		sb.WriteString(
			fmt.Sprintf(
				"HWADDR=%s\n",
				iface.MAC,
			),
		)

		if iface.Enabled {
			sb.WriteString("ONBOOT=yes\n")
		} else {
			sb.WriteString("ONBOOT=no\n")
		}

		if iface.MTU > 0 {
			sb.WriteString(
				fmt.Sprintf(
					"MTU=%d\n",
					iface.MTU,
				),
			)
		}

		if iface.DHCP {

			sb.WriteString(
				"BOOTPROTO=dhcp\n",
			)

		} else {

			sb.WriteString(
				"BOOTPROTO=none\n",
			)

			if len(iface.IPAddr) > 0 {

				ip, mask, err := parseCIDR(
					iface.IPAddr[0].Address,
				)
				if err == nil {

					sb.WriteString(
						fmt.Sprintf(
							"IPADDR=%s\n",
							ip,
						),
					)

					sb.WriteString(
						fmt.Sprintf(
							"PREFIX=%d\n",
							mask,
						),
					)
				}
			}

			if iface.Gateway != "" {

				sb.WriteString(
					fmt.Sprintf(
						"GATEWAY=%s\n",
						iface.Gateway,
					),
				)
			}
		}

		for i, dns := range iface.DNS {

			sb.WriteString(
				fmt.Sprintf(
					"DNS%d=%s\n",
					i+1,
					dns,
				),
			)
		}

		sb.WriteString("ARPCHECK=no")

		path := filepath.Join(
			baseDir,
			"ifcfg-"+iface.Name,
		)
		if err := backupIfExists(path); err != nil {
			return err
		}

		logger.Debugf("ifcfgWriter: %s:\n%s", path, sb.String())

		err := os.WriteFile(
			path,
			[]byte(sb.String()),
			0644,
		)

		if err != nil {
			return err
		}
	}

	return nil
}

func (w *nmWriter) Write(
	root string,
	cfg *NetworkConfig,
) error {

	dir := filepath.Join(
		root,
		"etc/NetworkManager/system-connections",
	)

	if err := os.MkdirAll(
		dir,
		0700,
	); err != nil {
		return err
	}

	for _, iface := range cfg.Interfaces {

		name := iface.Name
		if name == "" {
			name = iface.MAC
		}

		var sb strings.Builder

		//
		// connection
		//

		sb.WriteString("[connection]\n")

		sb.WriteString(
			fmt.Sprintf(
				"id=%s\n",
				name,
			),
		)

		sb.WriteString("type=ethernet\n")

		sb.WriteString(
			fmt.Sprintf(
				"interface-name=%s\n",
				name,
			),
		)

		if iface.Enabled {
			sb.WriteString("autoconnect=true\n")
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

			if iface.Gateway != "" {

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

		dnsList := mergeDNS(
			cfg.GlobalDNS,
			iface.DNS,
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

			for i, addr := range ipv6Addrs {

				sb.WriteString(
					fmt.Sprintf(
						"address%d=%s\n",
						i+1,
						addr,
					),
				)
			}

		} else {

			sb.WriteString("method=ignore\n")
		}

		file := filepath.Join(
			dir,
			name+".nmconnection",
		)

		if err := backupIfExists(file); err != nil {
			return err
		}

		logger.Debugf("nmWriter: %s:\n%s", file, sb.String())

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

func (w *interfacesWriter) Write(
	root string,
	cfg *NetworkConfig,
) error {

	dir := filepath.Join(
		root,
		"etc/network/interfaces.d",
	)

	if err := os.MkdirAll(
		dir,
		0755,
	); err != nil {
		return err
	}

	mainFile := filepath.Join(
		root,
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

	for _, iface := range cfg.Interfaces {

		var sb strings.Builder

		if iface.Enabled {

			sb.WriteString(
				fmt.Sprintf(
					"auto %s\n\n",
					iface.Name,
				),
			)
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

			for _, ip := range iface.IPAddr {

				sb.WriteString(
					fmt.Sprintf(
						"    address %s\n",
						ip.Address,
					),
				)
			}

			if iface.Gateway != "" {

				sb.WriteString(
					fmt.Sprintf(
						"    gateway %s\n",
						iface.Gateway,
					),
				)
			}
		}

		dnsList := mergeDNS(
			cfg.GlobalDNS,
			iface.DNS,
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

		file := filepath.Join(
			dir,
			iface.Name,
		)

		if err := backupIfExists(file); err != nil {
			return err
		}

		logger.Debugf("interfacesWriter: %s:\n%s", file, sb.String())

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

func (w *wickedWriter) Write(
	root string,
	cfg *NetworkConfig,
) error {

	dir := filepath.Join(
		root,
		"etc/sysconfig/network",
	)

	for _, iface := range cfg.Interfaces {

		var sb strings.Builder

		if iface.DHCP {

			sb.WriteString(
				"BOOTPROTO='dhcp'\n",
			)

		} else {

			sb.WriteString(
				"BOOTPROTO='static'\n",
			)
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

		if len(iface.IPAddr) > 0 {

			sb.WriteString(
				fmt.Sprintf(
					"IPADDR='%s'\n",
					iface.IPAddr[0].Address,
				),
			)
		}

		sb.WriteString(
			"CHECK_DUPLICATE_IP='no'\n")

		file := filepath.Join(
			dir,
			"ifcfg-"+iface.Name,
		)

		if err := backupIfExists(file); err != nil {
			return err
		}

		logger.Debugf("wickedWriter: %s:\n%s", file, sb.String())

		if err := os.WriteFile(
			file,
			[]byte(sb.String()),
			0644,
		); err != nil {
			return err
		}
	}

	return w.writeRoutes(
		root,
		cfg,
	)
}

func (w *wickedWriter) writeRoutes(
	root string,
	cfg *NetworkConfig,
) error {

	if len(cfg.Routes) == 0 {
		return nil
	}

	var sb strings.Builder

	for _, route := range cfg.Routes {

		dev := lookupInterfaceName(
			cfg,
			route.InterfaceMAC,
		)

		if dev == "" {
			continue
		}

		sb.WriteString(
			fmt.Sprintf(
				"%s %s - %s\n",
				route.Destination,
				route.Gateway,
				dev,
			),
		)
	}

	file := filepath.Join(
		root,
		"etc/sysconfig/network/routes",
	)
	logger.Debugf("wickedWriter.writeRoutes: %s:\n%s", file, sb.String())

	return os.WriteFile(
		file,
		[]byte(sb.String()),
		0644,
	)
}

func lookupInterfaceName(
	cfg *NetworkConfig,
	mac string,
) string {

	for _, nic := range cfg.Interfaces {

		if strings.EqualFold(
			nic.MAC,
			mac,
		) {
			return nic.Name
		}
	}

	return ""
}
