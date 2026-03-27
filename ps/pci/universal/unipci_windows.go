package universal

import (
	"regexp"

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

var (
	vendorRegex    = regexp.MustCompile(`VEN_([0-9A-Fa-f]+)`)
	deviceRegex    = regexp.MustCompile(`DEV_([0-9A-Fa-f]+)`)
	subsystemRegex = regexp.MustCompile(`SUBSYS_([0-9A-Fa-f]+)`) // Subsystem Vendor + Subsystem Device
	classRegex     = regexp.MustCompile(`CC_([0-9A-Fa-f]+)`)     // Base Class + SubClass + Program Interface
	revisionRegex  = regexp.MustCompile(`REV_([0-9A-Fa-f]+)`)
)

func listUniPci() ([]*UniPci, error) {

	//
	// classGUID取值:
	// https://learn.microsoft.com/zh-cn/windows-hardware/drivers/install/system-defined-device-setup-classes-available-to-vendors
	// 若要查阅获取所有设备的方法, 请参考：
	// https://learn.microsoft.com/zh-cn/windows/win32/api/setupapi/nf-setupapi-setupdigetclassdevsw
	//

	devInfo, err := windows.SetupDiGetClassDevsEx(nil, "", 0,
		windows.DIGCF_ALLCLASSES|windows.DIGCF_PRESENT, 0, "")
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
		ps = append(ps, p)
	}

	return ps, nil
}

func uniPciFromMsHardwareIds(hwIds []string) (p *UniPci, err error) {
	if len(hwIds) == 0 {
		return nil, errors.Errorf("hardware-id is empty")
	}

	p = new(UniPci)

	vendorStr := pciStringFromHardwareIds(hwIds, vendorRegex)
	p.vendorId, err = uint32FromString(vendorStr)
	if err != nil {
		return nil, err
	}

	deviceStr := pciStringFromHardwareIds(hwIds, deviceRegex)
	p.deviceId, err = uint32FromString(deviceStr)
	if err != nil {
		return nil, err
	}

	revStr := pciStringFromHardwareIds(hwIds, revisionRegex)
	p.revision, err = uint32FromString(revStr)
	if err != nil {
		return nil, err
	}

	subsystemStr := pciStringFromHardwareIds(hwIds, subsystemRegex)
	if len(subsystemStr) != 0 {
		if len(subsystemStr) != 8 {
			return nil, errors.Errorf("invalid subsystem length %d", len(subsystemStr))
		}
		subsystemVendorStr := subsystemStr[:4]
		p.subsystemVendorId, err = uint32FromString(subsystemVendorStr)
		if err != nil {
			return nil, err
		}
		subsystemDeviceStr := subsystemStr[4:8]
		p.subsystemDeviceId, err = uint32FromString(subsystemDeviceStr)
		if err != nil {
			return nil, err
		}
	}

	classStr := pciStringFromHardwareIds(hwIds, classRegex)
	if len(classStr) != 0 {
		if len(classStr) != 6 {
			return nil, errors.Errorf("invalid class length %d", len(classStr))
		}
		baseClassStr := classStr[:2]
		p.baseClass, err = uint32FromString(baseClassStr)
		if err != nil {
			return nil, err
		}
		subClassStr := classStr[2:4]
		p.subClass, err = uint32FromString(subClassStr)
		if err != nil {
			return nil, err
		}
		ifaceStr := classStr[4:6]
		p.programInterface, err = uint32FromString(ifaceStr)
		if err != nil {
			return nil, err
		}
	}

	return p, nil
}

func pciStringFromHardwareIds(ids []string, regex *regexp.Regexp) string {
	for _, id := range ids {
		match := regex.FindStringSubmatch(id)
		if len(match) > 1 {
			return match[1]
		}
	}
	return ""
}
