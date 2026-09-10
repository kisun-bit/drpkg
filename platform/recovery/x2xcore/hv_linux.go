package x2xcore

import (
	"github.com/kisun-bit/drpkg/logger"
	"github.com/pkg/errors"
)

func (fixer *linuxSystemFixer) unconfigHyperV() error {
	logger.Debugf("unconfigHyperV: ++")
	defer logger.Debugf("unconfigHyperV: --")

	logger.Debugf("unconfigHyperV: do nothing")

	fixer.infof(LogTplForUnconfigHVWith0Args)

	return nil
}

func (fixer *linuxSystemFixer) configHyperV() error {
	logger.Debugf("configHyperV: ++")
	defer logger.Debugf("configHyperV: --")

	return errors.New("configHyperV: not implemented")

	fixer.infof(LogTplForConfigHVWith0Args)
	// 对于低版本Linux抛出警告，让其目标平台使用兼容性硬件设备去启动系统（如ide、legacy nic等）
	fixer.warnf(LogTplForHyperVLowVersionWith0Args)

	return nil
}
