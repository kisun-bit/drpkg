package universal

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kisun-bit/drpkg/ps/pci/ids"
)

func Lookup(baseClass, vendor, device uint16) (baseClassStr string, vendorStr string, deviceStr string) {
	for _, v := range ids.IDs {
		if v.ID != vendor {
			continue
		}
		for _, d := range v.Devices {
			if d.ID != device {
				continue
			}
			vendorStr, deviceStr = v.Name, d.Name
			break
		}
		vendorStr = v.Name
		break
	}

	if v, ok := ids.Classes[uint32(baseClass)]; ok {
		baseClassStr = v
	}

	switch {
	case vendorStr == "":
		vendorStr = fmt.Sprintf("%04x", vendor)
	case deviceStr == "":
		deviceStr = fmt.Sprintf("%04x", device)
	case baseClassStr == "":
		baseClassStr = fmt.Sprintf("%04x", baseClass)
	}

	return
}

func uint32FromString(s string) (uint32, error) {
	r, e := strconv.ParseUint(
		trimLeadingZeros(s),
		16,
		32)
	return uint32(r), e
}

func trimLeadingZeros(s string) string {
	result := strings.TrimLeft(s, "0")
	if result == "" {
		return "0"
	}
	return result
}
