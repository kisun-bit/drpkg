package info

import (
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

func QueryLinuxKernels(rootDir string) ([]LinuxKernel, error) {
	bootDirs := []string{
		filepath.Join(rootDir, "boot"),
		filepath.Join(rootDir, "rofs", "boot"),
	}
	libModuleDir := filepath.Join(rootDir, "lib", "modules")

	ds, err := os.ReadDir(libModuleDir)
	if err != nil {
		return nil, err
	}
	if len(ds) == 0 {
		return nil, errors.New("no kernels found")
	}

	kernelNames := make([]string, 0, len(ds))
	for _, d := range ds {
		kernelNames = append(kernelNames, d.Name())
	}
	sort.Strings(kernelNames)

	// 获取当前运行内核（只在 rootDir == "/" 时使用）
	var runningKernel string
	if rootDir == "/" {
		if uname, err := UnameR(); err == nil {
			runningKernel = strings.TrimSpace(uname)
		}
	}

	kernels := make([]LinuxKernel, 0, len(kernelNames))
	for _, name := range kernelNames {
		k := LinuxKernel{Name: name}

		k.Vmlinuz = firstExistingFile(bootDirs, "vmlinuz-"+name)
		k.SystemMap = firstExistingFile(bootDirs, "System.map-"+name)
		k.Config = firstExistingFile(bootDirs, "config-"+name)
		k.Initrd = firstExistingFile(bootDirs,
			"initrd.img-"+name,
			"initrd-"+name,
			"initramfs-"+name+".img",
		)

		if runningKernel != "" {
			k.Default = runningKernel == name
		} else if len(kernelNames) == 1 {
			k.Default = true
		}

		k.Bootable = k.Vmlinuz != "" && k.Initrd != ""
		kernels = append(kernels, k)
	}

	return kernels, nil
}

func firstExistingFile(dirs []string, candidates ...string) string {
	for _, d := range dirs {
		for _, f := range candidates {
			path := filepath.Join(d, f)
			if extend.IsExisted(path) {
				return f
			}
		}
	}
	return ""
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

//func QueryGRUB(root string, bootByEfi bool) (cfgPath string, grubVersion int, err error) {
//	if isUEFI && efiDirName == "" {
//		return "", 0, errors.New("when booting with UEFI, the efi directory must exist")
//	}
//
//	// NOTE:
//	// 通过配置文件路径确定grubVersion的版本是不健壮的.
//
//	offlineBootDir := filepath.Join(root, "boot")
//	offlineEFIBootDir := filepath.Join(root, "boot/efi/EFI", efiDirName)
//
//	cfgMaybe := ""
//	if isUEFI {
//		cfgMaybe = filepath.Join(offlineEFIBootDir, "grub.cfg")
//		if pathIsFile(cfgMaybe) {
//			return chrootPath(root, cfgMaybe), 2, nil
//		}
//		// 尝试从/etc/grub2-efi.cfg进行探测.
//		etcGRUBEFIFile := filepath.Join(root, "etc", "grub2-efi.cfg")
//		if path, e := resolveSymlink(etcGRUBEFIFile); e == nil {
//			return chrootPath(root, path), 2, nil
//		}
//		// 仍然未找到, 那么就以关键路径逐一猜测.
//	}
//
//	cfgMaybe = filepath.Join(offlineBootDir, "grub2", "grub.cfg")
//	if pathIsFile(cfgMaybe) {
//		return chrootPath(root, cfgMaybe), 2, nil
//	}
//
//	// ubuntu 系列是如此.
//	cfgMaybe = filepath.Join(offlineBootDir, "grub", "grub.cfg")
//	if pathIsFile(cfgMaybe) {
//		return chrootPath(root, cfgMaybe), 2, nil
//	}
//
//	// grub一定是Legacy版本.
//	cfgMaybe = filepath.Join(offlineBootDir, "grub", "grub.conf")
//	if pathIsFile(cfgMaybe) {
//		return chrootPath(root, cfgMaybe), 1, nil
//	}
//
//	// grub一定是Legacy版本.
//	cfgMaybe = filepath.Join(offlineBootDir, "grub", "menu.lst")
//	if pathIsFile(cfgMaybe) {
//		return chrootPath(root, cfgMaybe), 1, nil
//	}
//
//	// 尝试从/etc/grub2.cfg进行探测.
//	etcGRUB2File := filepath.Join(root, "etc", "grub2.cfg")
//	if path, e := resolveSymlink(etcGRUB2File); e == nil {
//		return chrootPath(root, path), 2, nil
//	}
//
//	// 尝试从/etc/grub.conf进行探测.
//	etcGRUBFile := filepath.Join(root, "etc", "grub.conf")
//	if path, e := resolveSymlink(etcGRUBFile); e == nil {
//		return chrootPath(root, path), 2, nil
//	}
//
//	return "", 0, errors.New("failed to find grub config file")
//}

func QueryWindowsRelease(_ string) (WindowsRelease, error) {
	return WindowsRelease{}, errors.New("not implemented on linux")
}
