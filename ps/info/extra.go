package info

import (
	"strings"

	"github.com/kisun-bit/drpkg/ps/efi"
)

type LinuxKernel struct {
	Name      string `json:"name"`
	Vmlinuz   string `json:"vmlinuz"`
	SystemMap string `json:"systemMap"`
	Config    string `json:"config"`
	Initrd    string `json:"initrd"`
	Bootable  bool   `json:"bootable"`
	Default   bool   `json:"default"`
}

type LinuxRelease struct {
	Distro    string `json:"distro"`
	ReleaseID string `json:"releaseId"`
	Version   string `json:"version"`
}

type LinuxSwap struct {
	Filename string `json:"filename"`
	Type     string `json:"type"`
	Size     int64  `json:"size"`
	Used     int64  `json:"used"`
	Priority int    `json:"priority"`
	UUID     string `json:"uuid"`
	Label    string `json:"label"`
}

func IsVirtualHost(manufacturer string) bool {
	lowerManu := strings.ToLower(manufacturer)

	if lowerManu == "" {
		return true
	}

	virtualVendorList := []string{
		"vmware",
		"qemu",
		"xen",
		"openstack",
		// FIXME 更多
	}

	for _, v := range virtualVendorList {
		if strings.Contains(lowerManu, v) {
			return true
		}
	}

	return false
}

func QueryBootType() string {
	vars, e := efi.GetEfiVariables()
	if e == nil && len(vars) > 0 {
		return "uefi"
	}
	return "bios"
}
