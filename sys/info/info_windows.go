package info

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/kisun-bit/drpkg/sys/info/storage"
	"github.com/kisun-bit/drpkg/sys/ioctl"
	"github.com/pkg/errors"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func NewSystemJsonInfo() (json_ string, err error) {
	json_, err = baseInfoJson()
	if err != nil {
		return "", err
	}

	osname, err := ioctl.QueryProductName()
	if err != nil {
		return "", err
	}
	json_, err = sjson.Set(json_, "os_name", osname)
	if err != nil {
		return "", errors.Errorf("failed to set os name info to json, %v", err)
	}

	ver, err := ioctl.QueryWindowsVersion()
	if err != nil {
		return "", err
	}
	json_, err = sjson.Set(json_, "os_version", ver.String())
	if err != nil {
		return "", errors.Errorf("failed to set os name info to json, %v", err)
	}

	// 存储相关信息.
	hardDiskInfoJson, err := storage.StoragesJson()
	if err != nil {
		return "", err
	}
	json_, err = sjson.SetRaw(json_, "storage", hardDiskInfoJson)
	if err != nil {
		return "", errors.Errorf("failed to set storage info to json, %v", err)
	}

	// 设置存储对象的唯一标识.
	json_, err = renderStorageUniqueIdentify(json_)
	if err != nil {
		return "", err
	}

	// 优化最终JSON输出.
	var out bytes.Buffer
	err = json.Indent(&out, []byte(json_), "", "\t")
	if err != nil {
		return "", err
	}
	return out.String(), nil
}

// renderStorageUniqueIdentify 为存储(磁盘和分区)等计算其唯一标识.
func renderStorageUniqueIdentify(jsonInfo string) (renderJsonInfo string, err error) {
	res := make([]map[string]any, 0)
	for i, diskResult := range gjson.Get(jsonInfo, "storage").Array() {
		res = append(res, map[string]any{
			"st": _storageTypeWinDisk, "jo": diskResult, "jp": fmt.Sprintf("storage.%d", i)})
		for j, partResult := range diskResult.Get("parts").Array() {
			res = append(res, map[string]any{
				"st": _storageTypeWinPart, "jo": partResult, "jp": fmt.Sprintf("storage.%d.parts.%d", i, j)})
		}
	}
	return fixAllJsonObjects(jsonInfo, res)
}
