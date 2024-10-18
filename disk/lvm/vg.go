package lvm

import "fmt"

type VolumeGroup struct {
	Name       string
	UUID       string
	Pvs        []*PhysicalVolume
	Lvs        []*LogicalVolume
	Attr       int
	AttrStr    string
	ExtentSize int
	Size, Free int64
}

// VG attributes
const (
	VG_ATTR_WRITABLE   = 1 << iota
	VG_ATTR_READONLY   = 1 << iota
	VG_ATTR_RESIZABLE  = 1 << iota
	VG_ATTR_EXPORTED   = 1 << iota
	VG_ATTR_PARTIAL    = 1 << iota
	VG_ATTR_CONTIGUOUS = 1 << iota
	VG_ATTR_CLING      = 1 << iota
	VG_ATTR_NORMAL     = 1 << iota
	VG_ATTR_ANYWHERE   = 1 << iota
	VG_ATTR_CLUSTERED  = 1 << iota
	VG_ATTR_SHARED     = 1 << iota
)

func parseVgAttrs(attrStr string) (int, error) {
	attrVal := 0
	switch attrStr[5] {
	case 'c':
		attrVal += VG_ATTR_CLUSTERED
	case 's':
		attrVal += VG_ATTR_SHARED
	case '-':
	default:
		return -1, fmt.Errorf("invalid vg_attr[5]: %s", attrStr)
	}
	switch attrStr[4] {
	case 'c':
		attrVal += VG_ATTR_CONTIGUOUS
	case 'l':
		attrVal += VG_ATTR_CLING
	case 'n':
		attrVal += VG_ATTR_NORMAL
	case 'a':
		attrVal += VG_ATTR_ANYWHERE
	default:
		return -1, fmt.Errorf("invalid vg_attr[4]: %s", attrStr)
	}
	if attrStr[3] != '-' {
		attrVal += VG_ATTR_PARTIAL
	}
	if attrStr[2] != '-' {
		attrVal += VG_ATTR_EXPORTED
	}
	if attrStr[1] != '-' {
		attrVal += VG_ATTR_RESIZABLE
	}
	switch attrStr[0] {
	case 'w':
		attrVal += VG_ATTR_WRITABLE
	case 'r':
		attrVal += VG_ATTR_READONLY
	default:
		return -1, fmt.Errorf("invalid vg_attr[0]: %s", attrStr)
	}

	return attrVal, nil
}

func FindVg(name string) (*VolumeGroup, error) {
	vgs, err := Vgs(name)
	if err != nil {
		return nil, fmt.Errorf("findVg: %v", err)
	}

	return vgs[0], nil
}

func (v *VolumeGroup) Rename(newName string) error {
	var err error
	v, err = Vgrename(v.Name, newName)
	if err != nil {
		return err
	}
	return nil
}

func (v *VolumeGroup) Extend(pvs ...string) error {
	return Vgextend(v.Name, pvs...)
}

func (v *VolumeGroup) Reduce(pvs ...string) error {
	return Vgreduce(v.Name, pvs...)
}

func (v *VolumeGroup) Remove() error {
	return Vgremove(v.Name)
}

func (v *VolumeGroup) IsWritable() bool {
	return v.Attr&VG_ATTR_WRITABLE > 0
}

func (v *VolumeGroup) IsReadonly() bool {
	return v.Attr&VG_ATTR_READONLY > 0
}

func (v *VolumeGroup) IsResizable() bool {
	return v.Attr&VG_ATTR_RESIZABLE > 0
}

func (v *VolumeGroup) IsExported() bool {
	return v.Attr&VG_ATTR_EXPORTED > 0
}

func (v *VolumeGroup) IsPartial() bool {
	return v.Attr&VG_ATTR_PARTIAL > 0
}

func (v *VolumeGroup) IsContiguous() bool {
	return v.Attr&VG_ATTR_CONTIGUOUS > 0
}

func (v *VolumeGroup) IsCling() bool {
	return v.Attr&VG_ATTR_CLING > 0
}

func (v *VolumeGroup) IsNormal() bool {
	return v.Attr&VG_ATTR_NORMAL > 0
}

func (v *VolumeGroup) IsAnywhere() bool {
	return v.Attr&VG_ATTR_ANYWHERE > 0
}

func (v *VolumeGroup) IsClustered() bool {
	return v.Attr&VG_ATTR_CLUSTERED > 0
}

func (v *VolumeGroup) IsShared() bool {
	return v.Attr&VG_ATTR_SHARED > 0
}
