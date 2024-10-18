package volume

import (
	"fmt"
	"os"
)

type RAW struct {
	uniqueID string
	device   string
	handle   *os.File
}

func (r *RAW) ReadAt(b []byte, off int64) (int, error) {
	return r.handle.ReadAt(b, off)
}

func (r *RAW) Close() error {
	return r.handle.Close()
}

func (r *RAW) Type() string {
	return DevRAW
}

func (r *RAW) StartOffset() int64 {
	return 0
}

func (r *RAW) EndOffset() int64 {
	return 0
}

func (r *RAW) SnapshotPath() string {
	return r.device
}

func (r *RAW) Create() error {
	return nil
}

func (r *RAW) UniqueID() string {
	return r.uniqueID
}

func (r *RAW) DevicePath() string {
	return r.device
}

func (r *RAW) Repr() string {
	return fmt.Sprintf("RawSnap(dev=%s)", r.device)
}

func (r *RAW) CowFiles() []string {
	return nil
}

func (r *RAW) CowFilesDev() string {
	return ""
}

func NewRAW(deviceUniqueID, device string) (raw Reader, err error) {
	r := new(RAW)
	r.device = device
	r.uniqueID = deviceUniqueID
	r.handle, err = os.Open(device)
	return r, err
}
