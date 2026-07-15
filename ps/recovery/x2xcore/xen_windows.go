package x2xcore

import (
	"github.com/kisun-bit/drpkg/define"
	"github.com/kisun-bit/drpkg/logger"
)

// unconfigXen 移除Xen的配置
func (fixer *windowsSystemFixer) unconfigXen() error {
	logger.Debugf("unconfigXen: ++")
	defer logger.Debugf("unconfigXen: --")

	xenDrivers := []string{
		"XEN",
		"xenagent",
		"xenbus",
		"xenbus_monitor",
		"xendisk",
		"xenfilt",
		"xeniface",
		"xennet",
		"XenSvc",
		"xenvbd",
		"xenvif",
		"XenPCI",
	}

	for _, v := range xenDrivers {
		if fixer.existedService(v) {
			// 禁用驱动
			if err := fixer.disableService(v); err != nil {
				return err
			}
		}
	}

	// 禁用过滤驱动
	if err := fixer.disableClassFilters(xenDrivers...); err != nil {
		return err
	}

	return nil
}

func (fixer *windowsSystemFixer) configXen() error {
	logger.Debugf("configXen: ++")
	defer logger.Debugf("configXen: --")

	// TODO 安装xen驱动

	ds, err := fixer.x2xLib.SelectWindowsBestVirtualDriver(
		define.HPVTXen,
		fixer.opts.RecoveryParam.Source.Arch,
		fixer.offsys.windowsVersion,
		false)
	if err != nil {
		return err
	}

	if err = fixer.addDrivers(ds); err != nil {
		return err
	}

	return nil
}
