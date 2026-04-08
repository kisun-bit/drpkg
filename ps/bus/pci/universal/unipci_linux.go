package universal

import (
	"path/filepath"
)

//
// 举例真实的硬件Id：
// pci:v00008086d00000E00sv00001028sd000004CEbc06sc00i00
//

func listUniPci() ([]*UniPci, error) {
	pciPathList, err := filepath.Glob("/sys/bus/pci/devices/*/modalias")
	if err != nil {
		return nil, err
	}

	ps := make([]*UniPci, 0)
	for _, pciPath := range pciPathList {
		p, e := uniPciFromModaliasPath(pciPath)
		if e != nil {
			return nil, e
		}
		ps = appendUniPci(ps, p)
	}

	return ps, nil
}
