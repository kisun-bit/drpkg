package x2xcore

import "github.com/kisun-bit/drpkg/logger"

// unconfigXen 移除Xen的配置
func (fixer *windowsSystemFixer) unconfigXen() error {
	logger.Debugf("unconfigXen: ++")
	defer logger.Debugf("unconfigXen: --")

	// TODO 将xenvbd驱动设置为开机不启动

	return nil
}

func (fixer *windowsSystemFixer) configXen() error {
	logger.Debugf("configXen: ++")
	defer logger.Debugf("configXen: --")

	// TODO 安装xenvbd驱动

	return nil
}
