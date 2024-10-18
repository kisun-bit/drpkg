//go:build linux

package qcow2

type QemuIOCacheMode uint8

type EffectBlock struct {
	Off     int64
	Payload []byte
}

type qemuBlock struct {
	Type    uint8
	Off     uint64
	Len     uint64 `struc:"sizeof=Payload"`
	Payload []byte
}

type qemuRequestBlock struct {
	Type uint8
	Off  uint64
	Len  uint64
}

type ImgGeneralInfo struct {
	VirtualSize         int64  `json:"virtual-size"`
	Filename            string `json:"filename"`
	ClusterSize         int    `json:"cluster-size"`
	Format              string `json:"format"`
	ActualSize          int64  `json:"actual-size"`
	BackingFilename     string `json:"backing-filename"`
	FullBackingFilename string `json:"full-backing-filename"`
	DirtyFlag           bool   `json:"dirty-flag"`
}

type QemuIOOption func(cfg *qemuExtCfg)

type qemuExtCfg struct {
	rwSerial bool
}

type EffectReaderOption func(cfg *effectReaderExtCfg)

type effectReaderExtCfg struct {
	check     bool
	rChSize   int
	bs        int
	readCores int
}

type qemuMapBlockInfo struct {
	DiskOffset, Length, MappedTo int64
	File                         string
}
