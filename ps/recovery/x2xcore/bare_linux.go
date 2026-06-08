package x2xcore

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/ps/bus/pci/universal"
	"github.com/kisun-bit/drpkg/ps/recovery/x2xlib"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
)

func (fixer *linuxSystemFixer) unconfigBareMetal() error {
	logger.Debugf("unconfigBareMetal: ++")
	defer logger.Debugf("unconfigBareMetal: --")

	logger.Debugf("unconfigBareMetal: do nothing")

	return nil
}

func (fixer *linuxSystemFixer) configBareMetal() error {
	logger.Debugf("configBareMetal: ++")
	defer logger.Debugf("configBareMetal: --")

	for _, k := range fixer.offsys.kernels {
		ldr, err := NewModuleLoader(fixer.offsys.root, k.Name)
		if err != nil {
			return err
		}

		_, err = fixer.compatKernel(k, ldr, fixer.opts.RecoveryParam.Target.PciList)
		if err != nil {
			return err
		}
	}

	return nil
}

func (fixer *linuxSystemFixer) compatKernel(k kernel, loader *Loader, pciList []string) (modules []string, err error) {
	logger.Debugf("compatKernel: ++")
	defer logger.Debugf("compatKernel: --")

	logger.Debugf("compatKernel: kernel=`%s`", loader.KernelVersion())
	logger.Debugf("compatKernel: pciList=\n`%v`", extend.Pretty(pciList))

	for _, p := range pciList {

		up, e := universal.UniPciFromString(p)
		if e != nil {
			return nil, e
		}

		ms, e := fixer.compatPci(loader, up)
		if e != nil {
			if funk.InUInt32s(x2xlib.SupportedBusTypes, up.BaseClassId()) {
				return nil, e
			}
			// TODO 其余非存储控制器、网卡、显卡的硬件设备，抛出警告即可
			logger.Warnf("compatKernel: unsupported hardware: %s (%s)", up.Human(), up)
			continue
		}
		logger.Debugf("compatKernel: pci=`%s` modules=`%v`", p, ms)

		if len(ms) != 0 {
			modules = append(modules, ms...)
		}
	}

	if len(modules) != 0 {
		if err = fixer.initrdAddModule(k, modules...); err != nil {
			return nil, errors.Wrap(err, "initrdAddModule")
		}
	}

	return
}

func (fixer *linuxSystemFixer) compatPci(loader *Loader, up *universal.UniPci) (modules []string, err error) {
	logger.Debugf("compatPci: ++")
	defer logger.Debugf("compatPci: --")

	modalias := up.Modalias()
	logger.Debugf("compatPci: pci=`%s` modalias=`%s`", up, modalias)

	modpathList, err := loader.LoadByDevice(modalias)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	if errors.Is(err, os.ErrNotExist) {

		if fixer.x2xLib == nil {
			return nil, errors.Errorf("x2xlib(%s) not found", fixer.opts.RecoveryParam.X2xLibrary)
		}

		// 从驱动库进行匹配
		dr, e := fixer.x2xLib.SelectLinuxBestNormalDriver(
			runtime.GOOS,
			fixer.offsys.distro.Family,
			loader.KernelVersion(),
			up.String())
		if e != nil {
			return nil, errors.Wrapf(e, "SelectLinuxBestNormalDriver")
		}

		logger.Debugf("compatPci: modalias=`%s` driver=\n%s", modalias, extend.Pretty(dr))

		// 注入驱动
		if e = fixer.batchInjectPackage(dr.Dir); e != nil {
			return nil, errors.Wrapf(e, "batchInjectPackage")
		}

		// 再次获取
		modpathList, err = loader.LoadByDevice(dr.Dir)
		if err != nil {
			return nil, errors.Wrapf(err, "LoadByDevice %s", up)
		}
	}

	for _, m := range modpathList {
		filename := filepath.Base(m)
		name := moduleName(filename)
		modules = append(modules, name)
	}

	return modules, nil
}
