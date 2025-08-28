package info

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/kisun-bit/drpkg/command"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/pkg/errors"
)

func IsMemoryOS() bool {
	_, o, _ := command.Execute(`mount | grep 'on / ' | awk '{print $5}'`)
	if strings.Contains(o, "tmpfs") || strings.Contains(o, "overlay") {
		return true
	}
	_, o, _ = command.Execute(`mount | grep 'on /run/initramfs/live ' | awk '{print $5}'`)
	if strings.Contains(o, "iso9660") {
		return true
	}
	if _, eStat := os.Stat("/etc/initrd-release"); eStat == nil {
		return true
	}
	return false
}

func UnameR() (string, error) {
	_, o, e := command.Execute("uname -r")
	if e != nil {
		return "", errors.Wrap(e, "uname -r")
	}
	return strings.TrimSpace(o), nil
}

func SystemManufacturer() string {
	r, out, _ := command.Execute("dmidecode -s system-manufacturer")
	if r == 0 {
		return strings.TrimSpace(out)
	}
	dmi, err := QueryDmi()
	if err != nil {
		return ""
	}
	return dmi.SystemName
}

func QueryLinuxKernels(rootDir string) ([]LinuxKernel, error) {
	bootDir := filepath.Join(rootDir, "boot")
	libModuleDir := filepath.Join(rootDir, "lib", "modules")

	ds, err := os.ReadDir(libModuleDir)
	if err != nil {
		return nil, err
	}

	kernelNames := make([]string, 0)
	for _, d := range ds {
		kernelNames = append(kernelNames, d.Name())
	}
	if len(kernelNames) == 0 {
		return nil, errors.New("no kernels found")
	}
	sort.Strings(kernelNames)

	kernels := make([]LinuxKernel, 0)
	for _, kernel := range kernelNames {
		k := LinuxKernel{}

		k.Name = kernel
		k.Vmlinuz = extend.FilenameIfExisted(filepath.Join(bootDir, fmt.Sprintf("vmlinuz-%s", k.Name)))
		k.SystemMap = extend.FilenameIfExisted(filepath.Join(bootDir, fmt.Sprintf("System.map-%s", k.Name)))
		k.Config = extend.FilenameIfExisted(filepath.Join(bootDir, fmt.Sprintf("config-%s", k.Name)))

		initrdSet := make([]string, 0)
		initrdSet = append(initrdSet, fmt.Sprintf("initrd.img-%s", k.Name))
		initrdSet = append(initrdSet, fmt.Sprintf("initrd-%s", k.Name))
		initrdSet = append(initrdSet, fmt.Sprintf("initramfs-%s.img", k.Name))
		// FIXME: 待补充其他initrd风格......

		for _, initrd := range initrdSet {
			path := filepath.Join(bootDir, initrd)
			//if others.IsExisted(path) || others.IsLinkTargetExisted(bootDir, initrd, false) {
			if extend.IsExisted(path) {
				k.Initrd = initrd
				break
			}
		}

		if rootDir == "/" {
			kernelStr, e := UnameR()
			if e != nil {
				return nil, e
			}
			k.Default = strings.TrimSpace(kernelStr) == k.Name
		} else {
			if len(kernelNames) == 1 {
				k.Default = true
			}
		}

		k.Bootable = k.Vmlinuz != "" && k.Initrd != ""

		kernels = append(kernels, k)
	}

	return kernels, nil
}

func QueryLinuxRelease(rootDir string) LinuxRelease {
	releaseFileGlob := filepath.Join(rootDir, "etc", "*release")
	releaseOutput := extend.GlobReadFiles(releaseFileGlob)

	prefixID := "ID"
	prefixVERSIONID := "VERSION_ID"

	formatValueFunc := func(_value string) string {
		return strings.TrimSuffix(strings.TrimPrefix(_value, "\""), "\"")
	}
	extractVersion := func(input string) string {
		re := regexp.MustCompile(`\d+(\.\d+)*\b`)
		version := re.FindString(input)
		return version
	}

	lr := LinuxRelease{}

	for _, line := range strings.Split(releaseOutput, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "=") {
			continue
		}
		lineItems := strings.Split(line, "=")
		if len(lineItems) < 2 {
			continue
		}
		switch lineItems[0] {
		case prefixID:
			lr.ReleaseID = formatValueFunc(lineItems[1])
		case prefixVERSIONID:
			lr.Version = formatValueFunc(lineItems[1])
		}
	}

	if lr.ReleaseID == "" && lr.Version == "" && releaseOutput != "" {
		for _, line := range strings.Split(releaseOutput, "\n") {
			line = strings.ToLower(strings.TrimSpace(line))
			if !strings.Contains(line, "release") {
				continue
			}
			lr.ReleaseID = "redhat"
			lr.Version = extractVersion(line)

			isCentos := strings.HasPrefix(line, "centos")
			if isCentos {
				lr.ReleaseID = "centos"
			}
			// FIXME: 识别更多低版本Linux
		}
	}

	lr.Distro = lr.ReleaseID + lr.Version
	return lr
}

func QuerySwapInfo() (ss []LinuxSwap, err error) {
	bs, err := os.ReadFile("/proc/swaps")
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(bs), "\n") {
		lineItems := strings.Fields(line)
		if len(lineItems) < 5 || len(lineItems) > 0 && !strings.HasPrefix(lineItems[0], "/") {
			continue
		}
		s := LinuxSwap{
			Filename: lineItems[0],
			Type:     lineItems[1],
			Size:     extend.MustInt64(lineItems[2]) * 1024,
			Used:     extend.MustInt64(lineItems[3]) * 1024,
			Priority: int(extend.MustInt64(lineItems[4])),
		}
		if strings.HasPrefix(s.Filename, "/dev") {
			s.Device = s.Filename
			s.UUID = extend.MatchDevLinkName("/dev/disk/by-uuid", filepath.Base(s.Filename))
			s.Label = extend.MatchDevLinkName("/dev/disk/by-label", filepath.Base(s.Filename))
			if strings.HasPrefix(filepath.Base(s.Filename), "dm-") {
				dmName := extend.MatchDevLinkName("/dev/mapper", filepath.Base(s.Filename))
				s.Device = filepath.Join("/dev/mapper", dmName)
			}
		}
		ss = append(ss, s)
	}
	return ss, nil
}

// SupportCPUVirtual 是否支持CPU虚拟化
// 如Intel VT-x、AMD-V
func SupportCPUVirtual() bool {
	_, out, _ := command.Execute("egrep -o 'vmx|svm' /proc/cpuinfo")
	return strings.Contains(out, "vmx") || strings.Contains(out, "svm")
}

func QueryEfiDir(rootDir string, releaseID string) string {
	defaultEFIDir := filepath.Join(rootDir, "boot/efi/EFI")
	if !extend.IsExisted(defaultEFIDir) {
		return ""
	}
	des, err := os.ReadDir(defaultEFIDir)
	if err != nil {
		return ""
	}
	for _, d := range des {
		if len(des) == 1 {
			return filepath.Join(defaultEFIDir, d.Name())
		}
		if len(des) == 2 {
			if strings.ToLower(d.Name()) != "boot" {
				return filepath.Join(defaultEFIDir, d.Name())
			}
			continue
		}
		if strings.ToLower(releaseID) == strings.ToLower(d.Name()) {
			return filepath.Join(defaultEFIDir, d.Name())
		}
	}
	return ""
}

func QueryGRUB(root string, bootByEfi bool) (cfgPath string, grubVersion int, err error) {
	if isUEFI && efiDirName == "" {
		return "", 0, errors.New("when booting with UEFI, the efi directory must exist")
	}

	// NOTE:
	// 通过配置文件路径确定grubVersion的版本是不健壮的.

	offlineBootDir := filepath.Join(root, "boot")
	offlineEFIBootDir := filepath.Join(root, "boot/efi/EFI", efiDirName)

	cfgMaybe := ""
	if isUEFI {
		cfgMaybe = filepath.Join(offlineEFIBootDir, "grub.cfg")
		if pathIsFile(cfgMaybe) {
			return chrootPath(root, cfgMaybe), 2, nil
		}
		// 尝试从/etc/grub2-efi.cfg进行探测.
		etcGRUBEFIFile := filepath.Join(root, "etc", "grub2-efi.cfg")
		if path, e := resolveSymlink(etcGRUBEFIFile); e == nil {
			return chrootPath(root, path), 2, nil
		}
		// 仍然未找到, 那么就以关键路径逐一猜测.
	}

	cfgMaybe = filepath.Join(offlineBootDir, "grub2", "grub.cfg")
	if pathIsFile(cfgMaybe) {
		return chrootPath(root, cfgMaybe), 2, nil
	}

	// ubuntu 系列是如此.
	cfgMaybe = filepath.Join(offlineBootDir, "grub", "grub.cfg")
	if pathIsFile(cfgMaybe) {
		return chrootPath(root, cfgMaybe), 2, nil
	}

	// grub一定是Legacy版本.
	cfgMaybe = filepath.Join(offlineBootDir, "grub", "grub.conf")
	if pathIsFile(cfgMaybe) {
		return chrootPath(root, cfgMaybe), 1, nil
	}

	// grub一定是Legacy版本.
	cfgMaybe = filepath.Join(offlineBootDir, "grub", "menu.lst")
	if pathIsFile(cfgMaybe) {
		return chrootPath(root, cfgMaybe), 1, nil
	}

	// 尝试从/etc/grub2.cfg进行探测.
	etcGRUB2File := filepath.Join(root, "etc", "grub2.cfg")
	if path, e := resolveSymlink(etcGRUB2File); e == nil {
		return chrootPath(root, path), 2, nil
	}

	// 尝试从/etc/grub.conf进行探测.
	etcGRUBFile := filepath.Join(root, "etc", "grub.conf")
	if path, e := resolveSymlink(etcGRUBFile); e == nil {
		return chrootPath(root, path), 2, nil
	}

	return "", 0, errors.New("failed to find grub config file")
}
