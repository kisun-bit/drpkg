package main

import (
	"fmt"
	"log"
	"time"

	"github.com/kisun-bit/drpkg/ps/info"
)

func main() {
	psi, err := info.QueryPSInfo()
	if err != nil {
		log.Fatalln(err)
		return
	}
	fmt.Println(psi.PrettyString())

	for {
		time.Sleep(1 * time.Second)
	}
}
