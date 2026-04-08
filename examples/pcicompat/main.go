package main

import (
	"fmt"
	"log"
	"os"

	"github.com/kisun-bit/drpkg/ps/pci/universal"
)

func main() {
	compatDrivers, incompatPci, err := universal.SearchCompatibleLinuxModules(
		"/",
		os.Args[1],
		[]string{
			`PCI\V15ad\D07c0\SV15ad\SD07c0\BC01\SC07\I00\REV00`,
			`PCI\V15ad\D07e0\SV15ad\SD07e0\BC01\SC06\I01\REV00`,
			`PCI\V15ad\D0405\SV15ad\SD0405\BC03\SC00\I00\REV00`,
			`PCI\V8086\D1237\SV1af4\SD1100\BC06\SC00\I00\REV02`,
			`PCI\V8086\D7010\SV1af4\SD1100\BC01\SC01\I80\REV00`,

			`PCI\V1af4\D1001\SV1af4\SD0002\BC01\SC00\I00\REV00`,
			`PCI\V1af4\D1002\SV1af4\SD0005\BC00\SCff\I00\REV00`,
			`PCI\V1af4\D1000\SV1af4\SD0001\BC02\SC00\I00\REV00`,
			`PCI\V1af4\D1003\SV1af4\SD0003\BC07\SC80\I00\REV00`,
			`PCI\V1af4\D1004\SV1af4\SD0008\BC01\SC00\I00\REV00`,

			`PCI\V5853\D0001\SV5853\SD0001\BC01\SC00\I00\REV02`,
		},
	)

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("已兼容PCI列表：")
	for _, m := range compatDrivers {
		fmt.Println(m.Pci, m.Name)
	}
	fmt.Println("未兼容PCI列表：")
	for _, p := range incompatPci {
		fmt.Println(p)
	}
}
