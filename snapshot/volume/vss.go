package volume

import (
	"fmt"
	"github.com/kisun-bit/drpkg/snapshot/volume/vss"
	"github.com/pkg/errors"
	"os"
	"sync"
	"time"
)

var vssCreateOrDeleteMutex sync.Mutex

type VolumeShadowCopyDevice struct {
	uniqueID    string
	diskPath    string
	volumePath  string // 卷名, 例如: C:\.
	volumeStart int64
	volumeEnd   int64
	Handle      *os.File
	VssSnap     vss.VssSnapshot
}

func (v *VolumeShadowCopyDevice) Close() error {
	vssCreateOrDeleteMutex.Lock()
	defer vssCreateOrDeleteMutex.Unlock()
	if err := v.Handle.Close(); err != nil {
		return err
	}
	return v.VssSnap.Delete()
}

func (v *VolumeShadowCopyDevice) ReadAt(b []byte, off int64) (int, error) {
	return v.Handle.ReadAt(b, off)
}

func (v *VolumeShadowCopyDevice) Type() string {
	return DevVss
}

func (v *VolumeShadowCopyDevice) StartOffset() int64 {
	return v.volumeStart
}

func (v *VolumeShadowCopyDevice) EndOffset() int64 {
	return v.volumeEnd
}

func (v *VolumeShadowCopyDevice) SnapshotPath() string {
	return v.VssSnap.GetSnapshotDeviceObject()
}

func (v *VolumeShadowCopyDevice) Repr() string {
	return fmt.Sprintf("VssSnap(snap=%s,disk=%s,region=%v-%v)",
		v.VssSnap.GetSnapshotDeviceObject(), v.diskPath, v.volumeStart, v.volumeEnd)
}

func (v *VolumeShadowCopyDevice) Create() error {
	vssCreateOrDeleteMutex.Lock()
	defer vssCreateOrDeleteMutex.Unlock()
	if v.Handle != nil {
		return nil
	}
	var err error
	v.VssSnap, err = vss.NewVssSnapshot("ms", v.volumePath, 180*time.Second, nil, true, nil)
	if err != nil {
		return errors.Wrapf(err, "create vss for %v", v.volumePath)
	}
	v.Handle, err = os.Open(v.VssSnap.GetSnapshotDeviceObject())
	if err != nil {
		return errors.Wrapf(err, "open vss for %v", v.volumePath)
	}
	return nil
}

func (v *VolumeShadowCopyDevice) UniqueID() string {
	return v.uniqueID
}

func (v *VolumeShadowCopyDevice) DevicePath() string {
	return v.diskPath
}

func (v *VolumeShadowCopyDevice) CowFiles() []string {
	return nil
}

func (v *VolumeShadowCopyDevice) CowFilesDev() string {
	return ""
}

func NewVolumeShadowCopyDevice(volumeUniqueID, diskPath, volumePath string, volumeStartOff, volumeEndOff int64, lazyCreate bool) (rac Reader, err error) {
	vssd := new(VolumeShadowCopyDevice)
	vssd.uniqueID = volumeUniqueID
	vssd.volumePath = volumePath
	vssd.volumeStart = volumeStartOff
	vssd.volumeEnd = volumeEndOff
	vssd.diskPath = diskPath
	if lazyCreate {
		return vssd, nil
	}
	err = vssd.Create()
	return vssd, err
}
