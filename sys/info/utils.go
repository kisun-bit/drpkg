package info

import (
	"fmt"
	"github.com/kisun-bit/drpkg/util"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	_storageTypeLinuxDisk = iota
	_storageTypeLinuxLVM
	_storageTypeWinDisk
	_storageTypeWinPart
)

func patchUniqueID(jsonInfo, jsonObjPath, uniqueID string) (patchedJsonInfo string, err error) {
	patchedJsonInfo, err = sjson.Set(jsonInfo,
		fmt.Sprintf("%s.unique_id", jsonObjPath), uniqueID)
	if err != nil {
		return "", err
	}
	patchedJsonInfo, err = sjson.Set(patchedJsonInfo,
		fmt.Sprintf("%s.unique_id_md5", jsonObjPath), util.Md5([]byte(uniqueID)))
	if err != nil {
		return "", err
	}
	return patchedJsonInfo, nil
}

func fixAllJsonObjects(jsonInfo string, res []map[string]any) (_ string, err error) {
	historyIDs := make([]string, 0)
	for _, result := range res {
		st := result["st"].(int)
		jo := result["jo"].(gjson.Result)
		jp := result["jp"].(string)
		id, e := calculateUniqueID(jo, st)
		if e != nil {
			return "", e
		}
		if funk.InStrings(historyIDs, id) {
			return "", errors.Errorf("duplicate unique id(%s)", id)
		}
		historyIDs = append(historyIDs, id)
		jsonInfo, e = patchUniqueID(jsonInfo, jp, id)
		if e != nil {
			return "", e
		}
	}
	return jsonInfo, nil
}
