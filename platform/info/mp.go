package info

type MultipathDevice struct {
	Name               string    `json:"name"`
	Device             string    `json:"device"`
	UUID               string    `json:"uuid"`
	Size               int64     `json:"size"`
	LogicalSectorSize  int       `json:"logicalSectorSize"`
	PhysicalSectorSize int       `json:"physicalSectorSize"`
	Table              DiskTable `json:"table"`
	Slaves             []string  `json:"slaves"`
}
