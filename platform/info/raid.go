package info

type RaidDevice struct {
	Name   string    `json:"name"`
	Level  int       `json:"level"`
	Device string    `json:"device"`
	Size   int64     `json:"size"`
	Table  DiskTable `json:"table"`
	Slaves []string  `json:"slaves"`
}
