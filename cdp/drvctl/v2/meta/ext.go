package meta

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/kisun-bit/drpkg/disk/table"
	"github.com/kisun-bit/drpkg/platform/info"
	"github.com/kisun-bit/drpkg/xutil"
	"github.com/pkg/errors"
)

type DiskPartTableData struct {
	Offset int64
	Data   []byte
}

func ReadDiskPartTableData(diskPath string) (dataList []DiskPartTableData, err error) {
	dt, err := table.GetDiskType(diskPath)
	if err != nil {
		return dataList, err
	}

	switch dt {
	case table.TableTypeMBR:
		// 主引导记录
		mbr, err := table.NewMBR(diskPath, 0, false)
		if err != nil {
			return dataList, err
		}
		defer mbr.Close()
		dataList = append(dataList, DiskPartTableData{
			Offset: 0,
			Data:   mbr.Bin,
		})
		// EBR记录
		ebrs, err := mbr.EBRList()
		if err != nil {
			return dataList, err
		}
		for _, ebr := range ebrs {
			dataList = append(dataList, DiskPartTableData{
				Offset: ebr.Offset,
				Data:   ebr.Bin,
			})
		}
		return dataList, nil
	case table.TableTypeGPT:
		// 主分区表
		gpt, err := table.NewGPT(diskPath, 0)
		if err != nil {
			return dataList, err
		}
		defer gpt.Close()
		dataList = append(dataList, DiskPartTableData{
			Offset: 0,
			Data:   gpt.Bin,
		})
		// 备份分区表
		bgpt, err := gpt.BackupGPT()
		if err != nil {
			return dataList, err
		}
		dataList = append(dataList, DiskPartTableData{
			Offset: bgpt.Offset,
			Data:   bgpt.Bin,
		})
		return dataList, nil
	default:
		return dataList, nil
	}
}

func getWindowsDiskPath(pnpDeviceId string) (string, error) {
	disks, err := info.QueryDisks()
	if err != nil {
		return "", err
	}
	for _, dev := range disks {
		if dev.PathId == pnpDeviceId {
			return dev.Device, nil
		}
	}
	return "", errors.Errorf("disk(id:%s) not found", pnpDeviceId)
}

// getWindowsPnpDeviceId 由 Windows 设备路径（如 \\.\PHYSICALDRIVE0）反查 PNPDeviceID。
// info.QueryDisks 返回的 Device 大小写可能与调用方的路径不同，故用 EqualFold 匹配。
func getWindowsPnpDeviceId(devicePath string) (string, error) {
	disks, err := info.QueryDisks()
	if err != nil {
		return "", err
	}
	for _, dev := range disks {
		if strings.EqualFold(dev.Device, devicePath) {
			return dev.PathId, nil
		}
	}
	return "", errors.Errorf("disk(path:%s) not found", devicePath)
}

// getPhysicalDiskID 将物理磁盘路径转换为 DiskID 的 ID 字段内容。
//   - Windows：路径 → PNPDeviceID（与 DiskID 契约一致）
//   - Linux  ：路径 → 稳定名称（按 by-path → by-id → mapper → dev 优先级回退）
func getPhysicalDiskID(devicePath string) (string, error) {
	if runtime.GOOS == "windows" {
		return getWindowsPnpDeviceId(devicePath)
	}
	return getLinuxDiskName(devicePath)
}

// getLinuxDiskName 将 Linux 设备路径（如 /dev/sda）反查为 DiskID.ID 的稳定名称。
//
// 与 getLinuxDiskPath 互逆：getLinuxDiskPath(name) 再把名称解析回设备路径。
// 按 /dev/disk/by-path → /dev/disk/by-id → /dev/mapper → /dev 的优先级，
// 返回第一个能解析到目标设备的目录条目名（不含目录前缀），最终回退到设备基础名。
func getLinuxDiskName(devicePath string) (string, error) {
	canonical, err := canonicalDevPath(devicePath)
	if err != nil {
		return "", err
	}

	for _, dir := range []string{"/dev/disk/by-path", "/dev/disk/by-id"} {
		if name, ok := findDevEntry(dir, canonical, false); ok {
			return name, nil
		}
	}

	// /dev/mapper：条目须是 multipath 设备（dm uuid 以 mpath- 开头），排除 LVM 卷。
	if name, ok := findDevEntry("/dev/mapper", canonical, true); ok {
		return name, nil
	}

	return filepath.Base(canonical), nil
}

// findDevEntry 在 dir 中查找解析后目标等于 want 的目录条目，返回其条目名。
// mpath 为 true 时还要求 resolveMpath 校验通过（multipath 设备）。
//
// os.ReadDir 已按文件名排序，因此扫描顺序是确定的。
func findDevEntry(dir, want string, mpath bool) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		p := filepath.Join(dir, e.Name())
		target, err := canonicalDevPath(p)
		if err != nil {
			continue
		}
		if target != want {
			continue
		}
		if mpath {
			if _, err := resolveMpath(p); err != nil {
				continue
			}
		}
		return e.Name(), true
	}
	return "", false
}

// canonicalDevPath 将设备路径规范化为 "/dev/<base>" 形式（解析符号链接）。
// 与 resolveSymlink 返回的形式一致，便于比较两个设备路径是否指向同一设备。
func canonicalDevPath(path string) (string, error) {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return filepath.Join("/dev", filepath.Base(target)), nil
}

func getLinuxDiskPath(name string) (string, error) {
	type rule struct {
		baseDir string
		resolve func(string) (string, error)
	}

	rules := []rule{
		{"/dev/disk/by-path", resolveSymlink},
		{"/dev/disk/by-id", resolveSymlink},
		{"/dev/mapper", resolveMpath},
		{"/dev", resolveDirect},
	}

	for _, r := range rules {
		path := filepath.Join(r.baseDir, name)
		if !xutil.IsExisted(path) {
			continue
		}

		return r.resolve(path)
	}

	return "", errors.New("disk not found")
}

func resolveSymlink(path string) (string, error) {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return filepath.Join("/dev", filepath.Base(target)), nil
}

func resolveMpath(path string) (string, error) {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}

	uuidPath := filepath.Join("/sys/block", filepath.Base(target), "dm", "uuid")
	data, err := os.ReadFile(uuidPath)
	if err != nil {
		return "", err
	}

	if !strings.HasPrefix(strings.TrimSpace(string(data)), "mpath-") {
		return "", errors.Errorf("invalid mpath uuid: %s", uuidPath)
	}

	return path, nil
}

func resolveDirect(path string) (string, error) {
	return path, nil
}
