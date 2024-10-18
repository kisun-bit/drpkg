package lvm

import (
	"fmt"
	"strings"
)

func CommandStringForPvCreate(device, uuid string) string {
	return fmt.Sprintf("lvm pvcreate -ff --yes -v --uuid %s --norestorefile %s", uuid, device)
}

func CommandStringForPvScanCache(device string) string {
	return fmt.Sprintf("lvm pvscan --cache %s", device)
}

func CommandStringForPvs(filterDevices ...string) string {
	filterStr := ""
	if len(filterDevices) > 0 {
		filterStr = strings.Join(filterDevices, " ")
	}
	return fmt.Sprintf("lvm pvs -o %s --noheadings --units b --nosuffix --separator , %s",
		"pv_name,vg_name,pv_uuid,pv_fmt,pv_attr,pv_size,pv_free",
		filterStr)
}

func CommandStringForPvResize(device string, size int64) string {
	return fmt.Sprintf("lvm pvresize -y --setphysicalvolumesize %v %s", size, device)
}

func CommandStringForPvRemove(device string) string {
	return fmt.Sprintf("lvm pvremove -y %s", device)
}

func CommandStringForVgCreate(vgName string, physicalextentsizeInBytes int, pvDevices ...string) string {
	if physicalextentsizeInBytes <= 0 {
		physicalextentsizeInBytes = 4 << 20
	}
	return fmt.Sprintf("lvm vgcreate --physicalextentsize %vb %s %s",
		physicalextentsizeInBytes, vgName, strings.Join(pvDevices, " "))
}

func CommandStringForVgs(filterVgNames ...string) string {
	filterStr := ""
	if len(filterVgNames) > 0 {
		filterStr = strings.Join(filterVgNames, " ")
	}
	return fmt.Sprintf("lvm vgs -o %s --noheadings --units b --nosuffix --separator , %s",
		"vg_name,vg_uuid,vg_attr,vg_extent_size,vg_free,vg_size",
		filterStr)
}

func CommandStringForVgRename(oldVgName, newVgName string) string {
	return fmt.Sprintf("lvm vgrename -y %s %s", oldVgName, newVgName)
}

func CommandStringForVgExtend(vgName string, pvDevices ...string) string {
	return fmt.Sprintf("lvm vgextend %s %s", vgName, strings.Join(pvDevices, " "))
}

func CommandStringForVgReduce(vgName string, pvDevices ...string) string {
	return fmt.Sprintf("lvm vgreduce %s %s", vgName, strings.Join(pvDevices, " "))
}

func CommandStringForVgRemove(vgName string) string {
	return fmt.Sprintf("lvm vgremove -f -y %s", vgName)
}

func CommandStringForLvCreate(lvName, vgName string, lvType LVType, lvSizeInByte int64) string {
	return fmt.Sprintf("lvm lvcreate -y --type %s -L %vb -n %s %s", lvType, lvSizeInByte, lvName, vgName)
}

func CommandStringForThinLVCreate(thinLvName, vgName, thinPoolName string, thinLvSizeInBytes int64) string {
	return fmt.Sprintf("lvm lvcreate -y -V %vb -n %s --thinpool %s %s",
		thinLvSizeInBytes, thinLvName, thinPoolName, vgName)
}

func CommandStringForLvs(filterLvFullNames ...string) string {
	return fmt.Sprintf("lvm lvs -o %s --noheadings --units b --nosuffix --separator , %s",
		"lv_name,vg_name,lv_uuid,lv_attr,lv_size,pool_lv,origin",
		strings.Join(filterLvFullNames, ""))
}

func CommandStringForLvRename(vgName, lvOldName, lvNewName string) string {
	return fmt.Sprintf("lvm lvrename %s %s %s", vgName, lvOldName, lvNewName)
}

func CommandStringForLvRemove(lvFullName string) string {
	return fmt.Sprintf("lvm lvremove -f -y %s", lvFullName)
}

func CommandStringForConvertLvToThinPool(lvFullNameForMetaData, lvFullNameForThinPool string) string {
	return fmt.Sprintf("lvm lvconvert -y --type thin-pool %s --poolmetadata %s",
		lvFullNameForThinPool, lvFullNameForMetaData)
}

func CommandStringForCreateThinPool(vgName, thinPoolName string, sizeInByte int64) string {
	return fmt.Sprintf("lvm lvcreate -L %vB -T %s/%s", sizeInByte, vgName, thinPoolName)
}
