package biotrkmeta

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/kisun-bit/drpkg/platform/info"
	"github.com/kisun-bit/drpkg/xutil"
	"github.com/pkg/errors"
)

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

func trimZeroString(data []byte) string {
	n := len(data)
	for n > 0 && data[n-1] == 0 {
		n--
	}
	return string(data[:n])
}
