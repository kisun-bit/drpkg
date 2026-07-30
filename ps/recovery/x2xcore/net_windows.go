package x2xcore

import (
	"bytes"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/kisun-bit/drpkg/logger"
	"github.com/pkg/errors"
	"golang.org/x/sys/windows"
)

// IsElevated 判断当前进程是否以管理员权限运行。
// 建议在调用 Apply 之前先检查，避免 netsh 因权限不足而大量失败。
func IsElevated() bool {
	var sid *windows.SID
	err := windows.AllocateAndInitializeSid(
		&windows.SECURITY_NT_AUTHORITY,
		2,
		windows.SECURITY_BUILTIN_DOMAIN_RID,
		windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&sid)
	if err != nil {
		return false
	}
	defer windows.FreeSid(sid)

	token := windows.Token(0)
	member, err := token.IsMember(sid)
	if err != nil {
		return false
	}
	return member
}

// ---------------------------------------------------------------------------
// netsh 执行封装
// ---------------------------------------------------------------------------

// runNetsh 执行一条 netsh 命令并返回输出。
//
// 注意：中文版 Windows 下 netsh 的输出是本地代码页编码（GBK），本函数不做编码转换，
// 错误判断主要依赖进程退出码；如果你需要解析中文输出定位具体错误原因，建议自行加上
// golang.org/x/text/encoding/simplifiedchinese 转换后再匹配关键字。
func runNetsh(args ...string) (string, error) {
	cmd := exec.Command("netsh", args...)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	output := out.String() + errBuf.String()
	if err != nil {
		return output, errors.Errorf("netsh %s execution failed: %v, output: %s",
			strings.Join(args, " "), err, strings.TrimSpace(output))
	}
	logger.Debugf("runNetSh: `netsh %s`\n%s", strings.Join(args, " "), output)
	return output, nil
}

// ---------------------------------------------------------------------------
// MAC 匹配（纯 Go 实现，不依赖外部命令）
// ---------------------------------------------------------------------------

// normalizeMAC 把 "AA-BB-CC-DD-EE-FF" / "aa:bb:cc:dd:ee:ff" 等格式
// 统一为不带分隔符的小写字符串，便于比较。
func normalizeMAC(mac string) string {
	mac = strings.ToLower(mac)
	return strings.NewReplacer("-", "", ":", "", ".", "").Replace(mac)
}

// resolvedInterface 保存匹配到的网卡的运行时状态。
type resolvedInterface struct {
	CurrentName string // 当前系统中的接口名（改名后会更新）
	MAC         string
	Index       int
}

// findInterfaceByMAC 通过 MAC 在当前系统中找到对应网卡。
func findInterfaceByMAC(mac string) (*resolvedInterface, error) {
	target := normalizeMAC(mac)
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, errors.Errorf("failed to enumerate network interfaces: %v", err)
	}
	for _, ifi := range ifaces {
		if len(ifi.HardwareAddr) == 0 {
			continue
		}
		if normalizeMAC(ifi.HardwareAddr.String()) == target {
			return &resolvedInterface{
				CurrentName: ifi.Name,
				MAC:         ifi.HardwareAddr.String(),
				Index:       ifi.Index,
			}, nil
		}
	}
	return nil, errors.Errorf("network interface with MAC %s not found", mac)
}

// ---------------------------------------------------------------------------
// 单项配置动作
// ---------------------------------------------------------------------------

func renameInterface(ri *resolvedInterface, newName string) error {
	if newName == "" || newName == ri.CurrentName {
		return nil
	}
	if _, err := runNetsh("interface", "set", "interface",
		"name="+ri.CurrentName, "newname="+newName); err != nil {
		return errors.Errorf("failed to rename network interface %s -> %s: %v", ri.CurrentName, newName, err)
	}
	ri.CurrentName = newName
	return nil
}

func setAdminState(ri *resolvedInterface, enabled bool) error {
	state := "DISABLED"
	if enabled {
		state = "ENABLED"
	}
	_, err := runNetsh("interface", "set", "interface",
		"name="+ri.CurrentName, "admin="+state)
	return err
}

func setMTU(ri *resolvedInterface, mtu int, isIPv6 bool) error {
	proto := "ipv4"
	if isIPv6 {
		proto = "ipv6"
	}
	_, err := runNetsh("interface", proto, "set", "subinterface",
		ri.CurrentName, "mtu="+strconv.Itoa(mtu), "store=persistent")
	return err
}

// cidrToIPv4Mask 把 "192.168.1.10/24" 拆成 ip + 点分掩码。
func cidrToIPv4Mask(cidr string) (ip, mask string, err error) {
	parts := strings.SplitN(cidr, "/", 2)
	if len(parts) != 2 {
		return "", "", errors.Errorf("invalid CIDR: %s", cidr)
	}
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", "", err
	}
	m := ipnet.Mask
	if len(m) != 4 {
		return "", "", errors.Errorf("%s is not a valid IPv4 CIDR", cidr)
	}
	return parts[0], fmt.Sprintf("%d.%d.%d.%d", m[0], m[1], m[2], m[3]), nil
}

// setDHCP 让接口切换为 DHCP（IPv4 全等价；IPv6 为近似等价，见文件头说明）。
func setDHCP(ri *resolvedInterface) error {
	if _, err := runNetsh("interface", "ipv4", "set", "address",
		"name="+ri.CurrentName, "source=dhcp", "store=persistent"); err != nil {
		return errors.Errorf("failed to configure IPv4 DHCP: %v", err)
	}
	if _, err := runNetsh("interface", "ipv4", "set", "dnsservers",
		"name="+ri.CurrentName, "source=dhcp"); err != nil {
		return errors.Errorf("failed to configure IPv4 DNS (via DHCP): %v", err)
	}
	// IPv6：启用有状态地址 + 其他有状态配置，接近 DHCPv6 行为
	_, _ = runNetsh("interface", "ipv6", "set", "interface",
		ri.CurrentName, "managedaddress=enabled", "otherstateful=enabled")
	return nil
}

// setStaticIPs 配置静态地址（自动区分 IPv4/IPv6），并按需设置默认网关。
func setStaticIPs(ri *resolvedInterface, ipConfigs []IPConfig, gateway string) error {
	var v4Addrs, v6Addrs []string
	for _, ipc := range ipConfigs {
		if strings.Contains(ipc.Address, ":") {
			v6Addrs = append(v6Addrs, ipc.Address)
		} else {
			v4Addrs = append(v4Addrs, ipc.Address)
		}
	}

	// ---- IPv4 ----
	if len(v4Addrs) > 0 {
		// 幂等：先清空该接口现有的 IPv4 地址和网关（store=persistent 确保清的是
		// 持久配置，而不只是当前运行时状态）
		_, _ = runNetsh("interface", "ipv4", "delete", "address",
			"name="+ri.CurrentName, "addr=all", "gateway=all", "store=persistent")

		for i, addr := range v4Addrs {
			ip, mask, err := cidrToIPv4Mask(addr)
			if err != nil {
				return errors.Errorf("failed to parse IPv4 address %s: %v", addr, err)
			}
			if i == 0 {
				args := []string{"interface", "ipv4", "set", "address",
					"name=" + ri.CurrentName, "source=static",
					"addr=" + ip, "mask=" + mask}
				if gateway != "" && !strings.Contains(gateway, ":") {
					args = append(args, "gateway="+gateway, "gwmetric=1")
				}
				args = append(args, "store=persistent")
				if _, err := runNetsh(args...); err != nil {
					return errors.Errorf("failed to configure primary IPv4 address: %v", err)
				}
			} else {
				if _, err := runNetsh("interface", "ipv4", "add", "address",
					"name="+ri.CurrentName, "addr="+ip, "mask="+mask,
					"store=persistent"); err != nil {
					return errors.Errorf("failed to add secondary IPv4 address: %v", err)
				}
			}
		}
	}

	// ---- IPv6 ----
	for _, addr := range v6Addrs {
		if _, err := runNetsh("interface", "ipv6", "add", "address",
			"interface="+ri.CurrentName, "address="+addr, "store=persistent"); err != nil {
			low := strings.ToLower(err.Error())
			if !strings.Contains(low, "already exists") && !strings.Contains(err.Error(), "已经存在") {
				return errors.Errorf("failed to add IPv6 address %s: %v", addr, err)
			}
		}
	}

	// IPv6 默认网关通过路由方式设置（netsh 没有和 IPv4 set address 等价的 gateway 参数）
	if gateway != "" && strings.Contains(gateway, ":") {
		_, _ = runNetsh("interface", "ipv6", "delete", "route", "::/0",
			"interface="+ri.CurrentName)
		if _, err := runNetsh("interface", "ipv6", "add", "route", "::/0",
			"interface="+ri.CurrentName, "nexthop="+gateway, "metric=1",
			"store=persistent"); err != nil {
			return errors.Errorf("failed to configure IPv6 default gateway: %v", err)
		}
	}

	return nil
}

// setInterfaceDNS 设置接口级 DNS（自动区分 IPv4/IPv6）。
func setInterfaceDNS(ri *resolvedInterface, dnsList []string) error {
	var v4, v6 []string
	for _, d := range dnsList {
		if strings.Contains(d, ":") {
			v6 = append(v6, d)
		} else {
			v4 = append(v4, d)
		}
	}
	apply := func(proto string, list []string) error {
		for i, d := range list {
			if i == 0 {
				if _, err := runNetsh("interface", proto, "set", "dnsservers",
					"name="+ri.CurrentName, "source=static", "addr="+d,
					"register=primary", "validate=no"); err != nil {
					return err
				}
			} else {
				if _, err := runNetsh("interface", proto, "add", "dnsservers",
					"name="+ri.CurrentName, "addr="+d,
					"index="+strconv.Itoa(i+1), "validate=no"); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := apply("ipv4", v4); err != nil {
		return errors.Errorf("failed to configure IPv4 DNS: %v", err)
	}
	if err := apply("ipv6", v6); err != nil {
		return errors.Errorf("failed to configure IPv6 DNS: %v", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// 单接口编排
// ---------------------------------------------------------------------------

func applyInterface(cfg InterfaceConfig) (*resolvedInterface, error) {
	ri, err := findInterfaceByMAC(cfg.MAC)
	if err != nil {
		return nil, err
	}

	if err := renameInterface(ri, cfg.Name); err != nil {
		return ri, err
	}

	if !cfg.Enabled {
		// 禁用的网卡不再继续下发 IP/DNS 等配置
		return ri, setAdminState(ri, false)
	}
	if err := setAdminState(ri, true); err != nil {
		return ri, err
	}

	if cfg.MTU > 0 {
		if err := setMTU(ri, cfg.MTU, false); err != nil {
			return ri, errors.Errorf("failed to configure MTU: %v", err)
		}
		_ = setMTU(ri, cfg.MTU, true) // IPv6 MTU 失败不视为致命错误
	}

	if cfg.DHCP {
		if err := setDHCP(ri); err != nil {
			return ri, err
		}
	} else if len(cfg.IPAddr) > 0 {
		if err := setStaticIPs(ri, cfg.IPAddr, cfg.Gateway); err != nil {
			return ri, err
		}
	}

	if len(cfg.DNS) > 0 {
		if err := setInterfaceDNS(ri, cfg.DNS); err != nil {
			return ri, err
		}
	}

	return ri, nil
}

// ---------------------------------------------------------------------------
// 全局路由
// ---------------------------------------------------------------------------

func applyRoute(r RouteConfig, macToName map[string]string) error {
	if r.Destination == "" {
		return errors.Errorf("route destination is missing")
	}
	isV6 := strings.Contains(r.Destination, ":")
	proto := "ipv4"
	if isV6 {
		proto = "ipv6"
	}

	ifName := ""
	if r.InterfaceMAC != "" {
		if name, ok := macToName[normalizeMAC(r.InterfaceMAC)]; ok {
			ifName = name
		} else if ri, err := findInterfaceByMAC(r.InterfaceMAC); err == nil {
			ifName = ri.CurrentName
		} else {
			return errors.Errorf("route-bound network interface %s not found", r.InterfaceMAC)
		}
	}

	if r.Table != 0 {
		logger.Debugf("warning: route %s specifies table=%d, Windows native routing does not support multiple routing tables/policy routing, field ignored",
			r.Destination, r.Table)
	}

	// 幂等：先尝试删除同目标的旧路由（忽略错误），再添加
	delArgs := []string{"interface", proto, "delete", "route", r.Destination}
	if ifName != "" {
		delArgs = append(delArgs, "interface="+ifName)
	}
	_, _ = runNetsh(delArgs...)

	args := []string{"interface", proto, "add", "route", r.Destination}
	if ifName != "" {
		args = append(args, "interface="+ifName)
	}
	if r.Gateway != "" {
		args = append(args, "nexthop="+r.Gateway)
	}
	if r.Metric > 0 {
		args = append(args, "metric="+strconv.Itoa(r.Metric))
	}
	args = append(args, "store=persistent")

	_, err := runNetsh(args...)
	if err != nil {
		return errors.Errorf("failed to add route %s: %v", r.Destination, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// 顶层入口
// ---------------------------------------------------------------------------

// ApplyNetworkConfig 应用整份网络配置。单个接口/路由失败不会阻断其它项，
// 所有错误会在最后汇总返回；调用方可根据需要决定是否整体重试或回滚。
func ApplyNetworkConfig(cfg NetworkConfig) error {
	if err := waitNetworkInterfacesReady(cfg, 120*time.Second); err != nil {
		logger.Warnf("failed to wait for network interfaces ready: %v", err)
	}

	var errs []string
	macToName := make(map[string]string)

	for _, ic := range cfg.Interfaces {
		ri, err := applyInterface(ic)
		if err != nil {
			errs = append(errs, fmt.Sprintf("interface[mac=%s]: %v", ic.MAC, err))
			continue
		}
		if ri != nil {
			macToName[normalizeMAC(ic.MAC)] = ri.CurrentName
		}

		// 未单独配置 DNS 的启用接口，使用全局 DNS 兜底
		if ic.Enabled && len(ic.DNS) == 0 && len(cfg.GlobalDNS) > 0 && ri != nil {
			if err := setInterfaceDNS(ri, cfg.GlobalDNS); err != nil {
				errs = append(errs, fmt.Sprintf("interface[mac=%s] apply global DNS: %v", ic.MAC, err))
			}
		}
	}

	for _, r := range cfg.Routes {
		if err := applyRoute(r, macToName); err != nil {
			errs = append(errs, fmt.Sprintf("route[%s]: %v", r.Destination, err))
		}
	}

	if len(errs) > 0 {
		return errors.Errorf("encountered %d error(s) while applying network configuration:\n- %s",
			len(errs), strings.Join(errs, "\n- "))
	}
	return nil
}

func waitNetworkInterfacesReady(
	cfg NetworkConfig,
	timeout time.Duration,
) error {

	deadline := time.Now().Add(timeout)

	targetMACs := make(map[string]struct{})

	for _, ic := range cfg.Interfaces {
		if ic.Enabled {
			targetMACs[normalizeMAC(ic.MAC)] = struct{}{}
		}
	}

	for time.Now().Before(deadline) {

		ready := true

		interfaces, err := net.Interfaces()
		if err != nil {
			ready = false
		} else {

			found := make(map[string]bool)

			for _, nic := range interfaces {

				mac := normalizeMAC(string(nic.HardwareAddr))

				if _, ok := targetMACs[mac]; ok {

					// 网卡存在即可
					// 如果希望更严格，可以判断 OperStatus
					found[mac] = true
				}
			}

			for mac := range targetMACs {
				if !found[mac] {
					ready = false
					break
				}
			}
		}

		if ready {
			return nil
		}

		time.Sleep(2 * time.Second)
	}

	return errors.Errorf(
		"timed out waiting for Windows network device initialization (%s)",
		timeout)
}
