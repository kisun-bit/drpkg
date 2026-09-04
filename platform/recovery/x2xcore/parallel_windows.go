package x2xcore

import "github.com/kisun-bit/drpkg/logger"

func (fixer *windowsSystemFixer) unconfigParallel() error {
	logger.Debugf("unconfigParallel: ++")
	defer logger.Debugf("unconfigParallel: --")

	prlSvcs := []string{
		"prl_boot",
		"prl_dd",
		"prl_eth5",
		"prl_fs",
		"prl_memdev",
		"prl_mouf",
		"prl_pv32",
		"prl_pv64",
		"prl_scsi",
		"prl_sound",
		"prl_strg",
		"prl_tg",
		"prl_time",
		"prl_uprof",
		"prl_va",
	}

	for _, v := range prlSvcs {
		if fixer.existedService(v) {
			// 禁用驱动
			if err := fixer.disableService(v); err != nil {
				return err
			}
		}
	}

	// 禁用过滤驱动
	if err := fixer.disableClassFilters(prlSvcs...); err != nil {
		return err
	}

	return nil
}

func (fixer *windowsSystemFixer) configParallel() error {
	logger.Debugf("configParallel: ++")
	defer logger.Debugf("configParallel: --")

	// TODO 未实现

	return nil
}
