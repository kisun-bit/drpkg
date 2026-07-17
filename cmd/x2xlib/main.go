package main

import (
	"github.com/kisun-bit/drpkg/define"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/ps/recovery/x2xlib"
)

func main() {
	x, e := x2xlib.NewX2XLib("D:\\workspace\\drpkg\\ps\\recovery\\x2xlib\\library", false)
	if e != nil {
		panic(e)
	}
	defer x.Close()

	_ = x

	//addVirtioForWin7(x)
	//addVirtioForWin8(x)
	//addVirtioForWin81(x)
	//addVirtioForWin10(x)
	//addVirtioForWin11(x)
	//addVirtioForWin2k8r2(x)
	//addVirtioForWin2k12(x)
	//addVirtioForWin2k12R2(x)
	//addVirtioForWin2k16(x)
	//addVirtioForWin2k19(x)
	//addVirtioForWin2k22(x)

}

func test(x *x2xlib.X2XLib) {
	id, _, e := x.AddLinuxNormalDriver(
		"zktest",
		"0.0.1",
		"amd64",
		"D:\\download\\kmod-sym53c8xx-0.0-4.el9_7.elrepo.x86_64",
		"honki",
		"测试使用",
		define.LinuxFamilyRHEL,
		x2xlib.Signature{
			Signer: define.DrvSignerDistro,
			Hash:   define.DrvHashUnknown,
		},
		[]string{"zk01"},
		[]string{"3.10.1"},
		[]string{"pci:v00001af4d00001001sv*sd*bc*sc*i*"})
	if e != nil {
		panic(e)
	}
	logger.Debugf("add linux driver success, id=%s", id)

	ret, e := x.SelectLinuxBestNormalDriver(
		"amd64",
		define.LinuxFamilyRHEL,
		"3.10.1",
		"pci:v00001af4d00001001sv00001af4sd00000000bc01sc06i01")
	if e != nil {
		panic(e)
	}
	logger.Debugf("select linux driver success, id=%s", ret.Id)
}

func addVirtioForWin7(x *x2xlib.X2XLib) {
	_, _, _ = x.AddWindowsVirtualDriver(
		"virtio", "0.1.185", define.HPVTKvm, "amd64", "D:\\download\\cas-virtio-win\\virtio-win7-0.1.185\\amd64", "New H3C Technologies Co., Ltd", "",
		[]x2xlib.Signature{{Signer: define.DrvSignerVendor, Hash: define.DrvHashSHA1}},
		[]string{"viostor", "vioscsi", "netkvm"},
		[]define.WindowsVersion{define.Win7},
	)
	_, _, _ = x.AddWindowsVirtualDriver(
		"virtio", "0.1.185", define.HPVTKvm, "386", "D:\\download\\cas-virtio-win\\virtio-win7-0.1.185\\x86", "New H3C Technologies Co., Ltd", "",
		[]x2xlib.Signature{{Signer: define.DrvSignerVendor, Hash: define.DrvHashSHA1}},
		[]string{"viostor", "vioscsi", "netkvm"},
		[]define.WindowsVersion{define.Win7},
	)
}

func addVirtioForWin8(x *x2xlib.X2XLib) {
	_, _, _ = x.AddWindowsVirtualDriver(
		"virtio", "0.1.185", define.HPVTKvm, "amd64", "D:\\download\\cas-virtio-win\\virtio-win8-0.1.185\\amd64", "New H3C Technologies Co., Ltd", "",
		[]x2xlib.Signature{{Signer: define.DrvSignerVendor, Hash: define.DrvHashSHA1}},
		[]string{"viostor", "vioscsi", "netkvm"},
		[]define.WindowsVersion{define.Win8},
	)
	_, _, _ = x.AddWindowsVirtualDriver(
		"virtio", "0.1.185", define.HPVTKvm, "386", "D:\\download\\cas-virtio-win\\virtio-win8-0.1.185\\x86", "New H3C Technologies Co., Ltd", "",
		[]x2xlib.Signature{{Signer: define.DrvSignerVendor, Hash: define.DrvHashSHA1}},
		[]string{"viostor", "vioscsi", "netkvm"},
		[]define.WindowsVersion{define.Win8},
	)
}

func addVirtioForWin81(x *x2xlib.X2XLib) {
	_, _, _ = x.AddWindowsVirtualDriver(
		"virtio", "0.1.185", define.HPVTKvm, "amd64", "D:\\download\\cas-virtio-win\\virtio-win8.1-0.1.185\\amd64", "New H3C Technologies Co., Ltd", "",
		[]x2xlib.Signature{{Signer: define.DrvSignerVendor, Hash: define.DrvHashSHA1}},
		[]string{"viostor", "vioscsi", "netkvm"},
		[]define.WindowsVersion{define.Win81},
	)
	_, _, _ = x.AddWindowsVirtualDriver(
		"virtio", "0.1.185", define.HPVTKvm, "386", "D:\\download\\cas-virtio-win\\virtio-win8.1-0.1.185\\x86", "New H3C Technologies Co., Ltd", "",
		[]x2xlib.Signature{{Signer: define.DrvSignerVendor, Hash: define.DrvHashSHA1}},
		[]string{"viostor", "vioscsi", "netkvm"},
		[]define.WindowsVersion{define.Win81},
	)
}

func addVirtioForWin10(x *x2xlib.X2XLib) {
	_, _, _ = x.AddWindowsVirtualDriver(
		"virtio", "0.1.217", define.HPVTKvm, "amd64", "D:\\download\\cas-virtio-win\\virtio-win10-0.1.217\\amd64", "New H3C Technologies Co., Ltd", "",
		[]x2xlib.Signature{{Signer: define.DrvSignerVendor, Hash: define.DrvHashSHA1}},
		[]string{"viostor", "vioscsi", "netkvm"},
		[]define.WindowsVersion{define.Win10},
	)
	_, _, _ = x.AddWindowsVirtualDriver(
		"virtio", "0.1.217", define.HPVTKvm, "386", "D:\\download\\cas-virtio-win\\virtio-win10-0.1.217\\x86", "New H3C Technologies Co., Ltd", "",
		[]x2xlib.Signature{{Signer: define.DrvSignerVendor, Hash: define.DrvHashSHA1}},
		[]string{"viostor", "vioscsi", "netkvm"},
		[]define.WindowsVersion{define.Win10},
	)
}

func addVirtioForWin11(x *x2xlib.X2XLib) {
	_, _, _ = x.AddWindowsVirtualDriver(
		"virtio", "0.1.217", define.HPVTKvm, "amd64", "D:\\download\\cas-virtio-win\\virtio-win11-0.1.217\\amd64", "New H3C Technologies Co., Ltd", "",
		[]x2xlib.Signature{{Signer: define.DrvSignerVendor, Hash: define.DrvHashSHA1}},
		[]string{"viostor", "vioscsi", "netkvm"},
		[]define.WindowsVersion{define.Win10},
	)
}

func addVirtioForWin2k8r2(x *x2xlib.X2XLib) {
	_, _, _ = x.AddWindowsVirtualDriver(
		"virtio", "0.1.185", define.HPVTKvm, "amd64", "D:\\download\\cas-virtio-win\\virtio-win2008R2-0.1.185\\amd64", "New H3C Technologies Co., Ltd", "",
		[]x2xlib.Signature{{Signer: define.DrvSignerVendor, Hash: define.DrvHashSHA1}},
		[]string{"viostor", "vioscsi", "netkvm"},
		[]define.WindowsVersion{define.Win2k8r2},
	)
}

func addVirtioForWin2k12(x *x2xlib.X2XLib) {
	_, _, _ = x.AddWindowsVirtualDriver(
		"virtio", "0.1.185", define.HPVTKvm, "amd64", "D:\\download\\cas-virtio-win\\virtio-win2012-0.1.185\\amd64", "New H3C Technologies Co., Ltd", "",
		[]x2xlib.Signature{{Signer: define.DrvSignerVendor, Hash: define.DrvHashSHA1}},
		[]string{"viostor", "vioscsi", "netkvm"},
		[]define.WindowsVersion{define.Win2k12},
	)
}

func addVirtioForWin2k12R2(x *x2xlib.X2XLib) {
	_, _, _ = x.AddWindowsVirtualDriver(
		"virtio", "0.1.185", define.HPVTKvm, "amd64", "D:\\download\\cas-virtio-win\\virtio-win2012R2-0.1.185\\amd64", "New H3C Technologies Co., Ltd", "",
		[]x2xlib.Signature{{Signer: define.DrvSignerVendor, Hash: define.DrvHashSHA1}},
		[]string{"viostor", "vioscsi", "netkvm"},
		[]define.WindowsVersion{define.Win2k12r2},
	)
}

func addVirtioForWin2k16(x *x2xlib.X2XLib) {
	_, _, _ = x.AddWindowsVirtualDriver(
		"virtio", "0.1.217", define.HPVTKvm, "amd64", "D:\\download\\cas-virtio-win\\virtio-win2016-0.1.185\\amd64", "New H3C Technologies Co., Ltd", "",
		[]x2xlib.Signature{{Signer: define.DrvSignerVendor, Hash: define.DrvHashSHA1}},
		[]string{"viostor", "vioscsi", "netkvm"},
		[]define.WindowsVersion{define.Win2k16},
	)
}

func addVirtioForWin2k19(x *x2xlib.X2XLib) {
	_, _, _ = x.AddWindowsVirtualDriver(
		"virtio", "0.1.217", define.HPVTKvm, "amd64", "D:\\download\\cas-virtio-win\\virtio-win2019-0.1.185\\amd64", "New H3C Technologies Co., Ltd", "",
		[]x2xlib.Signature{{Signer: define.DrvSignerVendor, Hash: define.DrvHashSHA1}},
		[]string{"viostor", "vioscsi", "netkvm"},
		[]define.WindowsVersion{define.Win2k19},
	)
}

func addVirtioForWin2k22(x *x2xlib.X2XLib) {
	_, _, _ = x.AddWindowsVirtualDriver(
		"virtio", "0.1.217", define.HPVTKvm, "amd64", "D:\\download\\cas-virtio-win\\virtio-win2022-0.1.185\\amd64", "New H3C Technologies Co., Ltd", "",
		[]x2xlib.Signature{{Signer: define.DrvSignerVendor, Hash: define.DrvHashSHA1}},
		[]string{"viostor", "vioscsi", "netkvm"},
		[]define.WindowsVersion{define.Win2k22},
	)
}
