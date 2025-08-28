package info

import (
	"fmt"
	"net"
	"strings"

	"github.com/kisun-bit/drpkg/extend"
	wmi_ "github.com/yusufpapurcu/wmi"
	"golang.org/x/sys/windows/registry"
)

func QueryIFExtra(ifName string) IFExtra {
	ie := IFExtra{}
	neti, err := net.InterfaceByName(ifName)
	if err == nil {
		ie.Linked = neti.Flags&net.FlagUp != 0
	}
	ie.Physical = IsPhysicalIF(ifName)
	return ie
}

func SearchNetInterfaceGUID(name string) (guid string, ok bool) {
	networkCtl := `SYSTEM\CurrentControlSet\Control\Network`
	hardwareIDListKey, err := registry.OpenKey(registry.LOCAL_MACHINE, networkCtl, registry.READ)
	if err != nil {
		return "", false
	}
	defer hardwareIDListKey.Close()
	hardwareIDList, err := hardwareIDListKey.ReadSubKeyNames(-1)
	if err != nil {
		return "", false
	}
	for _, hid := range hardwareIDList {
		if !extend.IsWinGUIDFormat(hid) {
			continue
		}
		interfaceGUIDListPath := fmt.Sprintf("%s\\%s", networkCtl, hid)
		interfaceGUIDListKey, err := registry.OpenKey(registry.LOCAL_MACHINE, interfaceGUIDListPath, registry.READ)
		if err != nil {
			continue
		}
		interfaceGUIDList, err := interfaceGUIDListKey.ReadSubKeyNames(-1)
		interfaceGUIDListKey.Close()
		if err != nil {
			continue
		}
		for _, iid := range interfaceGUIDList {
			if !extend.IsWinGUIDFormat(iid) {
				continue
			}
			interfaceConnPath := fmt.Sprintf("%s\\%s\\%s\\Connection", networkCtl, hid, iid)
			interfaceConnKey, err := registry.OpenKey(registry.LOCAL_MACHINE, interfaceConnPath, registry.READ)
			if err != nil {
				continue
			}
			netConnName, _, err := interfaceConnKey.GetStringValue("Name")
			interfaceConnKey.Close()
			if err != nil {
				continue
			}
			if name == netConnName {
				return iid, true
			}
		}
	}
	return "", false
}

type Win32_NetworkAdapter struct {
	Name            string
	NetConnectionID string
	PhysicalAdapter bool
	Description     string
	PNPDeviceID     string
}

func IsPhysicalIF(name string) bool {
	var adapters []Win32_NetworkAdapter
	query := fmt.Sprintf("SELECT Name, NetConnectionID, PhysicalAdapter, Description, PNPDeviceID FROM Win32_NetworkAdapter WHERE NetConnectionID='%s'", name)
	err := wmi_.Query(query, &adapters)
	if err != nil {
		return false
	}
	if len(adapters) == 0 {
		return false
	}

	// 弃用此处不严谨的逻辑
	//for _, adapter := range adapters {
	//	if strings.EqualFold(adapter.NetConnectionID, name) && adapter.PhysicalAdapter {
	//		// 排除虚拟网卡
	//		if strings.Contains(adapter.Description, "vEthernet") || strings.Contains(adapter.Description, "Virtual") {
	//			return false
	//		}
	//		return true
	//	}
	//}

	// 实际环境中发现，启用调试模式后，会基于物理网卡生成一个虚拟适配器，如下：
	// PS C:\WINDOWS\system32> Get-NetAdapter | Select Name, PnPDeviceID, PermanentAddress
	//
	// Name                       PnPDeviceID                                                    PermanentAddress
	// ----                       -----------                                                    ----------------
	// vEthernet (Default Switch) ROOT\VMS_MP\0000                                               00155D60FED2
	// 以太网 2                    PCI\VEN_10EC&DEV_8168&SUBSYS_E0001458&REV_16\4&dc1a27d&0&00E0  18C04D43AEB8
	// 以太网(内核调试器)           ROOT\KDNIC\0000                                                18C04D43AEB8
	// 可以看出，Mac地址一模一样的，且`以太网 2`并不会出现在资源管理器的`网络`中，那么我们将`以太网(内核调试器)`视为物理网卡即可

	if strings.HasPrefix(adapters[0].PNPDeviceID, "PCI") || strings.HasPrefix(adapters[0].PNPDeviceID, "ROOT\\KDNIC") {
		return true
	}

	return false
}
