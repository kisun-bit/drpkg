package main

import (
	"github.com/davecgh/go-spew/spew"
	"github.com/kisun-bit/drpkg/ps/sysrepair"
)

func main() {
	pf, err := sysrepair.RuntimePlatform()
	if err != nil {
		panic(err)
	}
	spew.Dump(pf)
}
