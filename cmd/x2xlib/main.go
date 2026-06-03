package main

import (
	"github.com/kisun-bit/drpkg/ps/recovery/x2xlib"
)

func main() {
	x, e := x2xlib.NewX2XLib("D:\\workspace\\drpkg\\ps\\recovery\\x2xlib\\library", false)
	if e != nil {
		panic(e)
	}
	defer x.Close()

	_ = x

	//if _, _, err := x.AddLinuxVirtualDriver(
	//	"xen-kmp-default",
	//	"4.1.23413",
	//	define.HPVTXen,
	//	"",
	//	"amd64",
	//	"D:\\download\\sles_xen",
	//	"",
	//	define.LinuxFamilySUSE,
	//	x2xlib.Signature{
	//		Signer: define.DrvSignerDistro,
	//		Hash:   define.DrvHashSHA256,
	//	},
	//	[]string{"3.0.101-63-default"},
	//); err != nil {
	//	panic(err)
	//}
	//
	//if _, _, err := x.AddWindowsVirtualDriver(
	//	"VMware PVSCSI StorPort driver (64-bit)",
	//	"1.3.26.0",
	//	define.HPVTVmware,
	//	"",
	//	"amd64",
	//	"D:\\download\\pvscsi\\Win7\\amd64",
	//	"",
	//	[]x2xlib.Signature{
	//		{Signer: define.DrvSignerWHQL, Hash: define.DrvHashSHA256},
	//	},
	//	"6.0",
	//	"6.1.65535",
	//); err != nil {
	//	panic(err)
	//}
	//
	//if _, _, err := x.AddWindowsVirtualDriver(
	//	"VMware PVSCSI StorPort driver (64-bit)",
	//	"1.3.26.0",
	//	define.HPVTVmware,
	//	"",
	//	"amd64",
	//	"D:\\download\\pvscsi\\Win8\\amd64",
	//	"",
	//	[]x2xlib.Signature{
	//		{Signer: define.DrvSignerWHQL, Hash: define.DrvHashSHA256},
	//	},
	//	"6.2",
	//	"10.0.20123",
	//); err != nil {
	//	panic(err)
	//}
	//
	//if _, _, err := x.AddWindowsVirtualDriver(
	//	"VMware PVSCSI StorPort driver (64-bit)",
	//	"1.3.26.0",
	//	define.HPVTVmware,
	//	"",
	//	"amd64",
	//	"D:\\download\\pvscsi\\Win10\\amd64",
	//	"",
	//	[]x2xlib.Signature{
	//		{Signer: define.DrvSignerWHQL, Hash: define.DrvHashSHA256},
	//	},
	//	"10.0.20124",
	//	"10.0.65535",
	//); err != nil {
	//	panic(err)
	//}
	//
	//if _, _, err := x.AddWindowsVirtualDriver(
	//	"VMware PVSCSI miniport driver (32-bit)",
	//	"1.3.26.0",
	//	define.HPVTVmware,
	//	"",
	//	"amd64",
	//	"D:\\download\\pvscsi\\Win7\\i386",
	//	"",
	//	[]x2xlib.Signature{
	//		{Signer: define.DrvSignerWHQL, Hash: define.DrvHashSHA256},
	//	},
	//	"6.0",
	//	"6.1.65535",
	//); err != nil {
	//	panic(err)
	//}
	//
	//if _, _, err := x.AddWindowsVirtualDriver(
	//	"VMware PVSCSI miniport driver (32-bit)",
	//	"1.3.26.0",
	//	define.HPVTVmware,
	//	"",
	//	"amd64",
	//	"D:\\download\\pvscsi\\Win8\\i386",
	//	"",
	//	[]x2xlib.Signature{
	//		{Signer: define.DrvSignerWHQL, Hash: define.DrvHashSHA256},
	//	},
	//	"6.2",
	//	"10.0.65535",
	//); err != nil {
	//	panic(err)
	//}

	//if _, _, err := x.AddLinuxNormalDriver(
	//	"NCR, Symbios and LSI 8xx and 1010 PCI SCSI adapters",
	//	"0.0.4",
	//	"",
	//	"amd64",
	//	"D:\\download\\kmod-sym53c8xx-0.0-2.el8_10.elrepo.x86_64",
	//	"",
	//	define.LinuxFamilyRHEL,
	//	x2xlib.Signature{},
	//	[]string{"4.18.0-553.el8_10.x86_64"},
	//	[]string{
	//		"pci:v00001000d0000008Fsv*sd*bc*sc*i*",
	//		"pci:v00001000d00000021sv*sd*bc*sc*i*",
	//		"pci:v00001000d00000020sv*sd*bc*sc*i*",
	//		"pci:v00001000d00000013sv*sd*bc*sc*i*",
	//		"pci:v00001000d00000012sv*sd*bc*sc*i*",
	//		"pci:v00001000d00000010sv*sd*bc01sc00i*",
	//		"pci:v00001000d0000000Fsv*sd*bc*sc*i*",
	//		"pci:v00001000d0000000Dsv*sd*bc*sc*i*",
	//		"pci:v00001000d0000000Csv*sd*bc*sc*i*",
	//		"pci:v00001000d0000000Bsv*sd*bc*sc*i*",
	//		"pci:v00001000d0000000Asv*sd*bc01sc00i*",
	//		"pci:v00001000d00000006sv*sd*bc*sc*i*",
	//		"pci:v00001000d00000005sv*sd*bc*sc*i*",
	//		"pci:v00001000d00000004sv*sd*bc*sc*i*",
	//		"pci:v00001000d00000003sv*sd*bc*sc*i*",
	//		"pci:v00001000d00000002sv*sd*bc*sc*i*",
	//		"pci:v00001000d00000001sv*sd*bc*sc*i*",
	//	},
	//); err != nil {
	//	panic(err)
	//}

	//if err := x.DeleteVirtualDriver("4c4d1fc5-2ae1-42cb-811b-07a41d285d52"); err != nil {
	//	panic(err)
	//}

	//up, e := universal.UniPciFromModalias("pci:v00001000d00000020sv*sd*bc*sc*i*")
	//if e != nil {
	//	panic(e)
	//}
	//if a, b, err := x.SelectLinuxBestNormalDriver("amd64", define.LinuxFamilyRHEL, "4.18.0-553.el8_10.x86_64", up.String()); err != nil {
	//	panic(err)
	//} else {
	//	fmt.Println(a, b)
	//}

	//if a, b, err := x.GetWindowsCompatibleVirtualDriver(define.HPVTVmware, "amd64", "6.0.200"); err != nil {
	//	panic(err)
	//} else {
	//	fmt.Println(a, b)
	//}
	//
	//if a, b, err := x.GetLinuxCompatibleVirtualDriver(define.HPVTXen, "amd64", define.DistroSLES, "3.0.101-63-default", ""); err != nil {
	//	panic(err)
	//} else {
	//	fmt.Println(a, b)
	//}
}
