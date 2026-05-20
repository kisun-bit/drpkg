package recovery

import (
	"github.com/kisun-bit/drpkg/logger"
	"github.com/pkg/errors"
)

// unconfigXen 移除Xen的配置
func (fixer *linuxSystemFixer) unconfigXen() error {
	logger.Debugf("unconfigXen: ++")
	defer logger.Debugf("unconfigXen: --")

	//
	// TODO
	//  若目标平台是xen，那么就将xen-hcall-runstor改成xen-hcall
	//  若目标平台不是xen，那么就将xen-hcall改成xen-hcall-runstor
	//

	return errors.New("unconfigXen: not implemented yet")
}
