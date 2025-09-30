package main

import (
	"log"

	"github.com/kisun-bit/drpkg/ps/info"
)

func main() {
	psi, err := info.QueryPSInfo()
	if err != nil {
		log.Fatal(err)
		return
	}
	_ = psi
	//fmt.Println(psi.PrettyString())
}
