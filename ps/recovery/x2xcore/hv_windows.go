package x2xcore

import "github.com/kisun-bit/drpkg/logger"

var hyperVServices = []string{
	"storvsc",
	"netvsc",
}

func (fixer *windowsSystemFixer) unconfigHyperV() error {
	logger.Debugf("unconfigHyperV: ++")
	defer logger.Debugf("unconfigHyperV: --")

	logger.Debugf("unconfigHyperV: do nothing")

	for _, service := range hyperVServices {
		if err := fixer.disableService(service); err != nil {
			return err
		}
	}

	return nil
}

func (fixer *windowsSystemFixer) configHyperV() error {
	logger.Debugf("configHyperV: ++")
	defer logger.Debugf("configHyperV: --")

	// Windows 7 / Server 2008 R2 及以上通常已内置 Hyper-V Integration Services。
	// XP/2003 等较老版本需要单独安装 Integration Services。
	for _, service := range hyperVServices {
		if !fixer.existedService(service) {
			logger.Infof("configHyperV: Hyper-V integration service %q not found", service)

			// TODO: 安装 Hyper-V Integration Services
			return nil
		}
	}

	for _, service := range hyperVServices {
		if err := fixer.enableService(service); err != nil {
			return err
		}
	}

	return nil
}
