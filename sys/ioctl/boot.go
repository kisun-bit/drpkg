package ioctl

import (
	"www.velocidex.com/golang/velociraptor/vql/efi"
)

func IsBootByUEFI() bool {
	vars, e := efi.GetEfiVariables()
	if e != nil {
		return false
	}
	return len(vars) > 0
}
