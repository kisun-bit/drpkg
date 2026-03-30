package universal

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/kisun-bit/drpkg/ps/pci/ids"
	"github.com/pkg/errors"
)

var (
	//
	// windows PCI解析相关正则
	//

	vendorRegex    = regexp.MustCompile(`VEN_([0-9A-Fa-f]+)`)
	deviceRegex    = regexp.MustCompile(`DEV_([0-9A-Fa-f]+)`)
	subsystemRegex = regexp.MustCompile(`SUBSYS_([0-9A-Fa-f]+)`) // Subsystem Vendor + Subsystem Device
	classRegex     = regexp.MustCompile(`CC_([0-9A-Fa-f]+)`)     // Base Class + SubClass + Program Interface
	revisionRegex  = regexp.MustCompile(`REV_([0-9A-Fa-f]+)`)

	//
	// linux PCI解析相关正则
	//

	modaliasRegex = regexp.MustCompile(`pci:v([0-9A-Fa-f]{8})d([0-9A-Fa-f]{8})sv([0-9A-Fa-f]{8})sd([0-9A-Fa-f]{8})bc([0-9A-Fa-f]{2})sc([0-9A-Fa-f]{2})i([0-9A-Fa-f]{2})`)
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

func uniPciFromMsHardwareIds(hwIds []string) (p *UniPci, err error) {
	if len(hwIds) == 0 {
		return nil, errors.Errorf("hardware-id is empty")
	}

	p = new(UniPci)

	vendorStr := pciStringFromMsHardwareIds(hwIds, vendorRegex)
	p.vendorId, err = uint32FromString(vendorStr)
	if err != nil {
		return nil, err
	}

	deviceStr := pciStringFromMsHardwareIds(hwIds, deviceRegex)
	p.deviceId, err = uint32FromString(deviceStr)
	if err != nil {
		return nil, err
	}

	revStr := pciStringFromMsHardwareIds(hwIds, revisionRegex)
	p.revision, err = uint32FromString(revStr)
	if err != nil {
		return nil, err
	}

	subsystemStr := pciStringFromMsHardwareIds(hwIds, subsystemRegex)
	if len(subsystemStr) != 0 {
		if len(subsystemStr) != 8 {
			return nil, errors.Errorf("invalid subsystem length %d", len(subsystemStr))
		}
		subsystemVendorStr := subsystemStr[:4]
		p.subsystemVendorId, err = uint32FromString(subsystemVendorStr)
		if err != nil {
			return nil, err
		}
		subsystemDeviceStr := subsystemStr[4:8]
		p.subsystemDeviceId, err = uint32FromString(subsystemDeviceStr)
		if err != nil {
			return nil, err
		}
	}

	classStr := pciStringFromMsHardwareIds(hwIds, classRegex)
	if len(classStr) != 0 {
		if len(classStr) != 6 {
			return nil, errors.Errorf("invalid class length %d", len(classStr))
		}
		baseClassStr := classStr[:2]
		p.baseClass, err = uint32FromString(baseClassStr)
		if err != nil {
			return nil, err
		}
		subClassStr := classStr[2:4]
		p.subClass, err = uint32FromString(subClassStr)
		if err != nil {
			return nil, err
		}
		ifaceStr := classStr[4:6]
		p.programInterface, err = uint32FromString(ifaceStr)
		if err != nil {
			return nil, err
		}
	}

	return p, nil
}

func uniPciFromModaliasPath(path string) (p *UniPci, err error) {
	pb, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	modalias := strings.TrimSpace(string(pb))

	if !strings.HasPrefix(modalias, "pci:") {
		return nil, errors.Errorf("modalias of %s is not a pci identifier", path)
	}

	p, e := uniPciFromModalias(modalias)
	if e != nil {
		return nil, err
	}

	revisionPath := filepath.Join(filepath.Dir(path), "revision")
	if rev, e := os.ReadFile(revisionPath); e == nil {
		revStr := strings.TrimPrefix(
			strings.TrimSpace(
				strings.ToUpper(
					string(rev))),
			"0X")
		p.revision, _ = uint32FromString(revStr)
	}

	return p, nil
}

func uniPciFromModalias(modalias string) (p *UniPci, err error) {
	match := modaliasRegex.FindStringSubmatch(modalias)
	if len(match) == 0 {
		return nil, errors.Errorf("modalias is not valid")
	}

	p = new(UniPci)

	vendorStr := match[1]
	p.vendorId, err = uint32FromString(vendorStr)
	if err != nil {
		return nil, err
	}

	deviceStr := match[2]
	p.deviceId, err = uint32FromString(deviceStr)
	if err != nil {
		return nil, err
	}

	subVendorStr := match[3]
	p.subsystemVendorId, err = uint32FromString(subVendorStr)
	if err != nil {
		return nil, err
	}

	subDeviceStr := match[4]
	p.subsystemDeviceId, err = uint32FromString(subDeviceStr)
	if err != nil {
		return nil, err
	}

	baseClassStr := match[5]
	p.baseClass, err = uint32FromString(baseClassStr)
	if err != nil {
		return nil, err
	}

	subClassStr := match[6]
	p.subClass, err = uint32FromString(subClassStr)
	if err != nil {
		return nil, err
	}

	programInterfaceStr := match[7]
	p.programInterface, err = uint32FromString(programInterfaceStr)
	if err != nil {
		return nil, err
	}

	//
	// modalias里没有revision，因为Linux不靠revision去匹配驱动
	//

	return p, nil
}

func appendUniPci(uniPciList []*UniPci, uniPci *UniPci) []*UniPci {
	existed := false
	for _, cp := range uniPciList {
		if cp.Equals(uniPci) {
			existed = true
			break
		}
	}

	if !existed {
		uniPciList = append(uniPciList, uniPci)
	}

	return uniPciList
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

func pciStringFromMsHardwareIds(ids []string, regex *regexp.Regexp) string {
	for _, id := range ids {
		match := regex.FindStringSubmatch(id)
		if len(match) > 1 {
			return match[1]
		}
	}
	return ""
}
