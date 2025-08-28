package info

import (
	"strings"

	"github.com/kisun-bit/drpkg/ps/efi"
)

type LinuxKernel struct {
	Name      string
	Vmlinuz   string
	SystemMap string
	Config    string
	Initrd    string
	Bootable  bool
	Default   bool
}

type LinuxRelease struct {
	Distro    string
	ReleaseID string
	Version   string
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
