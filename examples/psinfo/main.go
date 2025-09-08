package main

import (
	"log"

	"github.com/davecgh/go-spew/spew"
	"github.com/kisun-bit/drpkg/ps/info"
)

func main() {
	psi, err := info.QueryPSInfo()
	if err != nil {
		log.Fatalln(err)
		return
	}
	spew.Dump(psi.Public.Volumes)
}
