package info

import (
	"os"
	"strings"

	"github.com/pkg/errors"
	"github.com/shirou/gopsutil/v3/process"
	"golang.org/x/sys/windows/registry"
)

func IsMemoryOS() bool {
	pss, err := process.Processes()
	if err != nil {
		_, e := os.Stat("X:\\windows\\system32\\winpeshl.exe")
		return e == nil
	}
	for _, p := range pss {
		name, _ := p.Name()
		if strings.Contains(name, "winpeshl.exe") {
			return true
		}
	}
	return false
}

func QueryLinuxKernels(_ string) ([]LinuxKernel, error) {
	return []LinuxKernel{}, nil
}

func UnameR() (string, error) {
	return "", errors.New("not implemented")
}

func QueryLinuxRelease(rootDir string) LinuxRelease {
	return LinuxRelease{}
}

func SystemManufacturer() string {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\BIOS`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer key.Close()
	val, valType, err := key.GetStringValue("SystemManufacturer")
	if err != nil {
		return ""
	}
	if valType != registry.SZ {
		return ""
	}
	return val
}

func QuerySwapInfo() (_ []LinuxSwap, _ error) {
	return nil, nil
}
