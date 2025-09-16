package windows

import (
	"errors"
	"fmt"
	"github.com/kisun-bit/drpkg/ps/pci/ids"
	"golang.org/x/sys/windows"
	"regexp"
	"strconv"
	"strings"
)

func (p *PCI) InstallDriver() bool {
	return p.Service != ""
}

func (hd *PCI) String() string {
	hid := fmt.Sprintf("srv(%s)", hd.Service)
	if len(hd.HardwareIDs) > 0 {
		hid = hd.HardwareIDs[0]
	}
	return fmt.Sprintf("`%s` ~~ [%s]", hid, hd.FriendlyName)
}

func (hd *PCI) IsDriverMissed() bool {
	return hd.Status&windows.DN_HAS_PROBLEM != 0 && hd.Problem == 0x1C
}

func ListPCI(filters ...Filter) (hcs []*PCI, err error) {
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
		hd := PCI{}
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
			if guid, eConvert := GUIDFromBytes(value.([]uint8)); eConvert == nil {
				hd.BusClassGUID = guid.String()
			}
		}
		if e := windows.CM_Get_DevNode_Status(&hd.Status, &hd.Problem, devInfoData.DevInst, 0); e != nil {
			continue
		}
		if hd.BusClassGUID != GUID_BUS_TYPE_PCI {
			continue
		}

		if value, e := readFromHardwareIDs(hd.HardwareIDs, vendorRegex); e != nil {
			return nil, e
		} else {
			hd.Vendor = uint16(value)
		}
		if value, e := readFromHardwareIDs(hd.HardwareIDs, deviceRegex); e != nil {
			return nil, e
		} else {
			hd.Device = uint16(value)
		}
		if value, e := readFromHardwareIDs(hd.HardwareIDs, classRegex); e != nil {
			return nil, e
		} else {
			hd.Class = value
		}

		hd.VendorName, hd.DeviceName = fmt.Sprintf("%04x", hd.Vendor), fmt.Sprintf("%04x", hd.Device)
		hd.ClassDetailName = "ClassUnknown"
		if nm, ok := ids.ClassDetailNames[hd.Class]; ok {
			hd.ClassDetailName = nm
		}

		ignored := false
		for _, f := range filters {
			if !f(&hd) {
				ignored = true
				continue
			}
		}
		if ignored {
			continue
		}

		hcs = append(hcs, &hd)
	}
	return hcs, nil
}

var (
	vendorRegex = regexp.MustCompile(`VEN_([0-9A-Fa-f]+)`)
	deviceRegex = regexp.MustCompile(`DEV_([0-9A-Fa-f]+)`)
	classRegex  = regexp.MustCompile(`CC_([0-9A-Fa-f]+)`)
)

func readFromHardwareIDs(ids []string, regex *regexp.Regexp) (uint32, error) {
	for _, id := range ids {
		match := regex.FindStringSubmatch(id)
		if len(match) > 1 {
			r, e := strconv.ParseUint(
				trimLeadingZeros(match[1]),
				16,
				64)
			return uint32(r), e
		}
	}
	return 0, fmt.Errorf("no valid id found by %s", regex.String())
}

func trimLeadingZeros(s string) string {
	result := strings.TrimLeft(s, "0")
	if result == "" {
		return "0"
	}
	return result
}

func GUIDFromBytes(arr []uint8) (g windows.GUID, err error) {
	if len(arr) != 16 {
		return windows.GUID{}, errors.New("length of bytes array of GUID is insufficient")
	}
	g.Data1 = uint32(arr[3])<<24 | uint32(arr[2])<<16 | uint32(arr[1])<<8 | uint32(arr[0])
	g.Data2 = uint16(arr[5])<<8 | uint16(arr[4])
	g.Data3 = uint16(arr[7])<<8 | uint16(arr[6])
	copy(g.Data4[:], arr[8:])
	return g, nil
}
