package info

import (
	"fmt"
	"github.com/pkg/errors"
	"github.com/tidwall/gjson"
)

func calculateUniqueID(_storage gjson.Result, _storageType int) (_uniqueID string, _err error) {
	switch _storageType {
	case _storageTypeWinDisk:
		_uniqueID = fmt.Sprintf("%s%s",
			_storage.Get("serial_number").Str,
			_storage.Get("disk_path").Str)
	case _storageTypeWinPart:
		_uniqueID = fmt.Sprintf("%v%v%s",
			_storage.Get("start_sector").Int(),
			_storage.Get("end_sector").Int(),
			_storage.Get("disk_path").Str)
	default:
		return "", errors.Errorf("unhandled default case")
	}
	if _uniqueID == "" {
		return "", errors.Errorf("lack unique id")
	}
	return _uniqueID, nil
}
