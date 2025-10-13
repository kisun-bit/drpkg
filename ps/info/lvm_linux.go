package info

import (
	"fmt"

	"github.com/kisun-bit/drpkg/ps/lvm/lvm2cmd"
)

func QueryLVMInfo() (li LVM, err error) {
	if !lvm2cmd.SupportLVM {
		return li, nil
	}

	// FIXME: 需要过滤掉存在于非本地磁盘的LVM设备

	li.Effective = true

	vgs, err := lvm2cmd.Vgs()
	if err != nil {
		return li, err
	}

	for _, vg := range vgs {
		vgi := VG{
			Name: vg.Name,
			Size: int(vg.Size),
		}
		for _, vgp := range vg.Pvs {
			vgi.PVDeviceList = append(vgi.PVDeviceList, vgp.Path)
		}
		for _, lv := range vg.Lvs {
			lvi := LV{
				Name:   lv.Name,
				Device: fmt.Sprintf("/dev/mapper/%s-%s", lv.VgName, lv.Name),
				Attr:   lv.AttrStr,
				Size:   lv.Size,
			}
			segs, err := LVSegments(lvi.Device)
			if err != nil {
				return li, err
			}
			lvi.Segments = segs
			vgi.LVList = append(vgi.LVList, lvi)
		}
		li.VGList = append(li.VGList, vgi)
	}

	return li, nil
}
