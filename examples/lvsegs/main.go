package main

import (
	"fmt"
	"log"
	"os"

	"github.com/kisun-bit/drpkg/extend"
)

func main() {
	segs, err := extend.LVSegments(os.Args[1])
	if err != nil {
		log.Panic(err)
	}
	fmt.Println(extend.Pretty(segs))
}
