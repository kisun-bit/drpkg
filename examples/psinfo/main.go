package main

import (
	"fmt"
	"log"

	"github.com/kisun-bit/drpkg/ps/info"
)

func main() {
	psi, err := info.QueryPSInfo()
	if err != nil {
		log.Fatalln(err)
		return
	}
	fmt.Println(psi.String())
}
