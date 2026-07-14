package x2xcore

import "github.com/kisun-bit/drpkg/logger"

var vmwareDrivers = []string{
	"lsi_sas",
	"vmxnet3",
}

// unconfigVmware 移除Vmware的配置
func (fixer *windowsSystemFixer) unconfigVmware() error {
	logger.Debugf("unconfigVmware: ++")
	defer logger.Debugf("unconfigVmware: --")

	logger.Debugf("unconfigVmware: do nothing")

	return nil
}

func (fixer *windowsSystemFixer) configVmware() error {
	logger.Debugf("configVmware: ++")
	defer logger.Debugf("configVmware: --")

	// TODO

	return nil
}
