package info

type LVM struct {
	Effective bool `json:"effective"`
	VGList    []VG `json:"vgList"`
}

type VG struct {
	Name         string   `json:"name"`
	Size         int      `json:"size"`
	PVDeviceList []string `json:"pvDeviceList"`
	LVList       []LV     `json:"lvList"`
}

type LV struct {
	Name     string    `json:"name"`
	Device   string    `json:"device"`
	Attr     string    `json:"attr"`
	Size     int64     `json:"size"`
	Segments []Segment `json:"segments"`
}
