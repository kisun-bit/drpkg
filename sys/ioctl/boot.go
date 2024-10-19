package ioctl

import "github.com/kisun-bit/drpkg/sys/efi"

func IsBootByUEFI() bool {
	vars, e := efi.GetEfiVariables()
	if e != nil {
		return false
	}
	return len(vars) > 0
}
