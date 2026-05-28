package main

import (
	"fmt"

	"github.com/kisun-bit/drpkg/define"
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

	//driverId, err := x.AddWindowsVirtualizationDriver(
	//	"amd64",
	//	define.HPVTVmware,
	//	"pvscsi",
	//	"1.3.26.0",
	//	"E:\\Program Files\\VMware\\VMware Tools\\Drivers\\pvscsi\\Win7\\amd64",
	//	"6.0",
	//	"6.1",
	//)
	//fmt.Println(driverId, err)

	//driverId, err := x.AddWindowsVirtualizationDriver(
	//	"amd64",
	//	define.HPVTVmware,
	//	"pvscsi",
	//	"1.3.26.0",
	//	"E:\\Program Files\\VMware\\VMware Tools\\Drivers\\pvscsi\\Win8\\amd64",
	//	"6.2",
	//	"10.0.20123",
	//)
	//fmt.Println(driverId, err)

	driverId, err := x.AddWindowsVirtualizationDriver(
		"amd64",
		define.HPVTVmware,
		"pvscsi",
		"1.3.26.0",
		"E:\\Program Files\\VMware\\VMware Tools\\Drivers\\pvscsi\\Win10\\amd64",
		"10.0.20124",
		"10.0.26100",
	)
	fmt.Println(driverId, err)

	//err := x.RemoveVirtualizationDriver(
	//	"windows",
	//	"microsoft",
	//	"amd64",
	//	[]string{"pvscsi.b06191465a7a11f1bba718c04d43aeb8"},
	//)
	//fmt.Println(err)
}
