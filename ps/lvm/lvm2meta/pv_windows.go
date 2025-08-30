package lvm2meta

import (
	"github.com/pkg/errors"
	"io"
)

func NewPhysicalVolume(dev string) (*PhysicalVolume, error) {
	return nil, errors.New("NewPhysicalVolume is unsupported")
}

func (pv *PhysicalVolume) ReadBlock(offset uint64) ([]byte, error) {
	return nil, errors.New("ReadBlock is unsupported")
}

func (h PhysicalVolumeHeader) UUIDToString() string {
	return ""
}

func (h PhysicalVolumeHeader) String() string {
	return ""
}

func (ext PhysicalVolumeHeaderExtension) String() string {
	return ""
}

func (pv *PhysicalVolume) ReadHeaderExt(reader io.Reader) (PhysicalVolumeHeaderExtension, error) {
	return PhysicalVolumeHeaderExtension{}, nil
}

func (pv *PhysicalVolume) ReadHeader(reader io.Reader) (PhysicalVolumeHeader, error) {
	return PhysicalVolumeHeader{}, nil
}

func (pv *PhysicalVolume) ReadLabelHeader(reader io.Reader) (LabelHeader, error) {
	return LabelHeader{}, nil
}
