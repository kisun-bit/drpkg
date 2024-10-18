package volume

import "io"

const (
	SnapDriverVSS         = "vss"
	SnapDriverElastioSnap = "elastio-snap"
	SnapDriverDattobd     = "dattobd"
)

const (
	DevElastioOrDatto = "elastio_or_datto"
	DevRAW            = "raw"
	DevRegion         = "region"
	DevVss            = "vss"
)

var (
	LinuxValidSnapDrivers   = []string{SnapDriverElastioSnap, SnapDriverDattobd}
	WindowsValidSnapDrivers = []string{SnapDriverVSS}
)

type Reader interface {
	io.ReaderAt
	io.Closer
	Create() error
	StartOffset() int64
	EndOffset() int64
	Type() string
	DevicePath() string
	SnapshotPath() string
	Repr() string
	UniqueID() string
	CowFiles() []string
	CowFilesDev() string
}
