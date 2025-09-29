package info

import (
	wmi_ "github.com/yusufpapurcu/wmi"
)

type Win32_BIOS struct {
	Manufacturer      string
	SerialNumber      string
	SMBIOSBIOSVersion string
	ReleaseDate       string
}

type Win32_BaseBoard struct {
	Manufacturer string
	Product      string
	SerialNumber string
	Version      string
}

type Win32_ComputerSystemProduct struct {
	UUID              string
	IdentifyingNumber string
	Vendor            string
	Version           string
	SKUNumber         string
	Name              string
}

func QueryDmi() (DmiInfo, error) {
	var bios []Win32_BIOS
	var baseboard []Win32_BaseBoard
	var systemProduct []Win32_ComputerSystemProduct

	if err := wmi_.Query("SELECT Manufacturer, SerialNumber, SMBIOSBIOSVersion, ReleaseDate FROM Win32_BIOS", &bios); err != nil {
		return DmiInfo{}, err
	}
	if err := wmi_.Query("SELECT Manufacturer, Product, SerialNumber, Version FROM Win32_BaseBoard", &baseboard); err != nil {
		return DmiInfo{}, err
	}
	if err := wmi_.Query("SELECT GUID, IdentifyingNumber, Vendor, Version, SKUNumber, Name FROM Win32_ComputerSystemProduct", &systemProduct); err != nil {
		return DmiInfo{}, err
	}

	_fixVal := func(_val string) string {
		if _val == "Default string" {
			return ""
		}
		return _val
	}

	di := DmiInfo{}

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
