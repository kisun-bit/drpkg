package info

import (
	"os"
	"strings"
)

func QueryDmi() (DmiInfo, error) {
	_readDmiVal := func(_name string) string {
		if _val, _e := os.ReadFile("/sys/class/dmi/id/" + _name); _e == nil {
			if string(_val) == "To be filled by O.E.M." {
				return ""
			}
			return strings.TrimSpace(string(_val))
		}
		return ""
	}

	di := DmiInfo{}
	di.BIOSVendor = _readDmiVal("bios_vendor")
	di.BIOSVersion = _readDmiVal("bios_version")
	di.BIOSDate = _readDmiVal("bios_date")

	di.BaseBoardVendor = _readDmiVal("board_vendor")
	di.BaseBoardVersion = _readDmiVal("board_version")
	di.BaseBoardSerialNumber = _readDmiVal("board_serial")
	di.BaseBoardName = _readDmiVal("board_name")

	di.SystemFamily = _readDmiVal("product_family")
	di.SystemName = _readDmiVal("product_name")
	di.SystemSerial = _readDmiVal("product_serial")
	di.SystemSku = _readDmiVal("product_sku")
	di.SystemUUID = _readDmiVal("product_uuid")
	di.SystemVersion = _readDmiVal("product_version")

	return di, nil
}
