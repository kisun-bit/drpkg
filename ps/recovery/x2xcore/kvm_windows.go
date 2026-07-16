package x2xcore

import (
	"github.com/kisun-bit/drpkg/define"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/pkg/errors"
)

var kvmDrivers = []string{
	"viostor",
	"vioscsi",
	"netkvm",
}

func (fixer *windowsSystemFixer) unconfigKvm() error {
	logger.Debugf("unconfigKvm: ++")
	defer logger.Debugf("unconfigKvm: --")

	logger.Debugf("unconfigKvm: do nothing")

	for _, driver := range kvmDrivers {
		if e := fixer.disableService(driver); e != nil {
			return errors.Wrapf(e, "disable service %s", driver)
		}
	}

	return nil
}

func (fixer *windowsSystemFixer) configKvm() error {
	logger.Debugf("configKvm: ++")
	defer logger.Debugf("configKvm: --")

	isAllExisted := true
	for _, v := range kvmDrivers {
		if !fixer.existedService(v) {
			isAllExisted = false
			break
		}
	}

	if isAllExisted {
		for _, driver := range kvmDrivers {
			if e := fixer.enableService(driver); e != nil {
				return errors.Wrapf(e, "enable service %s", driver)
			}
		}
		logger.Debugf("configKvm: do nothing")
		return nil
	}

	ds, e := fixer.x2xLib.SelectWindowsBestVirtualDriver(
		define.HPVTKvm,
		fixer.opts.RecoveryParam.Target.Arch,
		fixer.offsys.windowsVersion,
		true,
	)
	if e != nil {
		return errors.Wrapf(e, "SelectWindowsBestVirtualDriver")
	}

	ntVer, ok := define.OsNTVersion[fixer.offsys.windowsVersion]
	if !ok {
		return errors.New("not supported windows version")
	}
	if ntVer >= define.NT61 {
		if e = fixer.injectDriversByDism(ds); e != nil {
			return e
		}
	}

	// TODO 完成对win2k、winxp、win2k3、winvista、win2k8的驱动注入

	return nil
}
