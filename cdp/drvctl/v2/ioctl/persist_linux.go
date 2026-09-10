package ioctl

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"time"

	"encoding/binary"
	"github.com/lunixbochs/struc"
	"github.com/pkg/errors"
)

// 持久化文件路径与 initramfs 脚本名称。
const (
	persistFilePath      = "/etc/biotrk/cfg.json"
	scriptAddTask        = "biotrk_add_task"
	scriptDelTask        = "biotrk_del_task"
	defaultScriptTimeout = 60 * time.Second
)

// PersistStartRequest 将 StartTaskRequest 保存到本地文件并通过 initramfs 脚本持久化。
//
// 步骤：
//  1. 将 req 按 struct 序列化为二进制。
//  2. 调用 biotrk_add_task <临时文件路径> 脚本。
//  3. 将 req 以 JSON 格式写入 /etc/biotrk/cfg.json。
//
// 系统重启后，驱动从 initramfs 中的配置文件恢复受保护设备元数据。
func PersistStartRequest(req *StartTaskRequest) error {
	// 1. 调用 initramfs 脚本
	if err := addTaskWithInitRamFs(req, defaultScriptTimeout); err != nil {
		return err
	}

	// 2. 写本地 JSON 文件
	data, err := json.Marshal(req)
	if err != nil {
		return errors.Wrapf(err, "json marshal")
	}

	if err := os.MkdirAll("/etc/biotrk", 0755); err != nil {
		return errors.Wrapf(err, "mkdir /etc/biotrk")
	}

	if err := os.WriteFile(persistFilePath, data, 0666); err != nil {
		return errors.Wrapf(err, "write %s", persistFilePath)
	}

	return nil
}

// ReadPersistRequest 从本地文件读取持久化的 StartTaskRequest。
func ReadPersistRequest() (*StartTaskRequest, error) {
	data, err := os.ReadFile(persistFilePath)
	if err != nil {
		return nil, errors.Wrapf(err, "read %s", persistFilePath)
	}

	req := &StartTaskRequest{}
	if err := json.Unmarshal(data, req); err != nil {
		return nil, errors.Wrapf(err, "json unmarshal")
	}

	return req, nil
}

// RemovePersist 删除持久化参数。
//
// 步骤：
//  1. 调用 biotrk_del_task 脚本删除 initramfs 中的配置。
//  2. 删除本地 JSON 文件 /etc/biotrk/cfg.json。
func RemovePersist() error {
	if err := delTaskWithInitRamFs(defaultScriptTimeout); err != nil {
		return err
	}

	if err := os.Remove(persistFilePath); err != nil && !os.IsNotExist(err) {
		return errors.Wrapf(err, "remove %s", persistFilePath)
	}

	return nil
}

// addTaskWithInitRamFs 打包 StartTaskRequest 写入临时文件，调用 biotrk_add_task 脚本。
func addTaskWithInitRamFs(req *StartTaskRequest, timeout time.Duration) error {
	var packedReq bytes.Buffer
	if err := struc.PackWithOptions(&packedReq, req, &struc.Options{Order: binary.LittleEndian}); err != nil {
		return errors.Wrapf(err, "pack request")
	}

	tmpFile, err := os.CreateTemp("", "packedReq-*.bin")
	if err != nil {
		return errors.Wrapf(err, "create temp file")
	}
	tmpPath := tmpFile.Name()
	defer func() {
		tmpFile.Close()
		os.Remove(tmpPath)
	}()

	if _, err := tmpFile.Write(packedReq.Bytes()); err != nil {
		return errors.Wrapf(err, "write temp file")
	}
	if err := tmpFile.Sync(); err != nil {
		return errors.Wrapf(err, "sync temp file")
	}
	tmpFile.Close()

	_, err = runCmdWithTimeout(scriptAddTask, []string{tmpPath}, timeout)
	return err
}

// delTaskWithInitRamFs 调用 biotrk_del_task 脚本删除 initramfs 中的配置。
func delTaskWithInitRamFs(timeout time.Duration) error {
	_, err := runCmdWithTimeout(scriptDelTask, nil, timeout)
	return err
}

// runCmdWithTimeout 在指定超时内执行脚本，返回合并的 stdout/stderr。
func runCmdWithTimeout(scriptPath string, args []string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = defaultScriptTimeout
	}

	execPath, err := exec.LookPath(scriptPath)
	if err != nil {
		return "", errors.Errorf("script %q not found: %v", scriptPath, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, execPath, args...)
	cmd.Env = os.Environ()

	out, err := cmd.CombinedOutput()
	outStr := string(out)

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return outStr, errors.Errorf("command %q timed out after %s: %s", scriptPath, timeout, outStr)
	}

	if err != nil {
		return outStr, errors.Errorf("command %q failed: %v; output: %s", scriptPath, err, outStr)
	}

	return outStr, nil
}
