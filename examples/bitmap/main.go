package main

import (
	"fmt"
	"os"

	"github.com/kisun-bit/drpkg/extend"
)

func main() {
	if err := extend.CreateHiddenFile(os.Args[1], 129<<20); err != nil {
		panic(err)
	}
	fmt.Println("success")
}
