package x2xcore

import (
	"github.com/kisun-bit/drpkg/define"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/pkg/errors"
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

	ds, err := fixer.x2xLib.SelectWindowsBestVirtualDriver(
		define.HPVTXen,
		fixer.opts.RecoveryParam.Source.Arch,
		fixer.offsys.windowsVersion,
		false)
	if err != nil {
		return err
	}

	ntVer, ok := define.OsNTVersion[fixer.offsys.windowsVersion]
	if !ok {
		return errors.New("not supported windows version")
	}
	if ntVer >= define.NT61 {
		if e := fixer.injectDriversByDism(ds); e != nil {
			return e
		}
	}

	// TODO 完成对win2k、winxp、win2k3、winvista、win2k8的驱动注入

	return nil
}
