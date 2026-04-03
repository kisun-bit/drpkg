package info

type MultipathDevice struct {
	Device string   `json:"device"`
	Size   int64    `json:"size"`
	Disks  []string `json:"disks"`
}
