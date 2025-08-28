package info

import (
	wmi_ "github.com/yusufpapurcu/wmi"
)

func QueryDmi() (*DmiInfo, error) {
	var bios []Win32_BIOS
	var baseboard []Win32_BaseBoard
	var systemProduct []Win32_ComputerSystemProduct

	if err := wmi_.Query("SELECT Manufacturer, SerialNumber, SMBIOSBIOSVersion, ReleaseDate FROM Win32_BIOS", &bios); err != nil {
		return nil, err
	}
	if err := wmi_.Query("SELECT Manufacturer, Product, SerialNumber, Version FROM Win32_BaseBoard", &baseboard); err != nil {
		return nil, err
	}
	if err := wmi_.Query("SELECT UUID, IdentifyingNumber, Vendor, Version, SKUNumber, Name FROM Win32_ComputerSystemProduct", &systemProduct); err != nil {
		return nil, err
	}

	_fixVal := func(_val string) string {
		if _val == "Default string" {
			return ""
		}
		return _val
	}

	di := new(DmiInfo)
	if len(bios) > 0 {
		di.BIOSVendor = _fixVal(bios[0].Manufacturer)
		di.BIOSVersion = _fixVal(bios[0].SMBIOSBIOSVersion)
		di.BIOSDate = _fixVal(bios[0].ReleaseDate)
	}

	if len(baseboard) > 0 {
		di.BaseBoardVendor = _fixVal(baseboard[0].Manufacturer)
		di.BaseBoardVersion = _fixVal(baseboard[0].Version)
		di.BaseBoardSerialNumber = _fixVal(baseboard[0].SerialNumber)
		di.BaseBoardName = _fixVal(baseboard[0].Product)
	}

	if len(systemProduct) > 0 {
		di.SystemName = _fixVal(systemProduct[0].Name)
		di.SystemVersion = _fixVal(systemProduct[0].Version)
		di.SystemUUID = _fixVal(systemProduct[0].UUID)
		di.SystemSku = _fixVal(systemProduct[0].SKUNumber)
		di.SystemSerial = _fixVal(systemProduct[0].IdentifyingNumber)
	}

	return di, nil
}
