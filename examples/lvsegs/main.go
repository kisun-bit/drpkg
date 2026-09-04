//go:build linux

package main

import (
	"fmt"
	"log"
	"os"

	"github.com/kisun-bit/drpkg/xutil"
)

func main() {
	segs, err := xutil.LVSegments(os.Args[1])
	if err != nil {
		log.Panic(err)
	}
	fmt.Println(xutil.Pretty(segs))
}
