package sysrepair

import (
	"strings"

	"github.com/pkg/errors"
	"golang.org/x/sys/windows"
)

func vmbusExisted() (bool, error) {
	devInfo, err := windows.SetupDiGetClassDevsEx(
		nil,
		"",
		0,
		windows.DIGCF_ALLCLASSES|windows.DIGCF_PRESENT,
		0,
		"",
	)
	if err != nil {
		return false, err
	}
	defer devInfo.Close()

	for i := 0; ; i++ {
		devInfoData, eEnum := devInfo.EnumDeviceInfo(i)
		if eEnum != nil {
			if errors.Is(eEnum, windows.ERROR_NO_MORE_ITEMS) {
				break
			}
			continue
		}
		hwIdVal, eHwIdVal := devInfo.DeviceRegistryProperty(devInfoData, windows.SPDRP_HARDWAREID)
		if eHwIdVal != nil {
			continue
		}
		for _, hwid := range hwIdVal.([]string) {
			if strings.HasPrefix(hwid, "VMBUS") {
				return true, nil
			}
		}
	}

	return false, nil
}
