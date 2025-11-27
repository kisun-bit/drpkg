package main

import (
	"fmt"

	"github.com/kisun-bit/drpkg/extend"
)

func main() {
	//psi, err := info.QueryPSInfo()
	//if err != nil {
	//	log.Fatal(err)
	//	return
	//}
	////_ = psi
	//fmt.Println(psi.PrettyString())

	fmt.Println(extend.QueryDosDevice("C:"))
}
