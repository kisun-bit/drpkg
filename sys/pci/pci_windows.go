package pci

import (
	"errors"
	"fmt"
	"github.com/kisun-bit/drpkg/util"
	"golang.org/x/sys/windows"
)

type HardwareConfigSpaceInfo struct {
	FriendlyName  string   // 显示名称.
	BusNumber     uint32   // 总线号, 例如PCI硬件的0x00002表示PCI 总线2.
	Address       uint32   // 地址, 如PCI硬件的0x70007即00070007, 表示PCI 设备 7、功能 7
	InstancePath  string   // 设备实例路径.
	HardwareIDs   []string // 硬件ID集合.
	CompatibleIDs []string // 兼容ID集合.
	BusClassGUID  string   // 总线类型GUID. 以GUID_BUS_TYPE开头的常量ID.
	Service       string   // 内核驱动服务名称, 一般为驱动文件名(除文件后缀).
	Status        uint32   // 硬件状态.
	Problem       uint32   // 状态问题代码.
}

func (hd *HardwareConfigSpaceInfo) Repr() string {
	hid := fmt.Sprintf("srv(%s)", hd.Service)
	if len(hd.HardwareIDs) > 0 {
		hid = hd.HardwareIDs[0]
	}
	return fmt.Sprintf("`%s` ~~ [%s]", hid, hd.FriendlyName)
}

// IsDriverMissed 若缺少/未安装驱动,则返回true.
func (hd *HardwareConfigSpaceInfo) IsDriverMissed() bool {
	return hd.BusClassGUID == GUID_BUS_TYPE_PCI && hd.Status&windows.DN_HAS_PROBLEM != 0 && hd.Problem == 0x1C
}

// ListLocalPCIHardware 列举本地所有的PCI设备.
//
// 说明:
// ----------------------------------------------------------------------------------
// 此方法并不会列举出无法获取硬件设备的节点状态的PCI硬件.
func ListLocalPCIHardware(filter func(_hd HardwareConfigSpaceInfo) bool) (hcs []HardwareConfigSpaceInfo, err error) {
	// classGUID取值:
	// https://learn.microsoft.com/zh-cn/windows-hardware/drivers/install/system-defined-device-setup-classes-available-to-vendors
	// 若要查阅获取所有设备的方法, 请参考：
	// https://learn.microsoft.com/zh-cn/windows/win32/api/setupapi/nf-setupapi-setupdigetclassdevsw
	devInfo, err := windows.SetupDiGetClassDevsEx(nil, "", 0,
		windows.DIGCF_ALLCLASSES|windows.DIGCF_PRESENT, 0, "")
	if err != nil {
		return nil, err
	}
	defer func(devInfo windows.DevInfo) {
		_ = devInfo.Close()
	}(devInfo)

	for i := 0; ; i++ {
		devInfoData, eEnum := devInfo.EnumDeviceInfo(i)
		if eEnum != nil {
			if errors.Is(eEnum, windows.ERROR_NO_MORE_ITEMS) {
				break
			}
			continue
		}
		hd := HardwareConfigSpaceInfo{}
		if value, e := devInfo.DeviceInstanceID(devInfoData); e == nil {
			hd.InstancePath = value
		}
		if value, e := devInfo.DeviceRegistryProperty(devInfoData, windows.SPDRP_DEVICEDESC); e == nil {
			hd.FriendlyName = value.(string)
		}
		if value, e := devInfo.DeviceRegistryProperty(devInfoData, windows.SPDRP_HARDWAREID); e == nil {
			hd.HardwareIDs = value.([]string)
		}
		if value, e := devInfo.DeviceRegistryProperty(devInfoData, windows.SPDRP_COMPATIBLEIDS); e == nil {
			hd.CompatibleIDs = value.([]string)
		}
		if value, e := devInfo.DeviceRegistryProperty(devInfoData, windows.SPDRP_ADDRESS); e == nil {
			hd.Address = value.(uint32)
		}
		if value, e := devInfo.DeviceRegistryProperty(devInfoData, windows.SPDRP_SERVICE); e == nil {
			hd.Service = value.(string)
		}
		if value, e := devInfo.DeviceRegistryProperty(devInfoData, windows.SPDRP_BUSNUMBER); e == nil {
			hd.BusNumber = value.(uint32)
		}
		if value, e := devInfo.DeviceRegistryProperty(devInfoData, windows.SPDRP_BUSTYPEGUID); e == nil {
			if guid, eConvert := util.GUIDFromBytes(value.([]uint8)); eConvert == nil {
				hd.BusClassGUID = guid.String()
			}
		}
		if e := windows.CM_Get_DevNode_Status(&hd.Status, &hd.Problem, devInfoData.DevInst, 0); e != nil {
			continue
		}
		if hd.BusClassGUID == GUID_BUS_TYPE_PCI {
			if (filter != nil && filter(hd)) || (filter == nil) {
				hcs = append(hcs, hd)
			}
		}
	}
	return hcs, nil
}
