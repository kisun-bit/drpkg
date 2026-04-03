package info

type MultipathDevice struct {
	Name   string   `json:"name"`
	Device string   `json:"device"`
	Size   int64    `json:"size"`
	Disks  []string `json:"disks"`
}
