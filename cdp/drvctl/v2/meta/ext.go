package meta

import (
	"os"
	"path/filepath"
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
