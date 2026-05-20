package recovery

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
