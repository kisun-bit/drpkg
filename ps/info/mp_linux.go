package info

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kisun-bit/drpkg/extend"
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
		if !strings.HasPrefix(string(uuidBody), "mpath-") {
			continue
		}
		dmNamePath := filepath.Join(filepath.Dir(dmPath), "name")
		nameBody, err := os.ReadFile(dmNamePath)
		if err != nil {
			return nil, err
		}
		dmName := strings.TrimSpace(string(nameBody))
		devicePath := fmt.Sprintf("/dev/mapper/%s", dmName)

		mp := MultipathDevice{}
		mp.Device = devicePath

		size, err := extend.FileSize(devicePath)
		if err != nil {
			return nil, err
		}
		mp.Size = int64(size)

		ss, err := extend.MultipathSegments(devicePath)
		if err != nil {
			return nil, err
		}

		for _, s := range ss {
			mp.Disks = append(mp.Disks, s.Device)
		}

		mps = append(mps, mp)
	}

	return mps, nil
}
