package info

import (
	"golang.org/x/sys/windows"
	"os"
	"strings"

	"github.com/kisun-bit/drpkg/command"
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

func SupportCPUVirtual() bool {
	_, o, _ := command.Execute("wmic cpu get VirtualizationFirmwareEnabled")
	return strings.Contains(o, "TRUE")
}

func QueryWindowsRelease() (WindowsRelease, error) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE)
	if err != nil {
		return WindowsRelease{}, err
	}
	defer key.Close()
	val, valType, err := key.GetStringValue("ProductName")
	if err != nil {
		return WindowsRelease{}, err
	}
	if valType != registry.SZ {
		return WindowsRelease{}, errors.Errorf("unexpected value type: %d", valType)
	}

	w := WindowsRelease{}
	w.OsName = val
	w.Type = "client"
	if strings.Contains(strings.ToLower(val), "server") {
		w.Type = "server"
	}

	v := windows.RtlGetVersion()
	w.Version = WindowsVersion{
		Major: int(v.MajorVersion),
		Minor: int(v.MinorVersion),
		Build: int(v.BuildNumber),
	}
	return w, nil
}

// ListVolumeNames 获取所有的卷标.
func ListVolumeNames() (volumeNames []string, err error) {
	buf := make([]uint16, 254)
	n, e := windows.GetLogicalDriveStrings(254, &buf[0])
	if err != nil {
		return nil, e
	}
	for _, v := range buf[:n] {
		letter := string(rune(v))
		if len(letter) == 0 {
			continue
		}
		if letter[0] <= 'A' || letter[0] > 'Z' {
			continue
		}
		volumeNames = append(volumeNames, letter)
	}
	return volumeNames, nil
}
