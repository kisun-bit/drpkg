package recovery

import (
	"github.com/kisun-bit/drpkg/logger"
	"github.com/pkg/errors"
)

// unconfigVmware 移除Vmware的配置
func (fixer *linuxSystemFixer) unconfigVmware() error {
	logger.Debugf("unconfigVmware: ++")
	defer logger.Debugf("unconfigVmware: --")

	//
	// TODO
	//

	return errors.New("unconfigVmware: not implemented yet")
}
