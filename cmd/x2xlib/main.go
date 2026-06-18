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

	id, _, e := x.AddLinuxNormalDriver(
		"zktest",
		"0.0.1",
		"honki",
		"amd64",
		"D:\\download\\kmod-sym53c8xx-0.0-4.el9_7.elrepo.x86_64",
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
