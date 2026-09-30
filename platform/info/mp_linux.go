package info

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kisun-bit/drpkg/xutil"
)

func QueryMultipath() ([]MultipathDevice, error) {
	dmPathList, err := filepath.Glob("/sys/block/*/dm/uuid")
	if err != nil {
		return nil, err
	}

	mps := make([]MultipathDevice, 0)

	for _, dmPath := range dmPathList {
		uuidBody, err := os.ReadFile(dmPath)
		if err != nil {
			return nil, err
		}
		// dm uuid 形如 mpath-3600508b...，mpath- 前缀标记其为 multipath 设备。
		mpUUID := strings.TrimSpace(string(uuidBody))
		if !strings.HasPrefix(mpUUID, "mpath-") {
			continue
		}
		dmNamePath := filepath.Join(filepath.Dir(dmPath), "name")
		nameBody, err := os.ReadFile(dmNamePath)
		if err != nil {
			return nil, err
		}
		dmName := strings.TrimSpace(string(nameBody))
		devicePath := fmt.Sprintf("/dev/mapper/%s", dmName)
		//dp, err := filepath.EvalSymlinks(devicePath)
		//if err != nil {
		//	return nil, err
		//}

		mp := MultipathDevice{}
		mp.Name = dmName
		mp.Device = devicePath
		mp.UUID = mpUUID

		size, err := xutil.FileSize(mp.Device)
		if err != nil {
			return nil, err
		}
		mp.Size = int64(size)

		mp.Table, err = GetDiskTable(mp.Device)
		if err != nil {
			return nil, err
		}

		ss, err := xutil.MultipathSegments(mp.Device)
		if err != nil {
			return nil, err
		}

		for _, s := range ss {
			mp.Slaves = append(mp.Slaves, s.Device)
		}

		mps = append(mps, mp)
	}

	return mps, nil
}
