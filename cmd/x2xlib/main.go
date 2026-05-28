package main

import (
	"github.com/kisun-bit/drpkg/ps/recovery/x2xlib"
)

func main() {
	x, e := x2xlib.NewX2XLib("D:\\workspace\\drpkg\\ps\\recovery\\x2xlib\\library")
	if e != nil {
		panic(e)
	}
	//if err := x.Initialize(); err != nil {
	//	panic(err)
	//}

	//driverId, err := x.AddLinuxVirtualizationDriver(
	//	"sles",
	//	"amd64",
	//	define.HPVTXen,
	//	"xen-kmp-default",
	//	"4.1.23413",
	//	"D:\\download\\sles_xen",
	//	[]string{
	//		"3.0.101-63-default",
	//	},
	//)
	//fmt.Println(driverId, err)

}
