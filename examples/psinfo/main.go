package main

import (
	"github.com/davecgh/go-spew/spew"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/ps/info"
)

func main() {
	psi, err := info.QueryPSInfo()
	if err != nil {
		logger.Fatal(err)
	}
	spew.Dump(psi)
}
