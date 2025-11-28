package info

import "github.com/kisun-bit/drpkg/extend"

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
	Name     string           `json:"name"`
	Device   string           `json:"device"`
	Attr     string           `json:"attr"`
	Size     int64            `json:"size"`
	Segments []extend.Segment `json:"segments"`
}
