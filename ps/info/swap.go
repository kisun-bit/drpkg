package info

type LinuxSwap struct {
	Filename string `json:"filename"`
	Type     string `json:"type"`
	Size     int64  `json:"size"`
	Used     int64  `json:"used"`
	Priority int    `json:"priority"`
	Brief    string `json:"brief"`
	UUID     string `json:"uuid"`
	Label    string `json:"label"`
}
