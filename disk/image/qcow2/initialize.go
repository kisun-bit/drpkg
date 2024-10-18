//go:build linux

package qcow2

import (
	"github.com/kisun-bit/drpkg/util/logger"
	"os"
)

var (
	_QemuImgExecPath = "qemu-img"
	_QemuIoWExecPath = "qemu-iow"
)

func QemuEnvSetup(qemuImgExecPath, qemuIoWExecPath string) {
	if qemuImgExecPath != "" {
		if _, err := os.Stat(qemuIoWExecPath); os.IsNotExist(err) {
			logger.Fatalf("QemuEnvSetup failed to set _QemuImgExecPath: %v", err)
		}
		_QemuImgExecPath = qemuImgExecPath
		logger.Infof("QemuEnvSetup _QemuImgExecPath=\"%s\"", _QemuImgExecPath)
	}
	if qemuIoWExecPath != "" {
		if _, err := os.Stat(qemuIoWExecPath); os.IsNotExist(err) {
			logger.Fatalf("QemuEnvSetup failed to set _QemuIoWExecPath: %v", err)
		}
		_QemuIoWExecPath = qemuIoWExecPath
		logger.Infof("QemuEnvSetup _QemuIoWExecPath=\"%s\"", _QemuIoWExecPath)
	}
}
