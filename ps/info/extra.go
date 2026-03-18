package info

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/ps/efi"
)

type LinuxKernel struct {
	Name      string `json:"name"`
	Vmlinuz   string `json:"vmlinuz"`
	SystemMap string `json:"systemMap"`
	Config    string `json:"config"`
	Initrd    string `json:"initrd"`
	Bootable  bool   `json:"bootable"`
	Default   bool   `json:"default"`
}

type LinuxRelease struct {
	Distro    string `json:"distro"`
	ReleaseID string `json:"releaseId"`
	Version   string `json:"version"`
}

type LinuxSwap struct {
	Filename string `json:"filename"`
	Device   string `json:"device"`
	Type     string `json:"type"`
	Size     int64  `json:"size"`
	Used     int64  `json:"used"`
	Priority int    `json:"priority"`
	UUID     string `json:"uuid"`
	Label    string `json:"label"`
}

type WindowsRelease struct {
	OsName  string         `json:"osName"`
	Type    string         `json:"type"` // 可取client、server之一
	Version WindowsVersion `json:"version"`
}

type WindowsVersion struct {
	Major int `json:"major"`
	Minor int `json:"minor"`
	Build int `json:"build"`
}

type EFI struct {
	Effective   bool   `json:"effective"`
	BootCurrent string `json:"bootCurrent"`
	BootFile    string `json:"bootFile"`
}

func IsVirtualHost(manufacturer string) bool {
	lowerManu := strings.ToLower(manufacturer)

	if lowerManu == "" {
		return true
	}

	virtualVendorList := []string{
		"vmware",
		"qemu",
		"xen",
		"openstack",
		"kvm",
		// FIXME 更多
	}

	for _, v := range virtualVendorList {
		if strings.Contains(lowerManu, v) {
			return true
		}
	}

	return false
}

func QueryBootType() BootType {
	vars, e := efi.GetEfiVariables()
	if e == nil && len(vars) > 0 {
		return "uefi"
	}

	if runtime.GOOS == "linux" {
		if _, err := os.Stat("/sys/firmware/efi"); err == nil {
			return "uefi"
		}
	}

	return "bios"
}

func QueryEFIInfo() (e EFI, err error) {
	vars, err := efi.GetEfiVariables()
	if err != nil {
		return e, err
	}

	// 统一处理 BootXXXX
	resolveBootEntry := func(namespace string, entry uint16) (string, error) {
		name := efi.BootEntryName(entry)
		data, err := efi.GetEfiVariableValue(namespace, name)
		if err != nil {
			return "", err
		}
		text, err := efi.DecodeUTF16(data)
		if err != nil {
			return "", err
		}
		path, _ := efi.MatchUEFIPath(text)
		return path, nil
	}

	// ===============================
	// 1. 优先从 BootCurrent 查
	// ===============================
	for _, v := range vars {
		if v.Name != "BootCurrent" {
			continue
		}

		data, err := efi.GetEfiVariableValue(v.Namespace, v.Name)
		if err != nil {
			return e, err
		}

		if len(data) == 6 { // 某些固件前4字节是属性
			data = data[4:]
		}

		cur, err := efi.BytesToU16(data)
		if err != nil {
			return e, err
		}

		e.BootCurrent = efi.BootEntryName(cur)
		e.BootFile, err = resolveBootEntry(v.Namespace, cur)
		if err != nil {
			return e, err
		}

		//
		// Note:
		// 1) 只有 从磁盘文件系统启动的 EFI 启动项 才会在 NVRAM 里带上 .efi 路径
		// 2) CDROM、PXE 网络、固件内置 Shell 等启动方式，启动项只是一个设备/固件指针，不会记录 .efi 文件路径，
		// 3) 对于光驱（Boot0001），UEFI 固件会自动去光盘的 \EFI\BOOT\BOOTX64.EFI 搜索，而不是写死在 NVRAM 启动项里
		// \EFI\BOOT\BOOTX64.EFI是 UEFI 规范定义的默认启动程序
		//
		// 示例（LiveCD）：
		// [root@RunStor ~]# efibootmgr -v
		// BootCurrent: 0001
		// BootOrder: 0004,0000,0001,0002,0003
		// Boot0000* EFI Virtual disk (0.0)        PciRoot(0x0)/Pci(0x15,0x0)/Pci(0x0,0x0)/SCSI(0,0)
		// Boot0001* EFI VMware Virtual SATA CDROM Drive (0.0)     PciRoot(0x0)/Pci(0x11,0x0)/Pci(0x0,0x0)/Sata(0,0,0)
		// Boot0002* EFI Network   PciRoot(0x0)/Pci(0x16,0x0)/Pci(0x0,0x0)/MAC(005056ac730b,1)
		// Boot0003* EFI Internal Shell (Unsupported option)       MemoryMapped(11,0xefe6018,0xf3f5017)/FvFile(c57ad6b7-0515-40a8-9d21-551652854e37)
		// Boot0004* CentOS        HD(1,GPT,2c396c7a-a5b0-437d-85dc-fa2d3239e53d,0x800,0x64000)/File(\EFI\centos\shimx64.efi)
		// ...

		if e.BootFile == "" {
			fillDefaultBootFile(&e)
		}
		e.Effective = e.BootFile == ""
	}

	if e.Effective {
		return e, nil
	}

	// ===============================
	// 2. fallback：从 BootOrder 查第一个
	// ===============================
	for _, v := range vars {
		if v.Name != "BootOrder" {
			continue
		}

		data, err := efi.GetEfiVariableValue(v.Namespace, v.Name)
		if err != nil {
			return e, err
		}
		if len(data) < 2 {
			break
		}

		cur, err := efi.BytesToU16(data[:2])
		if err != nil {
			return e, err
		}

		e.BootCurrent = efi.BootEntryName(cur)
		e.BootFile, _ = resolveBootEntry(v.Namespace, cur)

		if e.BootFile == "" {
			fillDefaultBootFile(&e)
		}
		e.Effective = e.BootFile == ""
	}

	return e, nil
}

func fillDefaultBootFile(e *EFI) {
	if !extend.IsWindowsPlatform() {
		if matches, _ := filepath.Glob("/boot/efi/EFI/BOOT/BOOT*.EFI"); len(matches) > 0 {
			e.BootFile = strings.TrimPrefix(matches[0], "/boot/efi")
		}
		return
	}
}
