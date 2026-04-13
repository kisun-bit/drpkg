package info

type RaidDevice struct {
	Name       string   `json:"name"`
	Level      int      `json:"level"`
	Device     string   `json:"device"`
	Size       uint64   `json:"size"`
	SubDevices []string `json:"subDevices"`
}
