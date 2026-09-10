package ioctl

import (
	"bytes"
	"encoding/binary"

	"github.com/lunixbochs/struc"
	"github.com/pkg/errors"
	"golang.org/x/sys/windows/registry"
)

// 注册表持久化路径，与 v1 保持一致。
const (
	regPathCdpConfig   = `SYSTEM\CurrentControlSet\Services\biotrk\Parameters`
	regKeyPolicyConfig = "PolicyConfigData"
	regKeyPolicyActive = "PolicyActive"
)

// PersistStartRequest 将 StartTaskRequest 序列化后写入注册表。
//
// 写入路径：HKLM\SYSTEM\CurrentControlSet\Services\biotrk\Parameters
//   - PolicyConfigData: REG_BINARY，StartTaskRequest 的 struc 序列化数据
//   - PolicyActive:     REG_DWORD，值为 1，表示 CDP 任务处于活动状态
//
// 系统重启后，驱动会从此注册表项恢复受保护设备元数据。
func PersistStartRequest(req *StartTaskRequest) error {
	k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, regPathCdpConfig, registry.ALL_ACCESS)
	if err != nil {
		return errors.Wrapf(err, "open registry key %s", regPathCdpConfig)
	}
	defer k.Close()

	buf := new(bytes.Buffer)
	if err := struc.PackWithOptions(buf, req, &struc.Options{Order: binary.LittleEndian}); err != nil {
		return errors.Wrapf(err, "pack StartTaskRequest")
	}

	if err := k.SetBinaryValue(regKeyPolicyConfig, buf.Bytes()); err != nil {
		return errors.Wrapf(err, "set %s", regKeyPolicyConfig)
	}

	if err := k.SetDWordValue(regKeyPolicyActive, 1); err != nil {
		return errors.Wrapf(err, "set %s", regKeyPolicyActive)
	}

	return nil
}

// ReadPersistRequest 从注册表读取持久化的 StartTaskRequest。
func ReadPersistRequest() (*StartTaskRequest, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, regPathCdpConfig, registry.READ)
	if err != nil {
		return nil, errors.Wrapf(err, "open registry key %s", regPathCdpConfig)
	}
	defer k.Close()

	data, _, err := k.GetBinaryValue(regKeyPolicyConfig)
	if err != nil {
		return nil, errors.Wrapf(err, "read %s", regKeyPolicyConfig)
	}

	req := &StartTaskRequest{}
	if err := struc.UnpackWithOptions(bytes.NewReader(data), req, &struc.Options{Order: binary.LittleEndian}); err != nil {
		return nil, errors.Wrapf(err, "unpack StartTaskRequest")
	}

	return req, nil
}

// RemovePersist 删除注册表中的 CDP 持久化参数。
//
// 删除 PolicyActive 和 PolicyConfigData 两个值。
// 若值不存在，不视为错误。
func RemovePersist() error {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, regPathCdpConfig, registry.ALL_ACCESS)
	if err != nil {
		return errors.Wrapf(err, "open registry key %s", regPathCdpConfig)
	}
	defer k.Close()

	// 删除 PolicyActive
	if err := k.DeleteValue(regKeyPolicyActive); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return errors.Wrapf(err, "delete %s", regKeyPolicyActive)
	}

	// 删除 PolicyConfigData
	if err := k.DeleteValue(regKeyPolicyConfig); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return errors.Wrapf(err, "delete %s", regKeyPolicyConfig)
	}

	return nil
}
