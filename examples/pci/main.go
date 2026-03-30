package main

import (
	"fmt"

	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/ps/pci/universal"
)

func main() {
	ps, err := universal.ListUniPci()
	if err != nil {
		logger.Fatalf("ListUniPci: %v", err)
	}

	for i, p := range ps {

		//// 仅保留存储、网络等驱动，业务场景不必这么做
		//if p.BaseClassId() != 0x01 && p.BaseClassId() != 0x02 {
		//	continue
		//}

		fmt.Printf("[%03d] %s | %s\n", i, p, p.Human())
		p2, e := universal.UniPciFromString(p.String())
		if e != nil {
			logger.Fatalf("UniPciFromString: %v", err)
		}
		if !p.Equals(p2) {
			logger.Fatalf("UniPciFromString: expected %s, got %s", p, p2)
		}
	}
}
