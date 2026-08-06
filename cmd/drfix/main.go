package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/kardianos/service"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/ps/minisvc"
	"github.com/kisun-bit/drpkg/ps/recovery/x2xcore"
	"go.uber.org/zap/zapcore"
)

const (
	serviceName    = "drfix"
	version        = "6.19.00083"
	markerFileName = ".drfix_repair_done" // 修复完成标记文件
)

// getMarkerPath 返回标记文件的持久化路径
func getMarkerPath() string {
	return filepath.Join(extend.ExecDir(), markerFileName)
}

// isRepairDone 检查修复是否已在之前的服务生命周期中完成
func isRepairDone() bool {
	_, err := os.Stat(getMarkerPath())
	return err == nil
}

// markRepairDone 原子性地写入修复完成标记
func markRepairDone() error {
	dir := filepath.Dir(getMarkerPath())
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create marker dir %s failed: %v", dir, err)
	}

	// 先写临时文件再 rename，保证原子性，避免写入中途崩溃导致标记文件损坏
	tmpPath := getMarkerPath() + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(version), 0644); err != nil {
		return fmt.Errorf("write tmp marker failed: %v", err)
	}
	if err := os.Rename(tmpPath, getMarkerPath()); err != nil {
		return fmt.Errorf("rename marker failed: %v", err)
	}
	return nil
}

func main() {
	// ========== 1. 查找 virtio 端口设备路径 ==========
	reqPortPath := x2xcore.FindVirtioPort(x2xcore.RequestVirtioPortName)
	logPortPath := x2xcore.FindVirtioPort(x2xcore.LogVirtioPortName)

	if reqPortPath == "" || logPortPath == "" {
		fmt.Fprintf(os.Stderr, "virtio port device not found: req=%q, log=%q\n", reqPortPath, logPortPath)
		os.Exit(1)
	}

	// ========== 2. 打开日志端口并初始化日志（尽早初始化） ==========
	logPortHandle, err := os.OpenFile(logPortPath, os.O_RDWR, 0666)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open log port device %s: %v\n", logPortPath, err)
		os.Exit(1)
	}
	defer logPortHandle.Close()

	cl := logger.NewLogger(serviceName, zapcore.DebugLevel, logPortHandle, os.Stdout)
	logger.SetupDefaultLogger(cl)

	logger.Debugf("request port device: %s", reqPortPath)
	logger.Debugf("log port device: %s", logPortPath)

	// ========== 3. 打开请求端口 ==========
	reqPortHandle, err := os.OpenFile(reqPortPath, os.O_RDWR, 0666)
	if err != nil {
		// 修正：原代码此处误打印了 logPortPath
		logger.Fatalf("failed to open request port device %s: %v", reqPortPath, err)
	}
	defer reqPortHandle.Close()

	// ========== 4. 配置服务 ==========
	cfg := service.Config{
		Name:        serviceName,
		DisplayName: serviceName,
	}

	switch runtime.GOOS {
	case "windows":
		cfg.Option = map[string]interface{}{
			"OnFailure": "restart",
		}
	case "linux":
		cfg.Dependencies = []string{
			"After=network.target syslog.target",
		}
	}

	// ========== 5. 启动服务 ==========
	err = minisvc.Run(minisvc.Options{
		Version: version,
		Config:  cfg,
		Lifecycle: minisvc.Lifecycle{
			Start: func() error {
				// ✅ Start 必须快速返回，不能阻塞
				go func() {
					// 跨重启单次执行保护
					if isRepairDone() {
						logger.Info("repair already completed in previous lifecycle, skipping")
						return
					}

					logger.Info("repair not yet done, starting repair process")
					err := runRepair(context.Background(), reqPortHandle)
					if err != nil {
						logger.Errorf("repair failed: %v", err)
						// 注意：这里不能再 return err 给 SCM 了
						// 依赖 OnFailure: restart 策略自动重试
						return
					}

					if err := markRepairDone(); err != nil {
						logger.Errorf("repair succeeded but failed to write marker: %v", err)
						return
					}

					logger.Infof("repair completed and marked as done (version=%s)", version)
				}()

				// ✅ 立即返回，告诉 SCM "服务已成功启动"
				return nil
			},
		},
	})

	if err != nil {
		logger.Errorf("service run failed: %v", err)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// runRepair 执行实际的修复流程
func runRepair(ctx context.Context, reqPort *os.File) (err error) {
	if err := x2xcore.WriteSerialMessageTypeGuestReady(reqPort); err != nil {
		return fmt.Errorf("write guest ready failed: %v", err)
	}

	repairParam, err := x2xcore.ReadReceivedSerialMessageTypeStartRepair(reqPort)
	if err != nil {
		return fmt.Errorf("read start repair message failed: %v", err)
	}

	defer func() {
		reportRepairResult(reqPort, &err)
	}()

	fixer, err := x2xcore.NewSysFixer(ctx, &repairParam, reqPort)
	if err != nil {
		return fmt.Errorf("create sys fixer failed: %v", err)
	}
	defer fixer.Cleanup()

	if err := fixer.Prepare(); err != nil {
		return fmt.Errorf("fixer prepare failed: %v", err)
	}

	if err := fixer.Repair(); err != nil {
		return fmt.Errorf("fixer repair failed: %v", err)
	}

	return nil
}

// reportRepairResult 通过串口上报修复结果
func reportRepairResult(reqPort *os.File, errp *error) {
	err := *errp
	if err == nil {
		_ = x2xcore.WriteSerialMessageTypeRepairResult(reqPort, x2xcore.RepairResult{
			Success: true,
			Extra:   nil, // TODO: 填充
		})
	} else {
		_ = x2xcore.WriteSerialMessageTypeRepairResult(reqPort, x2xcore.RepairResult{
			Success:  false,
			ErrorMsg: err.Error(),
		})
	}
}
