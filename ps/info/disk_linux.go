package info

import (
	"fmt"

	"github.com/kisun-bit/drpkg/extend"
)

func QueryDisks() (disks []Disk, err error) {
	diskPaths, err := extend.ListDisks()
	if err != nil {
		return nil, err
	}
	for _, diskPath := range diskPaths {
		fmt.Println(diskPath)
	}
	return disks, nil
}
