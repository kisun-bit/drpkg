package lvm

import (
	"fmt"
	"github.com/pkg/errors"
)

type PhysicalVolume struct {
	Path, VgName, PvFmt, UUID string
	Attr                      int
	AttrStr                   string
	Size, Free                int64
}

// PV attributes
const (
	PV_ATTR_MISSING     = 1 << iota
	PV_ATTR_EXPORTED    = 1 << iota
	PV_ATTR_DUPLICATE   = 1 << iota
	PV_ATTR_ALLOCATABLE = 1 << iota
	PV_ATTR_USED        = 1 << iota
)

func parsePvAttrs(attrStr string) (int, error) {
	attrVal := 0
	if attrStr[2] != '-' {
		attrVal += PV_ATTR_MISSING
	}
	if attrStr[1] != '-' {
		attrVal += PV_ATTR_EXPORTED
	}
	switch attrStr[0] {
	case 'd':
		attrVal += PV_ATTR_DUPLICATE
	case 'a':
		attrVal += PV_ATTR_ALLOCATABLE
	case 'u':
		attrVal += PV_ATTR_USED
	case '-':
	default:
		return -1, fmt.Errorf("invalid pv_attr: %s", attrStr)
	}

	return attrVal, nil
}

func FindPv(path string) (*PhysicalVolume, error) {
	pvs, err := Pvs(path)
	if err != nil {
		return nil, fmt.Errorf("findPv: %v", err)
	}
	if len(pvs) == 0 {
		return nil, errors.Errorf("pv %s not found", path)
	}
	return pvs[0], nil
}

func (p *PhysicalVolume) Remove() error {
	return Pvremove(p.Path)
}

func (p *PhysicalVolume) IsMissing() bool {
	return p.Attr&PV_ATTR_MISSING > 0
}

func (p *PhysicalVolume) IsExported() bool {
	return p.Attr&PV_ATTR_EXPORTED > 0
}

func (p *PhysicalVolume) IsDuplicate() bool {
	return p.Attr&PV_ATTR_DUPLICATE > 0
}

func (p *PhysicalVolume) IsAllocatable() bool {
	return p.Attr&PV_ATTR_ALLOCATABLE > 0
}

func (p *PhysicalVolume) IsUsed() bool {
	return p.Attr&PV_ATTR_USED > 0
}
