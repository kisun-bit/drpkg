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
	"runtime"
)

func NewSystemJsonInfo() (json_ string, err error) {
	json_, err = baseInfoJson()
	if err != nil {
		return "", err
	}

	name, version, id, versionID, prettyName, err := ioctl.QueryOSRelease()
	if err != nil {
		return "", err
	}
	osName := name + " " + version
	if prettyName != "" {
		osName = prettyName
	}
	json_, err = sjson.Set(json_, "os_name", osName)
	if err != nil {
		return "", errors.Errorf("failed to set os name to json, %v", err)
	}
	json_, err = sjson.Set(json_, "release_name", name)
	if err != nil {
		return "", errors.Errorf("failed to set release name to json, %v", err)
	}
	json_, err = sjson.Set(json_, "release_version", version)
	if err != nil {
		return "", errors.Errorf("failed to set release version to json, %v", err)
	}
	json_, err = sjson.Set(json_, "release_id", id)
	if err != nil {
		return "", errors.Errorf("failed to set release id to json, %v", err)
	}
	json_, err = sjson.Set(json_, "release_version_id", versionID)
	if err != nil {
		return "", errors.Errorf("failed to set release version id to json, %v", err)
	}
	json_, err = sjson.Set(json_, "os_version", versionID)
	if err != nil {
		return "", errors.Errorf("failed to set os version id to json, %v", err)
	}
	json_, err = sjson.Set(json_, "release_pretty_name", prettyName)
	if err != nil {
		return "", errors.Errorf("failed to set pretty name to json, %v", err)
	}

	kernel, kernelImg, initrd, err := ioctl.DefaultBootKernelInfo(id)
	if err != nil {
		return "", err
	}
	json_, err = sjson.Set(json_, "default_kernel", kernel)
	if err != nil {
		return "", errors.Errorf("failed to set default kernel to json, %v", err)
	}
	json_, err = sjson.Set(json_, "kernel_img", kernelImg)
	if err != nil {
		return "", errors.Errorf("failed to set kernel image to json, %v", err)
	}
	json_, err = sjson.Set(json_, "initrd", initrd)
	if err != nil {
		return "", errors.Errorf("failed to set initrd to json, %v", err)
	}

	kernels, err := ioctl.Kernels()
	if err != nil {
		return "", err
	}
	json_, err = sjson.Set(json_, "kernels", kernels)
	if err != nil {
		return "", errors.Errorf("failed to set kernels to json, %v", err)
	}

	grubVersion, grubInstallPath, grubMkConfigPath, err := ioctl.GrubVersionAndPathForOnlineSystem()
	if err != nil {
		return "", err
	}
	json_, err = sjson.Set(json_, "grub_version", grubVersion)
	if err != nil {
		return "", errors.Errorf("failed to set grub version to json, %v", err)
	}
	json_, err = sjson.Set(json_, "grub_install_path", grubInstallPath)
	if err != nil {
		return "", errors.Errorf("failed to set grub-install path to json, %v", err)
	}
	json_, err = sjson.Set(json_, "grub_mkconfig_path", grubMkConfigPath)
	if err != nil {
		return "", errors.Errorf("failed to set grub-mkconfig path to json, %v", err)
	}

	target, err := ioctl.GRUB2Target(runtime.GOARCH, gjson.Get(json_, "boot_mode").String())
	if err != nil {
		return "", err
	}
	json_, err = sjson.Set(json_, "grub_target", target)
	if err != nil {
		return "", errors.Errorf("failed to set grub targets to json, %v", err)
	}

	// LVM 相关信息.
	lvmInfo, err := storage.LVMInfo()
	if err != nil {
		return "", err
	}
	json_, err = sjson.Set(json_, "lvm", lvmInfo)
	if err != nil {
		return "", errors.Errorf("failed to set lvm info to json, %v", err)
	}

	// 存储相关信息.
	hardDiskInfoJson, err := storage.Storages(lvmInfo)
	if err != nil {
		return "", err
	}
	json_, err = sjson.SetRaw(json_, "storage", hardDiskInfoJson)
	if err != nil {
		return "", errors.Errorf("failed to set storage info to json, %v", err)
	}

	// Swap相关信息
	swaps, _ := storage.SwapInfo()
	json_, err = sjson.Set(json_, "swap", swaps)
	if err != nil {
		return "", errors.Errorf("failed to set swap info to json, %v", err)
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

// renderStorageUniqueIdentify 为存储(磁盘和分区)、LVM等计算其唯一标识.
func renderStorageUniqueIdentify(jsonInfo string) (renderJsonInfo string, err error) {
	res := make([]map[string]any, 0)
	for i, diskResult := range gjson.Get(jsonInfo, "storage").Array() {
		res = append(res, map[string]any{
			"st": _storageTypeLinuxDisk, "jo": diskResult, "jp": fmt.Sprintf("storage.%d", i)})
		for j, partResult := range diskResult.Get("parts").Array() {
			res = append(res, map[string]any{
				"st": _storageTypeLinuxDisk, "jo": partResult, "jp": fmt.Sprintf("storage.%d.parts.%d", i, j)})
		}
	}
	for i, pvResult := range gjson.Get(jsonInfo, "lvm.pv_list").Array() {
		res = append(res, map[string]any{
			"st": _storageTypeLinuxLVM, "jo": pvResult, "jp": fmt.Sprintf("lvm.pv_list.%d", i)})
	}
	for i, vgResult := range gjson.Get(jsonInfo, "lvm.vg_list").Array() {
		res = append(res, map[string]any{
			"st": _storageTypeLinuxLVM, "jo": vgResult, "jp": fmt.Sprintf("lvm.vg_list.%d", i)})
	}
	for i, lvResult := range gjson.Get(jsonInfo, "lvm.lv_list").Array() {
		res = append(res, map[string]any{
			"st": _storageTypeLinuxLVM, "jo": lvResult, "jp": fmt.Sprintf("lvm.lv_list.%d", i)})
	}
	return fixAllJsonObjects(jsonInfo, res)
}
