package sysrepair

import (
	"regexp"
	"strings"
)

//
// Linux内建驱动
//

var (
	sysmapAtaRegex, _    = regexp.Compile(`.+\sata_init`)
	sysmapVirtioRegex, _ = regexp.Compile(`.+\svirtio_pci.*`)
	sysmapXenRegex, _    = regexp.Compile(`.+\sxen_platform_pci.*`)
)

func matchedAtaInSysmap(line string) bool {
	line = strings.TrimSpace(line)
	return sysmapAtaRegex.MatchString(line)
}

func matchedVirtioInSysmap(line string) bool {
	line = strings.TrimSpace(line)
	return sysmapVirtioRegex.MatchString(line)
}

func matchedXenInSysmap(line string) bool {
	line = strings.TrimSpace(line)
	return sysmapXenRegex.MatchString(line)
}
