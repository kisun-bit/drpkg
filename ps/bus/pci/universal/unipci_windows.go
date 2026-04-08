package universal

import (
	"github.com/kisun-bit/drpkg/extend"
	"github.com/pkg/errors"
	"golang.org/x/sys/windows"
)

//
// 举例真实的硬件Id，
//
// 示例1：
// ---- 硬件Id ----
// PCI\VEN_10EC&DEV_8168&SUBSYS_E0001458&REV_16
// PCI\VEN_10EC&DEV_8168&SUBSYS_E0001458
// PCI\VEN_10EC&DEV_8168&CC_020000
// PCI\VEN_10EC&DEV_8168&CC_0200
// ---- 兼容Id ----
// PCI\VEN_10EC&DEV_8168&REV_16
// PCI\VEN_10EC&DEV_8168
// PCI\VEN_10EC&CC_020000
// PCI\VEN_10EC&CC_0200
// PCI\VEN_10EC
// PCI\CC_020000
// PCI\CC_0200
//
// 示例2:
// ---- 硬件Id ----
// PCI\VEN_8086&DEV_A282&SUBSYS_B0051458&REV_00
// PCI\VEN_8086&DEV_A282&SUBSYS_B0051458
// PCI\VEN_8086&DEV_A282&CC_010601
// PCI\VEN_8086&DEV_A282&CC_0106
// ---- 兼容Id ----
// PCI\VEN_8086&DEV_A282&REV_00
// PCI\VEN_8086&DEV_A282
// PCI\VEN_8086&CC_010601
// PCI\VEN_8086&CC_0106
// PCI\VEN_8086
// PCI\CC_010601
// PCI\CC_0106
//

// 参考：https://github.com/ynkdir/py-win32more/blob/457415c9da492d2f4dab49c63fd5ffc9f93e57fb/win32more/Windows/Win32/Devices/DeviceAndDriverInstallation/__init__.py#L991
const GUID_BUS_TYPE_PCI = "{C8EBDFB0-B510-11D0-80E5-00A0C92542E3}"

func listUniPci() ([]*UniPci, error) {

	//
	// classGUID取值:
	// https://learn.microsoft.com/zh-cn/windows-hardware/drivers/install/system-defined-device-setup-classes-available-to-vendors
	// 若要查阅获取所有设备的方法, 请参考：
	// https://learn.microsoft.com/zh-cn/windows/win32/api/setupapi/nf-setupapi-setupdigetclassdevsw
	//

	devInfo, err := windows.SetupDiGetClassDevsEx(
		nil,
		"",
		0,
		windows.DIGCF_ALLCLASSES|windows.DIGCF_PRESENT,
		0,
		"",
	)
	if err != nil {
		return nil, err
	}
	defer devInfo.Close()

	ps := make([]*UniPci, 0)

	for i := 0; ; i++ {
		devInfoData, eEnum := devInfo.EnumDeviceInfo(i)
		if eEnum != nil {
			if errors.Is(eEnum, windows.ERROR_NO_MORE_ITEMS) {
				break
			}
			continue
		}
		busTypeVal, eBusType := devInfo.DeviceRegistryProperty(devInfoData, windows.SPDRP_BUSTYPEGUID)
		if eBusType != nil {
			continue
		}
		busTypeGuid, eConvert := extend.MsGuidFromBytes(busTypeVal.([]uint8))
		if eConvert != nil {
			continue
		}
		if busTypeGuid.String() != GUID_BUS_TYPE_PCI {
			continue
		}
		hwIdVal, eHwIdVal := devInfo.DeviceRegistryProperty(devInfoData, windows.SPDRP_HARDWAREID)
		if eHwIdVal != nil {
			continue
		}
		p, ePci := uniPciFromMsHardwareIds(hwIdVal.([]string))
		if ePci != nil {
			return nil, ePci
		}
		ps = appendUniPci(ps, p)
	}

	return ps, nil
}
