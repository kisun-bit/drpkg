package lvm

import (
	"fmt"
	"github.com/kisun-bit/drpkg/util/basic"
	"github.com/pkg/errors"
	"strconv"
	"strings"
	"unicode"
)

const (
	ECMD_PROCESSED    = iota + 1
	ENO_SUCH_CMD      = iota + 1
	EINVALID_CMD_LINE = iota + 1
	EINIT_FAILED      = iota + 1
	ECMD_FAILED       = iota + 1
)

var SupportLVM bool
var LVMMarjorVersion int

func init() {
	_, _ = lvmProbe()
}

func lvmProbe() (bool, error) {
	r, o, eo := ExecV1("lvm version")
	switch r {
	case 127:
		return false, nil
	case 0:
		SupportLVM = true
		noSpaceOs := strings.Split(basic.TrimAllSpace(o), ":")
		if len(noSpaceOs) > 1 && unicode.IsDigit(rune(noSpaceOs[1][0])) {
			LVMMarjorVersion, _ = strconv.Atoi(string(noSpaceOs[1][0]))
		}
		return true, nil
	default:
		return false, errors.Errorf("support lvm: failed to query version of lvm, output=`%s` error=`%s`", o, eo)
	}
}

func Pvcreate(device, uuid string) error {
	r, o, eo := ExecV1(CommandStringForPvCreate(device, uuid))
	if r != 0 {
		return errors.Errorf("failed to create physical volume named %s. output=`%s`, error_output=`%s`",
			device, o, eo)
	}
	_ = PvscanCache(device)
	return nil
}

func PvscanCache(device string) error {
	r, o, eo := ExecV1(CommandStringForPvScanCache(device))
	if r != 0 {
		return errors.Errorf("failed to scan physical volume named %s. output=`%s`, error_output=`%s`",
			device, o, eo)
	}
	return nil
}

func Pvs(pvPaths ...string) ([]*PhysicalVolume, error) {
	r, o, eo := ExecV1(CommandStringForPvs(pvPaths...))
	if r != 0 {
		return nil, errors.Errorf(
			"failed to list physical volume with pvPaths-rule(`%v`). o=`%s`, error_output=`%s`",
			pvPaths, o, eo)
	}

	pvList := make([]*PhysicalVolume, 0)
	for _, pv := range strings.Split(o, "\n") {
		pv = strings.TrimSpace(pv)
		if !strings.HasPrefix(pv, "/") {
			continue
		}
		vals := strings.Split(pv, ",")
		/*
			vals的索引与值类型的对应关系：
			pv_name,vg_name,pv_uuid,pv_fmt,pv_attr,pv_size,pv_free
			0       1       2       3      4       5       6
			**/
		attrVal, err := parsePvAttrs(vals[4])
		if err != nil {
			return nil, fmt.Errorf("pvs: %v", err)
		}
		size, err := strconv.ParseInt(vals[5], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("pvs: could not convert %s to int64", vals[5])
		}
		free, err := strconv.ParseInt(vals[6], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("pvs: could not convert %s to int64", vals[6])
		}
		pvList = append(pvList, &PhysicalVolume{
			Path:    vals[0],
			VgName:  vals[1],
			UUID:    vals[2],
			PvFmt:   vals[3],
			Attr:    attrVal,
			AttrStr: vals[4],
			Size:    size,
			Free:    free,
		})
	}
	return pvList, nil
}

func Pvresize(device string, size int64) error {
	r, o, eo := ExecV1(CommandStringForPvResize(device, size))
	if r != 0 {
		return errors.Errorf("failed to set size for physical volume named %s. output=`%s`, error_output=`%s`",
			device, o, eo)
	}
	return nil
}

func Pvremove(device string) error {
	r, o, eo := ExecV1(CommandStringForPvRemove(device))
	if r != 0 {
		return errors.Errorf("failed to remove physical volume named %s. output=`%s`, error_output=`%s`",
			device, o, eo)
	}
	_ = PvscanCache(device)
	return nil
}

func Vgcreate(name string, physicalextentsizeInBytes int, pvPaths ...string) error {
	r, o, eo := ExecV1(CommandStringForVgCreate(name, physicalextentsizeInBytes, pvPaths...))
	if r != 0 {
		return errors.Errorf("failed to create volume group named %s. output=`%s`, error_output=`%s`",
			name, o, eo)
	}
	return nil
}

func Vgs(filterVgNames ...string) ([]*VolumeGroup, error) {
	r, output, err := ExecV1(CommandStringForVgs(filterVgNames...))
	if r != 0 {
		return nil, fmt.Errorf("vgs: %v", err)
	}

	vgList := make([]*VolumeGroup, 0)
	vgs := strings.Split(output, "\n")
	for _, vg := range vgs {
		vg = strings.TrimSpace(vg)
		if vg == "" {
			continue
		}
		vals := strings.Split(vg, ",")
		/*
			vals的索引与值类型的对应关系：
			vg_name,vg_uuid,vg_attr,vg_extent_size,vg_free,vg_size
			0       1       2       3              4       5
			**/
		attrVal, err := parseVgAttrs(vals[2])
		if err != nil {
			return nil, fmt.Errorf("vgs parse attrs: %v", err)
		}
		size, err := strconv.ParseInt(vals[5], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("vgs: could not convert %s to int64", vals[5])
		}
		free, err := strconv.ParseInt(vals[4], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("vgs: could not convert %s to int64", vals[4])
		}
		extentSize, err := strconv.Atoi(vals[3])
		if err != nil {
			return nil, err
		}

		pvs, err := Pvs()
		if err != nil {
			return nil, fmt.Errorf("vgs: could not get list of pvs: %v", err)
		}
		matchedPvs := make([]*PhysicalVolume, 0)
		for _, pv := range pvs {
			if pv.VgName == vals[0] {
				matchedPvs = append(matchedPvs, pv)
			}
		}

		lvs, err := Lvs()
		if err != nil {
			return nil, fmt.Errorf("vgs: could not get list of lvs: %v", err)
		}
		matchedLvs := make([]*LogicalVolume, 0)
		for _, lv := range lvs {
			if lv.VgName == vals[0] {
				matchedLvs = append(matchedLvs, lv)
			}
		}

		vgList = append(vgList, &VolumeGroup{
			Name:       vals[0],
			UUID:       vals[1],
			AttrStr:    vals[2],
			Pvs:        matchedPvs,
			Lvs:        matchedLvs,
			Attr:       attrVal,
			ExtentSize: extentSize,
			Size:       size,
			Free:       free,
		})
	}

	return vgList, nil
}

func Vgrename(oldName, newName string) (*VolumeGroup, error) {
	r, o, eo := ExecV1(CommandStringForVgRename(oldName, newName))
	if r != 0 {
		return nil, errors.Errorf("failed to rename volume group named %s to %s. output=`%s`, error_output=`%s`",
			oldName, newName, o, eo)
	}
	newVg, err := Vgs(newName)
	if err != nil {
		return nil, fmt.Errorf("vgrename: %v", err)
	}
	if len(newVg) == 0 {
		return nil, fmt.Errorf("vgrename: new vg not found")
	}
	return newVg[0], nil
}

func Vgextend(vgName string, pvDevices ...string) error {
	if len(pvDevices) == 0 {
		return errors.New("vgextend: No PVs were provided")
	}
	r, o, eo := ExecV1(CommandStringForVgExtend(vgName, pvDevices...))
	if r != 0 {
		return errors.Errorf("failed to extend volume group named %s. output=`%s`, error_output=`%s`",
			vgName, o, eo)
	}
	return nil
}

func Vgreduce(vgName string, pvDevices ...string) error {
	if len(pvDevices) == 0 {
		return errors.New("vgreduce: No PVs were provided")
	}
	r, o, eo := ExecV1(CommandStringForVgReduce(vgName, pvDevices...))
	if r != 0 {
		return errors.Errorf("failed to reduce volume group named %s. output=`%s`, error_output=`%s`",
			vgName, o, eo)
	}
	return nil
}

func Vgremove(vgName string) error {
	r, o, eo := ExecV1(CommandStringForVgRemove(vgName))
	if r != 0 {
		return errors.Errorf("failed to remove volume group named %s. output=`%s`, error_output=`%s`",
			vgName, o, eo)
	}
	return nil
}

func Lvcreate(lvName string, vgName string, lvType LVType, sizeInBytes int64) error {
	r, o, eo := ExecV1(CommandStringForLvCreate(lvName, vgName, lvType, sizeInBytes))
	if r != 0 {
		return errors.Errorf("failed to create logical volume named %s. output=`%s`, error_output=`%s`",
			lvName, o, eo)
	}
	return nil
}

func LvThinCreate(name, vg, pool string, size int64) error {
	r, o, eo := ExecV1(CommandStringForThinLVCreate(name, vg, pool, size))
	if r != 0 {
		return errors.Errorf("failed to create thin logical volume named %s. output=`%s`, error_output=`%s`",
			name, o, eo)
	}
	return nil
}

func Lvs(filter ...string) ([]*LogicalVolume, error) {
	r, o, eo := ExecV1(CommandStringForLvs(filter...))
	if r != 0 {
		return nil, errors.Errorf(
			"failed to list physical volume with pvPaths-rule(`%v`). o=`%s`, error_output=`%s`",
			filter, o, eo)
	}

	lvList := make([]*LogicalVolume, 0)
	for _, lv := range strings.Split(o, "\n") {
		lv = strings.TrimSpace(lv)
		if lv == "" {
			continue
		}
		vals := strings.Split(lv, ",")
		/*
			vals的索引与值类型的对应关系：
			lv_name,vg_name,lv_uuid,lv_attr,lv_size,pool_lv
			0       1       2       3       4       5
			**/
		attrs, err := ParseLvAttrs(vals[3])
		if err != nil {
			return nil, fmt.Errorf("lvs parse attrs: %v", err)
		}

		size, err := strconv.ParseInt(vals[4], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("lvs: could not convert %s to int64", vals[5])
		}

		lvList = append(lvList, &LogicalVolume{
			Name:            vals[0],
			VgName:          vals[1],
			UUID:            vals[2],
			Pool:            vals[5],
			Origin:          vals[6],
			AttrVolType:     attrs[0],
			AttrPermissions: attrs[1],
			AttrAllocPolicy: attrs[2],
			AttrFixed:       attrs[3],
			AttrState:       attrs[4],
			AttrDevice:      attrs[5],
			AttrTargetType:  attrs[6],
			AttrBlocks:      attrs[7],
			AttrHealth:      attrs[8],
			AttrSkip:        attrs[9],
			AttrStr:         vals[3],
			Size:            size,
		})
	}

	return lvList, nil
}

func Lvrename(oldName, newName string, vg string) (*LogicalVolume, error) {
	r, o, eo := ExecV1(CommandStringForLvRename(vg, oldName, newName))
	if r != 0 {
		return nil, errors.Errorf("failed to rename logical volume named %s to %s. output=`%s`, error_output=`%s`",
			oldName, newName, o, eo)
	}
	newLv, err := Lvs(vg + "/" + newName)
	if err != nil {
		return nil, fmt.Errorf("lvrename: %v", err)
	}
	if len(newLv) == 0 {
		return nil, fmt.Errorf("lvrename: new lv not found")
	}
	return newLv[0], nil
}

func Lvremove(lvFullName string) error {
	r, o, eo := ExecV1(CommandStringForLvRemove(lvFullName))
	if r != 0 {
		return errors.Errorf("failed to remove logical volume named %s. output=`%s`, error_output=`%s`",
			lvFullName, o, eo)
	}
	return nil
}

func ConvertThinPool(poolName, poolMetadataName string) error {
	r, o, eo := ExecV1(CommandStringForConvertLvToThinPool(poolName, poolMetadataName))
	if r != 0 {
		return errors.Errorf(
			"failed to make thin volume with pool-meta-lv(`%v`) and pool(`%s`). o=`%s`, error_output=`%s`",
			poolMetadataName, poolName, o, eo)
	}
	return nil
}

func extractPathsFromPvs(pvs ...interface{}) ([]string, error) {
	pvPaths := make([]string, 0)
	for _, pv := range pvs {
		switch pvar := pv.(type) {
		case string:
			pvPaths = append(pvPaths, pvar)
		case *PhysicalVolume:
			pvPaths = append(pvPaths, pvar.Path)
		default:
			return nil, errors.New("invalid type for pv. Must be either a string with the PV's path or a pointer to a PV struct")
		}
	}

	return pvPaths, nil
}

func extractNameFromVg(vg interface{}) (string, error) {
	var vgName string
	switch vgvar := vg.(type) {
	case string:
		vgName = vgvar
	case *VolumeGroup:
		vgName = vgvar.Name
	default:
		return "", errors.New("invalid type for vg. Must be either a string with the VG's name or a pointer to a VG struct")
	}

	return vgName, nil
}

func extractNameFromLv(lv interface{}) (string, error) {
	var lvName string
	switch lvar := lv.(type) {
	case string:
		lvName = lvar
	case *LogicalVolume:
		lvName = lvar.VgName + "/" + lvar.Name
	default:
		return "", errors.New("invalid type for lv. Must be either a string with the LV's path ([group_name]/[lv_name]) or a pointer to a LV struct")
	}

	return lvName, nil
}

func extractNameFromPool(pool interface{}) (string, error) {
	var poolName string
	switch lvar := pool.(type) {
	case string:
		poolName = lvar
	case *LogicalVolume:
		poolName = lvar.Name
	default:
		return "", errors.New("invalid type for pool. Must be either a string with the pool's name or a pointer to a LV struct")
	}

	return poolName, nil
}
