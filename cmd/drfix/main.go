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
	markerFileName = ".drfix_repair_done"
)

// getMarkerPath returns repair completion marker path.
func getMarkerPath() string {
	return filepath.Join(extend.ExecDir(), markerFileName)
}

// isRepairDone checks whether repair was completed previously.
func isRepairDone() bool {
	_, err := os.Stat(getMarkerPath())
	return err == nil
}

// markRepairDone writes repair completion marker atomically.
func markRepairDone() error {
	markerPath := getMarkerPath()

	if err := os.MkdirAll(filepath.Dir(markerPath), 0755); err != nil {
		return fmt.Errorf("create marker directory failed: %v", err)
	}

	tmpPath := markerPath + ".tmp"

	if err := os.WriteFile(
		tmpPath,
		[]byte(version),
		0644,
	); err != nil {
		return fmt.Errorf("write marker temp file failed: %v", err)
	}

	if err := os.Rename(tmpPath, markerPath); err != nil {
		return fmt.Errorf("rename marker file failed: %v", err)
	}

	return nil
}

func main() {

	cfg := service.Config{
		Name:        serviceName,
		DisplayName: serviceName,
	}

	switch runtime.GOOS {
	case "windows":
		cfg.Option = map[string]interface{}{
			"OnFailure": "noaction",
		}

	case "linux":
		cfg.Dependencies = []string{
			"After=network.target syslog.target",
		}
	}

	err := minisvc.Run(minisvc.Options{
		Version: version,
		Config:  cfg,

		Lifecycle: minisvc.Lifecycle{

			Start: func() error {

				// service manager requires Start return quickly.
				go runRepairWorker()

				return nil
			},
		},
	})

	if err != nil {
		fmt.Fprintf(
			os.Stderr,
			"service start failed: %v\n",
			err,
		)

		os.Exit(1)
	}
}

func runRepairWorker() {

	reqPortPath, logPortPath, err := initVirtioPorts()

	if err != nil {
		fmt.Printf(
			"init virtio ports failed: %v\n",
			err,
		)
		return
	}

	logPort, err := openVirtioPort(logPortPath)

	if err != nil {
		fmt.Printf(
			"open log port failed: %v\n",
			err,
		)
		return
	}

	defer logPort.Close()

	log := logger.NewLogger(
		serviceName,
		zapcore.DebugLevel,
		logPort,
		os.Stdout,
	)

	logger.SetupDefaultLogger(log)

	logger.Infof(
		"request virtio port: %s",
		reqPortPath,
	)

	logger.Infof(
		"log virtio port: %s",
		logPortPath,
	)

	reqPort, err := openVirtioPort(reqPortPath)

	if err != nil {
		logger.Errorf(
			"open request port failed: %v",
			err,
		)
		return
	}

	defer reqPort.Close()

	if isRepairDone() {

		logger.Info(
			"repair already completed, skip",
		)

		return
	}

	logger.Info(
		"starting repair",
	)

	err = runRepair(
		context.Background(),
		reqPort,
	)

	if err != nil {

		logger.Errorf(
			"repair failed: %v",
			err,
		)

		return
	}

	if err := markRepairDone(); err != nil {

		logger.Errorf(
			"write repair marker failed: %v",
			err,
		)

		return
	}

	logger.Infof(
		"repair completed successfully, version=%s",
		version,
	)
}

func initVirtioPorts() (
	string,
	string,
	error,
) {

	req := x2xcore.FindVirtioPort(
		x2xcore.RequestVirtioPortName,
	)

	log := x2xcore.FindVirtioPort(
		x2xcore.LogVirtioPortName,
	)

	if req == "" || log == "" {

		return "",
			"",
			fmt.Errorf(
				"virtio port not found: request=%q log=%q",
				req,
				log,
			)
	}

	return req, log, nil
}

func openVirtioPort(path string) (*os.File, error) {

	file, err := os.OpenFile(
		path,
		os.O_RDWR,
		0666,
	)

	if err != nil {
		return nil, fmt.Errorf(
			"open virtio port %s failed: %v",
			path,
			err,
		)
	}

	return file, nil
}

// runRepair executes the repair workflow.
func runRepair(
	ctx context.Context,
	reqPort *os.File,
) (err error) {

	if err := x2xcore.WriteSerialMessageTypeGuestReady(reqPort); err != nil {
		return fmt.Errorf("send guest ready message: %v", err)
	}

	repairParam, err := x2xcore.ReadReceivedSerialMessageTypeStartRepair(reqPort)
	if err != nil {
		return fmt.Errorf("receive start repair request: %v", err)
	}

	defer func() {
		reportRepairResult(reqPort, err)
	}()

	fixer, err := x2xcore.NewSysFixer(
		ctx,
		&repairParam,
		reqPort,
	)
	if err != nil {
		return fmt.Errorf("create system fixer: %v", err)
	}

	defer fixer.Cleanup()

	if err := fixer.Prepare(); err != nil {
		return fmt.Errorf("prepare repair environment: %v", err)
	}

	if err := fixer.Repair(); err != nil {
		return fmt.Errorf("execute system repair: %v", err)
	}

	return nil
}

// reportRepairResult reports repair result to host.
func reportRepairResult(
	reqPort *os.File,
	err error,
) {

	result := x2xcore.RepairResult{
		Success: err == nil,
	}

	if err != nil {
		result.ErrorMsg = err.Error()
	}

	if sendErr := x2xcore.WriteSerialMessageTypeRepairResult(
		reqPort,
		result,
	); sendErr != nil {

		logger.Errorf(
			"send repair result failed: %v",
			sendErr,
		)
	}
}
