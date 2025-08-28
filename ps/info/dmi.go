package info

type DmiInfo struct {
	BIOSVendor  string `json:"biosVendor"`
	BIOSVersion string `json:"biosVersion"`
	BIOSDate    string `json:"bios_date"`

	BaseBoardName         string `json:"baseboardName"`
	BaseBoardVendor       string `json:"baseboardVendor"`
	BaseBoardVersion      string `json:"baseboardVersion"`
	BaseBoardSerialNumber string `json:"baseboardSerialNumber"`

	SystemFamily  string `json:"systemFamily"`
	SystemName    string `json:"systemName"`
	SystemSerial  string `json:"systemSerial"`
	SystemSku     string `json:"systemSKU"`
	SystemUUID    string `json:"systemUuid"`
	SystemVersion string `json:"systemVersion"`
}
