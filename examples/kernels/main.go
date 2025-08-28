package main

import (
	"os"

	"github.com/davecgh/go-spew/spew"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/ps/info"
)

func main() {
	ks, err := info.QueryLinuxKernels(os.Args[1])
	if err != nil {
		logger.Fatal(err)
	}
	spew.Dump(ks)

	lr := info.QueryLinuxRelease(os.Args[1])
	spew.Dump(lr)
}
