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
	if err := x.Initialize(); err != nil {
		panic(err)
	}

	//if _, _, err := x.AddLinuxVDL(
	//	"XenKmpDefault",
	//	"4.1.23413",
	//	"",
	//	define.HPVTXen,
	//	"",
	//	"amd64",
	//	define.DistroSLES,
	//	[]string{"3.0.101-63-default"},
	//	"D:\\download\\sles_xen",
	//); err != nil {
	//	panic(err)
	//}
	//
	//if _, _, err := x.AddWindowsVDL(
	//	"VMwarePVSCSI",
	//	"1.3.26",
	//	"",
	//	define.HPVTVmware,
	//	"",
	//	"amd64",
	//	define.MsSignSha256,
	//	true,
	//	"6.0",
	//	"6.1.999999999",
	//	"E:\\Program Files\\VMware\\VMware Tools\\Drivers\\pvscsi\\Win7\\amd64",
	//); err != nil {
	//	panic(err)
	//}
	//
	//if _, _, err := x.AddWindowsVDL(
	//	"VMwarePVSCSI",
	//	"1.3.26",
	//	"",
	//	define.HPVTVmware,
	//	"",
	//	"amd64",
	//	define.MsSignSha256,
	//	true,
	//	"6.2",
	//	"10.0.20123",
	//	"E:\\Program Files\\VMware\\VMware Tools\\Drivers\\pvscsi\\Win8\\amd64",
	//); err != nil {
	//	panic(err)
	//}
	//
	//if _, _, err := x.AddWindowsVDL(
	//	"VMwarePVSCSI",
	//	"1.3.26",
	//	"",
	//	define.HPVTVmware,
	//	"",
	//	"amd64",
	//	define.MsSignSha256,
	//	true,
	//	"10.0.20124",
	//	"10.0.999999999",
	//	"E:\\Program Files\\VMware\\VMware Tools\\Drivers\\pvscsi\\Win10\\amd64",
	//); err != nil {
	//	panic(err)
	//}
	//if _, _, err := x.AddWindowsVDL(
	//	"VMwarePVSCSI",
	//	"1.3.26",
	//	"",
	//	define.HPVTVmware,
	//	"",
	//	"386",
	//	define.MsSignSha256,
	//	true,
	//	"6.0",
	//	"6.1.999999999",
	//	"E:\\Program Files\\VMware\\VMware Tools\\Drivers\\pvscsi\\Win7\\i386",
	//); err != nil {
	//	panic(err)
	//}
	//if _, _, err := x.AddWindowsVDL(
	//	"VMwarePVSCSI",
	//	"1.3.26",
	//	"",
	//	define.HPVTVmware,
	//	"",
	//	"386",
	//	define.MsSignSha256,
	//	true,
	//	"6.2",
	//	"10.0.20123",
	//	"E:\\Program Files\\VMware\\VMware Tools\\Drivers\\pvscsi\\Win8\\i386",
	//); err != nil {
	//	panic(err)
	//}

	if a, b, err := x.GetWindowsCompatibleVDL(define.HPVTVmware, "amd64", "10.0.1000", define.MsSignNone, false); err != nil {
		panic(err)
	} else {
		fmt.Println(a, b)
	}

	if a, b, err := x.GetLinuxCompatibleVDL(define.HPVTXen, "amd64", define.DistroSLES, "3.0.101-63-default", ""); err != nil {
		panic(err)
	} else {
		fmt.Println(a, b)
	}
}
