package recovery

import (
	"github.com/kisun-bit/drpkg/logger"
)

func (fixer *linuxSystemFixer) unconfigBareMetal() error {
	logger.Debugf("unconfigBareMetal: ++")
	defer logger.Debugf("unconfigBareMetal: --")

	logger.Debugf("unconfigBareMetal: do nothing")

	return nil
}

func (fixer *linuxSystemFixer) configBareMetal() error {
	logger.Debugf("configBareMetal: ++")
	defer logger.Debugf("configBareMetal: --")

	// TODO

	return nil
}
