package main

import (
	"fmt"
	"github.com/kisun-bit/drpkg/sys/info"
)

func main() {
	fmt.Println(info.NewSystemJsonInfo())
}
