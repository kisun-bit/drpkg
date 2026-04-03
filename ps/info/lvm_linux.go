package info

import (
	"fmt"
	"strings"

	"github.com/kisun-bit/drpkg/extend"
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
				Name: lv.Name,
				Device: fmt.Sprintf("/dev/mapper/%s-%s",
					strings.ReplaceAll(lv.VgName, "-", "--"),
					strings.ReplaceAll(lv.Name, "-", "--")),
				Attr: lv.AttrStr,
				Size: lv.Size,
			}

			// 仅标准卷才会计算数据分布区间
			if attrArr, e := lvm2cmd.ParseLvAttrs(lv.AttrStr); e == nil {
				if attrArr[0] == 0 {
					segs, err := extend.LVSegments(lvi.Device)
					if err != nil {
						return li, err
					}
					lvi.Segments = segs
				}
			}

			vgi.LVList = append(vgi.LVList, lvi)
		}
		li.VGList = append(li.VGList, vgi)
	}

	return li, nil
}
