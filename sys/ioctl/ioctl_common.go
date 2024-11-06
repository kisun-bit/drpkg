package ioctl

import "strings"

func IsVirtualGuest() bool {
	lowerManu := strings.ToLower(SystemManufacturer())

	isVmware := strings.Contains(lowerManu, "vmware, inc.")
	isQemu := strings.Contains(lowerManu, "qemu")
	isXen := strings.Contains(lowerManu, "xen")
	isHyperV := strings.Contains(lowerManu, "microsoft corporation")
	isVitualBox := strings.Contains(lowerManu, "oracle corporation")
	isKVM := strings.Contains(lowerManu, "red hat")
	isGoogleCloud := strings.Contains(lowerManu, "google")
	isAmazonEC2 := strings.Contains(lowerManu, "amazon ec2")

	// TODO 更多
	return isVmware || isQemu || isXen || isHyperV || isVitualBox || isGoogleCloud || isAmazonEC2 || isKVM
}
