package x2xcore

import "github.com/kisun-bit/drpkg/logger"

// unconfigVmware 移除Vmware的配置
func (fixer *windowsSystemFixer) unconfigVmware() error {
	logger.Debugf("unconfigVmware: ++")
	defer logger.Debugf("unconfigVmware: --")

	for _, v := range []string{
		"vmci",
		"vmhgfs",
		"vmmouse",
		"vm3dmp",
		"vmx_svga",
		"pvscsi",
		"lsi_sas",
	} {
		if fixer.existedService(v) {
			if err := fixer.disableService(v); err != nil {
				return err
			}
		}
	}

	return nil
}

func (fixer *windowsSystemFixer) configVmware() error {
	logger.Debugf("configVmware: ++")
	defer logger.Debugf("configVmware: --")

	// TODO 安装vmtools相关的驱动

	for _, v := range []string{
		"pvscsi",
		"lsi_sas",
	} {
		if fixer.existedService(v) {
			if err := fixer.enableService(v); err != nil {
				return err
			}
		}
	}

	return nil
}
