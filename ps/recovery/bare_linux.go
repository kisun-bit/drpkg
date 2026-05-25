package recovery

import (
	"github.com/kisun-bit/drpkg/logger"
)

func (fixer *linuxSystemFixer) unconfigBare() error {
	logger.Debugf("unconfigBare: ++")
	defer logger.Debugf("unconfigBare: --")

	logger.Debugf("unconfigBare: do nothing")

	return nil
}

func (fixer *linuxSystemFixer) configBare() error {
	logger.Debugf("configXen: ++")
	defer logger.Debugf("configXen: --")

	// TODO

	return nil
}
