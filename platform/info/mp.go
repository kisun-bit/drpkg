package info

type MultipathDevice struct {
	Name   string    `json:"name"`
	Device string    `json:"device"`
	Size   int64     `json:"size"`
	Table  DiskTable `json:"table"`
	Slaves []string  `json:"slaves"`
}
