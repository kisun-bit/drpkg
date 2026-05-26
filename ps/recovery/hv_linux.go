package recovery

import "github.com/kisun-bit/drpkg/logger"

func (fixer *linuxSystemFixer) unconfigHyperV() error {
	logger.Debugf("unconfigHyperV: ++")
	defer logger.Debugf("unconfigHyperV: --")

	logger.Debugf("unconfigHyperV: do nothing")

	return nil
}

func (fixer *linuxSystemFixer) configHyperV() error {
	logger.Debugf("configHyperV: ++")
	defer logger.Debugf("configHyperV: --")

	// TODO 对于低版本Linux抛出警告，让其目标平台使用兼容性硬件设备去启动系统（如ide、legacy nic等）

	return nil
}
