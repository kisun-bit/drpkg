package x2xcore

import (
	"os"

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

	fixer.infof(LogTplForUnconfigKVMWith0Args)

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

	fixer.infof(LogTplForConfigKVMWith0Args)

	legacy, e := fixer.isLegacyWindows()
	if e != nil {
		return e
	}

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

		// 旧系统（< Win7/2008R2）即使驱动服务已存在，也要确保
		// CriticalDeviceDatabase 引导注册项与 "SCSI miniport" 加载组
		// 完整，否则引导阶段无法加载块驱动。
		if legacy {
			if e := fixer.ensureLegacyKvmBootRegistry(); e != nil {
				return e
			}
		}

		logger.Debugf("configKvm: all drivers existed")
		return nil
	}

	ds, e := fixer.x2xLib.SelectWindowsBestVirtualDriver(
		define.HPVTKvm,
		fixer.opts.RecoveryParam.Target.Arch,
		fixer.offsys.windowsVersion,
		true,
	)
	if e != nil {
		if legacy && errors.Cause(e) == os.ErrNotExist {
			// 驱动库中没有该旧系统的 KVM 驱动：不阻断恢复，
			// 恢复后宿主机使用 IDE 等模拟设备即可启动。
			fixer.warnf(LogTplForNoLegacyVirtualDriverWith2Args,
				fixer.offsys.windowsVersion, kvmDrivers)
			return nil
		}
		return errors.Wrapf(e, "SelectWindowsBestVirtualDriver")
	}

	if !legacy {
		if e = fixer.injectDriversByDism(ds); e != nil {
			return e
		}
	} else {
		// win2k/winxp/win2k3/winvista/win2k8：通过文件复制 +
		// CriticalDeviceDatabase 方式注入
		blockDrv, _, e := fixer.injectDriversLegacy(ds)
		if e != nil {
			return e
		}
		if blockDrv == "" {
			// 没有可用的块设备启动驱动，不阻断恢复流程，
			// 恢复后宿主机使用 IDE 等模拟磁盘启动。
			fixer.warnf(LogTplForNoLegacyBlockDriverWith1Args,
				fixer.offsys.windowsVersion)
		}
	}

	return nil
}

// ensureLegacyKvmBootRegistry 在驱动服务已存在的前提下，为旧系统补齐
// 引导所需的注册表项（CDB 条目、加载组），并记录注入结果。
func (fixer *windowsSystemFixer) ensureLegacyKvmBootRegistry() error {
	logger.Debugf("ensureLegacyKvmBootRegistry: ++")
	defer logger.Debugf("ensureLegacyKvmBootRegistry: --")

	const scsiClassGuid = "{4D36E97B-E325-11CE-BFC1-08002BE10318}"

	if e := fixer.installLegacyService("viostor", "viostor.sys", true); e != nil {
		return e
	}
	if e := fixer.registerCriticalDeviceDatabase(
		"viostor", scsiClassGuid, viostorCompatIds); e != nil {
		return e
	}

	if e := fixer.installLegacyService("vioscsi", "vioscsi.sys", true); e != nil {
		return e
	}
	if e := fixer.registerCriticalDeviceDatabase(
		"vioscsi", scsiClassGuid, vioscsiCompatIds); e != nil {
		return e
	}

	// viostor 对应宿主机 virtio-blk 磁盘总线。
	// netkvm 不登记 CDB、不强制引导启动（保持 Start=3），
	// 由 PnP 管理器在首次启动时安装；此处仅记录其可用性供
	// 宿主机配置参考。
	fixer.offsys.legacyBlockDriver = "viostor"
	fixer.offsys.legacyNetDriver = fixer.existedService("netkvm")

	return nil
}
