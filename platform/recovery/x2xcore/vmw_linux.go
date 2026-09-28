package x2xcore

import (
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/xutil"
	"github.com/pkg/errors"
)

// unconfigVmware 移除Vmware的配置
func (fixer *linuxSystemFixer) unconfigVmware() error {
	logger.Debugf("unconfigVmware: ++")
	defer logger.Debugf("unconfigVmware: --")

	logger.Debugf("unconfigVmware: do nothing")
	fixer.infof(LogTplForUnconfigVmwareWith0Args)

	return nil
}

func (fixer *linuxSystemFixer) configVmware() error {
	logger.Debugf("configVmware: ++")
	defer logger.Debugf("configVmware: --")

	fixer.infof(LogTplForConfigVmwareWith0Args)

	if err := fixer.patchVmware(); err != nil {
		return nil
	}

	return nil
}

// patchVmware 为所有可启动内核打入 VMware 驱动
func (fixer *linuxSystemFixer) patchVmware() error {
	logger.Debugf("patchVmware: ++")
	defer logger.Debugf("patchVmware: --")

	for _, k := range fixer.offsys.kernels {
		if err := fixer.patchOneKernelVmware(k); err != nil {
			// 提示警告，此内核不兼容 VMware 硬件设备
			fixer.warnf(LogTplForVmwarePatchFailedWith1Args, err)
			logger.Warnf("patchVmware: patchOneKernelVmware: %v", err)
			return nil
		}
	}

	return nil
}

// patchOneKernelVmware 为指定内核打入 VMware 驱动
func (fixer *linuxSystemFixer) patchOneKernelVmware(k kernel) error {
	logger.Debugf("patchOneKernelVmware: ++")
	defer logger.Debugf("patchOneKernelVmware: --")

	logger.Debugf("patchOneKernelVmware: Kernel:\n%s", xutil.Pretty(&k))

	if fixer.offsys.root == "" {
		return ErrorRootEnvNotMounted
	}

	if !k.Bootable {
		return errors.Errorf("kernel(%s) is not bootable", k.Name)
	}

	// 启动核心驱动：PVSCSI 是 VMware 的准虚拟化 SCSI 存储控制器，若目标虚拟机
	// 用 PVSCSI 控制器挂载启动盘，initramfs 中必须包含此驱动，否则找不到根设备。
	vmwCoreMods := []string{
		"vmw_pvscsi",
	}

	// 可选附加驱动：网卡与 VMware Tools 通信组件，缺失不影响启动，仅存在时一并打入。
	vmwExtraMods := []string{
		"vmxnet3",                  // VMXNET3 网卡
		"vmw_vmci",                 // VMware VMCI（Tools 通信基础，vsock/vmhgfs 依赖它）
		"vmw_balloon",              // 内存气球驱动
		"vmw_vsock_vmci_transport", // VMCI VSockets（open-vm-tools 通信）
	}

	missedMods := make([]string, 0)

	collect := func(mods []string) {
		for _, m := range mods {
			found, err := fixer.kernelContainsModule(k, m)
			if err != nil {
				logger.Warnf(
					"patchOneKernelVmware: kernelContainsModule(%s, %s): %v",
					k.Name, m, err)
				continue
			}

			if found {
				logger.Debugf(
					"patchOneKernelVmware: module %s found in %s",
					m, k.Name)
				missedMods = append(missedMods, m)
			}
		}
	}

	collect(vmwCoreMods)
	collect(vmwExtraMods)

	logger.Debugf("patchOneKernelVmware: missedMods=%v", missedMods)

	if len(missedMods) == 0 {
		logger.Debugf("patchOneKernelVmware: no vmware modules need to patch")
		return nil
	}

	if err := fixer.initrdAddModule(k, missedMods...); err != nil {
		return err
	}

	return nil
}
