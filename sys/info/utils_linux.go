package info

import (
	"github.com/kisun-bit/drpkg/sys/ioctl"
	"github.com/pkg/errors"
	"github.com/tidwall/gjson"
	"path/filepath"
)

func calculateUniqueID(storage_ gjson.Result, storageType int) (uniqueID string, err error) {
	_devicePath := storage_.Get("disk_path").Str
	if _devicePath == "" {
		_devicePath = storage_.Get("volume_path").Str
	}
	switch storageType {
	case _storageTypeLinuxDisk:
		uniqueID = ioctl.MatchDiskBy(ioctl.DevDiskByPath, filepath.Base(_devicePath))
	case _storageTypeLinuxLVM:
		uniqueID = storage_.Get("uuid").Str
	default:
		return "", errors.Errorf("unhandled default case")
	}
	if uniqueID == "" {
		return "", errors.Errorf("unique id of %s", _devicePath)
	}
	return uniqueID, nil
}
