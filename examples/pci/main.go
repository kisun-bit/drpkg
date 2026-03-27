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
		fmt.Printf("[%02d] %s | %s\n", i, p, p.Human())
		p2, e := universal.UniPciFromString(p.String())
		if e != nil {
			logger.Fatalf("UniPciFromString: %v", err)
		}
		if !p.Equals(p2) {
			logger.Fatalf("UniPciFromString: expected %s, got %s", p, p2)
		}
	}
}
