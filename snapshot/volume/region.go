package volume

import (
	"fmt"
	"io"
	"os"
)

type Region struct {
	uniqueID   string
	device     string
	startOff   int64
	endOff     int64
	handle     *io.SectionReader
	baseHandle *os.File
}

func (r *Region) ReadAt(b []byte, off int64) (int, error) {
	return r.handle.ReadAt(b, off)
}

func (r *Region) Close() error {
	return r.baseHandle.Close()
}

func (r *Region) Type() string {
	return DevRegion
}

func (r *Region) StartOffset() int64 {
	return r.startOff
}

func (r *Region) EndOffset() int64 {
	return r.endOff
}

func (r *Region) SnapshotPath() string {
	return r.device
}

func (r *Region) Create() error {
	return nil
}

func (r *Region) UniqueID() string {
	return r.uniqueID
}

func (r *Region) DevicePath() string {
	return r.device
}

func (r *Region) Repr() string {
	return fmt.Sprintf("RegionSnap(dev=%s,region=%v-%v)", r.device, r.startOff, r.endOff)
}

func (r *Region) CowFiles() []string {
	return nil
}

func (r *Region) CowFilesDev() string {
	return ""
}

func NewRegion(regionUniqueID, device string, start, end int64) (rac Reader, err error) {
	r := new(Region)
	r.uniqueID = regionUniqueID
	r.baseHandle, err = os.Open(device)
	r.device = device
	if err != nil {
		return nil, err
	}
	r.handle = io.NewSectionReader(r.baseHandle, start, end-start)
	r.startOff, r.endOff = start, end
	return r, nil
}
