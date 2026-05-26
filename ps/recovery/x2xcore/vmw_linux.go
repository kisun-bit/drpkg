package x2xcore

import (
	"github.com/kisun-bit/drpkg/logger"
)

// unconfigVmware 移除Vmware的配置
func (fixer *linuxSystemFixer) unconfigVmware() error {
	logger.Debugf("unconfigVmware: ++")
	defer logger.Debugf("unconfigVmware: --")

	logger.Debugf("unconfigVmware: do nothing")

	return nil
}

func (fixer *linuxSystemFixer) configVmware() error {
	logger.Debugf("configVmware: ++")
	defer logger.Debugf("configVmware: --")

	// TODO

	return nil
}
