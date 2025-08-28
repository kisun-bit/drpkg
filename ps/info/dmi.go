package info

type DmiInfo struct {
	BIOSVendor  string `json:"bios_vendor"`
	BIOSVersion string `json:"bios_version"`
	BIOSDate    string `json:"bios_date"`

	BaseBoardName         string `json:"baseboard_name"`
	BaseBoardVendor       string `json:"baseboard_vendor"`
	BaseBoardVersion      string `json:"baseboard_version"`
	BaseBoardSerialNumber string `json:"baseboard_serial_number"`

	SystemFamily  string `json:"system_family"`
	SystemName    string `json:"system_name"`
	SystemSerial  string `json:"system_serial"`
	SystemSku     string `json:"system_sku"`
	SystemUUID    string `json:"system_uuid"`
	SystemVersion string `json:"system_version"`
}

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
