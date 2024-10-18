package ioctl

import (
	"fmt"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func (v WindowsVersion) IsHigherThan(other WindowsVersion) bool {
	if v.Major > other.Major {
		return true
	} else if v.Major < other.Major {
		return false
	}

	if v.Minor > other.Minor {
		return true
	} else if v.Minor < other.Minor {
		return false
	}

	if v.Build > other.Build {
		return true
	} else if v.Build < other.Build {
		return false
	}

	if v.Revision > other.Revision {
		return true
	} else if v.Revision < other.Revision {
		return false
	}

	// If Versions are equal
	return false
}

func (v WindowsVersion) IsLowerThan(other WindowsVersion) bool {
	return !v.IsHigherThan(other) && !v.IsEqualTo(other)
}

func (v WindowsVersion) IsEqualTo(other WindowsVersion) bool {
	return v.Major == other.Major && v.Minor == other.Minor && v.Build == other.Build && v.Revision == other.Revision
}

func (v WindowsVersion) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Build)
}

func QueryHostName() (name string, err error) {
	return windows.ComputerName()
}

// QueryProductName 查询产品名称.
// 例如: Windows 10 Enterprise
func QueryProductName() (name string, err error) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer key.Close()
	val, valType, err := key.GetStringValue("ProductName")
	if err != nil {
		return "", err
	}
	if valType != registry.SZ {
		return "", fmt.Errorf("unexpected value type: %d", valType)
	}
	return val, nil
}

func SystemManufacturer() string {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\BIOS`, registry.QUERY_VALUE)
	if err != nil {
		return "unknown"
	}
	defer key.Close()
	val, valType, err := key.GetStringValue("SystemManufacturer")
	if err != nil {
		return "unknown"
	}
	if valType != registry.SZ {
		return "unknown"
	}
	return val
}

func QueryWindowsVersion() (ver WindowsVersion, err error) {
	v := windows.RtlGetVersion()
	ver.Major = int(v.MajorVersion)
	ver.Minor = int(v.MinorVersion)
	ver.Build = int(v.BuildNumber)
	ver.Revision = 0
	return ver, nil
}

//func BCDEdit() (infos []map[string]string, err error) {
//	r, o, e := command.ExecV1("bcdedit")
//	if r != 0 {
//		return infos, errors.Errorf("failed to exec bcdedit. output(`%s`) error(`%s`)", o, e)
//	}
//	validLines := make([]string, 0)
//	for _, line := range strings.Split(o, "\n") {
//		line = strings.TrimSpace(line)
//		if line == "" {
//			continue
//		}
//		if strings.HasPrefix(line, "Windows") {
//			continue
//		}
//		validLines = append(validLines, line)
//	}
//	for _, line := range validLines {
//		if strings.HasPrefix(line, "-------------------") {
//			infos = append(infos, make(map[string]string))
//		}
//		lineItems := strings.Fields(line)
//		if len(lineItems) < 2 {
//			continue
//		}
//		key := lineItems[0]
//		val := strings.TrimSpace(strings.TrimPrefix(line, key))
//		infos[len(infos)-1][key] = val
//	}
//	return infos, nil
//}
//
//func QueryOSLoaderPath() (string, error) {
//	infos, err := BCDEdit()
//	if err != nil {
//		return "", err
//	}
//	for _, loaderInfo := range infos {
//		device, ok := loaderInfo["device"]
//		if !ok {
//			continue
//		}
//		systemroot, ok := loaderInfo["systemroot"]
//		if !ok {
//			continue
//		}
//		if strings.ToLower(device) == "partition=c:" && strings.ToLower(systemroot) == "\\windows" {
//			path, ok := loaderInfo["path"]
//			if ok {
//				return path, nil
//			}
//		}
//	}
//	return "", errors.Errorf("path of osloader not found")
//}

//func IsBootByUEFI() (bool, error) {
//	path, err := QueryOSLoaderPath()
//	if err != nil {
//		return false, err
//	}
//	return strings.HasSuffix(path, ".efi"), nil
//}
