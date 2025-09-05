package main

import (
	"fmt"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/ps/info"
)

func main() {
	psi, err := info.QueryPSInfo()
	if err != nil {
		logger.Fatal(err)
	}
	fmt.Println(psi.String())
}
