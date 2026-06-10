package x2xcore

import (
	"github.com/kisun-bit/drpkg/define"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/pkg/errors"
)

// unconfigKvm 移除KVM的配置
func (fixer *linuxSystemFixer) unconfigKvm() error {
	logger.Debugf("unconfigKvm: ++")
	defer logger.Debugf("unconfigKvm: --")

	logger.Debugf("unconfigKvm: do nothing")

	return nil
}

func (fixer *linuxSystemFixer) configKvm() error {
	logger.Debugf("configKvm: ++")
	defer logger.Debugf("configKvm: --")

	if err := fixer.patchVirtIO(); err != nil {
		return nil
	}

	return nil
}

func (fixer *linuxSystemFixer) patchVirtIO() error {
	logger.Debugf("patchVirtIO: ++")
	defer logger.Debugf("patchVirtIO: --")

	for _, k := range fixer.offsys.kernels {
		if err := fixer.patchOneKernelVirtIO(k); err != nil {
			// TODO 提示警告，此内核不兼容virtio硬件设备
			logger.Warnf(
				"patchVirtIO: patchOneKernelVirtIO: %v, disk-bus changes to `%s`",
				err, define.DiskBusSata)
			fixer.offsys.kvmDiskBus = define.DiskBusSata
			return nil
		}
	}

	return nil
}

// patchOneKernelVirtIO 为指定内核打入VirtIO
func (fixer *linuxSystemFixer) patchOneKernelVirtIO(k kernel) error {
	logger.Debugf("patchOneKernelVirtIO: ++")
	defer logger.Debugf("patchOneKernelVirtIO: --")

	logger.Debugf("patchOneKernelVirtIO: Kernel:\n%s", extend.Pretty(&k))

	if fixer.offsys.root == "" {
		return ErrorRootEnvNotMounted
	}

	if !k.Bootable {
		return errors.Errorf("kernel(%s) is not bootable", k.Name)
	}

	v, ok := k.KConfigs["CONFIG_VIRTIO"]
	logger.Debugf("patchOneKernelVirtIO: CONFIG_VIRTIO=`%v` existed=%v", v, ok)

	if !ok || (v != "m" && v != "y") {
		// TODO 从驱动库下载virtio
		logger.Warnf("patchOneKernelVirtIO: CONFIG_VIRTIO not found")
		return errors.New("CONFIG_VIRTIO not configured")
	}

	//
	// virtio已被内建支持或模块支持
	//

	// virtio最小可启动模块
	minBootMods := []string{
		"virtio",
		"virtio_ring",
		"virtio_pci",
		"virtio_scsi",
		"virtio_blk",
		"virtio_net",
	}

	logger.Debugf("patchOneKernelVirtIO: minBootMods=%v", minBootMods)

	//initrdPath := filepath.Join(fixer.offsys.root, "boot", k.Initrd)
	//cmdline := fmt.Sprintf("lsinitrd %s", initrdPath)
	//_, lsinitrdOutput, e := command.Execute(cmdline)
	//if e != nil {
	//	return errors.Wrapf(e, "execute `%s`", cmdline)
	//}

	// 本次需要打入initrd的模块
	missedMods := make([]string, 0)

	for _, m := range minBootMods {
		mval := ""
		mok := false

		switch m {
		case "virtio":
			mval, mok = k.KConfigs["CONFIG_VIRTIO"]
		case "virtio_ring":
			mval, mok = k.KConfigs["CONFIG_VIRTIO_RING"]
		case "virtio_pci":
			mval, mok = k.KConfigs["CONFIG_VIRTIO_PCI"]
		case "virtio_scsi":
			mval, mok = k.KConfigs["CONFIG_SCSI_VIRTIO"]
		case "virtio_blk":
			mval, mok = k.KConfigs["CONFIG_VIRTIO_BLK"]
		case "virtio_net":
			mval, mok = k.KConfigs["CONFIG_VIRTIO_NET"]
		}

		// 找不到就从driver目录中查找
		if !mok {
			logger.Warnf("patchOneKernelVirtIO: KCONFIG of %s not found", m)
			foundFromLib, _ := fixer.kernelContainsModule(k, m)
			if foundFromLib {
				logger.Debugf("patchOneKernelVirtIO: module file of %s found in %s", m, k.Name)
				missedMods = append(missedMods, m)
			} else {
				if m == "virtio_scsi" {
					logger.Debugf(
						"patchOneKernelVirtIO: module file of %s found in %s, change `%s` to `%s`",
						m,
						k.Name,
						fixer.offsys.kvmDiskBus,
						define.DiskBusVirtio)
					fixer.offsys.kvmDiskBus = define.DiskBusVirtio
				}
			}
			continue
		}

		logger.Debugf("patchOneKernelVirtIO: module=%s kconfig=%s", m, mval)

		// builtin支持
		if mval == "y" {
			continue
		}

		if mval != "m" {
			return errors.Errorf("KCONFIG of %s is `%s`", m, mval)
		}

		//// 驱动是否已打入initrd
		//patched := false
		//for _, line := range strings.Split(lsinitrdOutput, "\n") {
		//	line = strings.TrimSpace(line)
		//	if line == "" || !strings.Contains(line, m) {
		//		continue
		//	}
		//	items := strings.Fields(line)
		//	rf := items[len(items)-1]
		//	fn := filepath.Base(rf)
		//	if strings.Contains(fn, ".") && moduleName(fn) == m {
		//		patched = true
		//		break
		//	}
		//}
		//
		//if !patched {
		//	missedMods = append(missedMods, m)
		//}

		// 内核配置为m的模块，如果发现lsinitrd时此模块已经在initrd文件中存在，那么也需要重新打入
		missedMods = append(missedMods, m)
	}

	logger.Debugf("patchOneKernelVirtIO: missedMods=%v", missedMods)

	if err := fixer.initrdAddModule(k, missedMods...); err != nil {
		return err
	}
	return nil
}
