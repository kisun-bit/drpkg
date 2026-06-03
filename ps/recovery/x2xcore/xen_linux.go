package x2xcore

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/kisun-bit/drpkg/define"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/pkg/errors"
)

// unconfigXen 移除Xen的配置
func (fixer *linuxSystemFixer) unconfigXen() error {
	logger.Debugf("unconfigXen: ++")
	defer logger.Debugf("unconfigXen: --")

	if err := changeUVPTools(fixer.offsys.root, true); err != nil {
		return err
	}

	// FIXME: xen_platform_pci.dev_unplug=all 貌似只是华为云文档中的参数，并非通用参数，请调研
	//if err := fixer.fixGrubKernelArg("xen_platform_pci.dev_unplug=all", false); err != nil {
	//	return errors.Wrap(err, "xen_platform_pci.dev_unplug")
	//}

	return nil
}

func (fixer *linuxSystemFixer) configXen() error {
	logger.Debugf("configXen: ++")
	defer logger.Debugf("configXen: --")

	if err := changeUVPTools(fixer.offsys.root, false); err != nil {
		return err
	}

	if err := fixer.patchXen(); err != nil {
		return err
	}

	return nil
}

// changeUVPTools 调整华为云的UVPTools
// 华为云主机（基于xen）在安装UVPTools时，会存在如下写入逻辑（https://github.com/UVP-Tools/SAP-HANA-Tools/blob/eeceb65c5b06a4e9283273708906fadaafdc24c9/install#L1334）：
// ###pvdriver<begin> \n\
// # Let load XENPV modules \n\
// ${BALLOONMOD}modprobe xen-balloon >/dev/null 2>&1\n\
// ${HCALLMOD}modprobe xen-hcall >/dev/null 2>&1\n\
// ${VMDQMOD}modprobe xen-vmdq >/dev/null 2>&1\n\
// ${SCSIMOD}modprobe xen-scsifront >/dev/null 2>&1\n\
// ###pvdriver<end>" $RC_SYSINIT >/dev/null 2>&1
func changeUVPTools(rootDir string, deprecated bool) error {
	logger.Debugf(
		"renameXenModules: ++ (deprecated=%v)",
		deprecated,
	)
	defer logger.Debugf("renameXenModules: --")

	modulesDir := filepath.Join(rootDir, "lib", "modules")

	var errs []error

	err := filepath.WalkDir(
		modulesDir,
		func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				logger.Warnf(
					"renameXenModules: walk %s failed: %v",
					path,
					walkErr,
				)
				errs = append(errs, walkErr)
				return nil
			}

			if d.IsDir() {
				return nil
			}

			filename := d.Name()

			var newPath string

			if deprecated {
				// 只处理 xen-hcall
				if !strings.Contains(filename, "xen-hcall") {
					return nil
				}

				// 已弃用
				if strings.HasSuffix(filename, ".deprecated") {
					return nil
				}

				newPath = path + ".deprecated"
			} else {
				// 只恢复 deprecated 文件
				if !strings.HasSuffix(
					filename,
					".deprecated",
				) {
					return nil
				}

				if !strings.Contains(filename, "xen-hcall") {
					return nil
				}

				newPath = strings.TrimSuffix(
					path,
					".deprecated",
				)
			}

			logger.Infof(
				"renameXenModules: rename xen module %s -> %s",
				path,
				newPath,
			)

			if err := os.Rename(path, newPath); err != nil {
				logger.Errorf(
					"renameXenModules: rename failed %s -> %s: %v",
					path,
					newPath,
					err,
				)
				errs = append(errs, err)
			}

			return nil
		},
	)

	if err != nil {
		return errors.Errorf(
			"walk modules dir failed: %v",
			err,
		)
	}

	if len(errs) > 0 {
		return errors.Errorf(
			"rename xen modules partially failed: %v",
			errs,
		)
	}

	return nil
}

func (fixer *linuxSystemFixer) patchXen() error {
	logger.Debugf("patchXen: ++")
	defer logger.Debugf("patchXen: --")

	for _, k := range fixer.offsys.kernels {
		if err := fixer.patchOneKernelXen(k); err != nil {
			// TODO 提示警告，此内核不兼容xen硬件设备
			logger.Warnf("patchXen: patchOneKernelXen: %v", err)
			return nil
		}
	}

	return nil
}

func (fixer *linuxSystemFixer) patchOneKernelXen(k kernel) error {
	logger.Debugf("patchXen: ++")
	defer logger.Debugf("patchXen: --")

	if fixer.offsys.root == "" {
		return ErrorRootEnvNotMounted
	}

	if !k.Bootable {
		return errors.Errorf("kernel(%s) is not bootable", k.Name)
	}

	// 候选驱动集合：
	// 第一组：xen_vnif xen_vbd xen_platform_pci，
	//        注意：SUSE 11 SP1 64bit ~ SUSE 11 SP4 64bit系统需要在“menu.lst”文件添加xen_platform_pci.dev_unplug=all
	// 第二组：xen-blkfront xen-netfront xen-scsifront
	xenCand1Modules := []string{
		"xen-vnif",
		"xen-vbd",
		"xen-platform-pci",
	}

	xenCand2Modules := []string{
		"xen-blkfront",
		"xen-netfront",
		"xen-scsifront",
	}

	xenCand1ModulesFound, _ := fixer.kernelContainsModule(k, xenCand1Modules[0])
	xenCand2ModulesFound, _ := fixer.kernelContainsModule(k, xenCand2Modules[0])

	if !xenCand1ModulesFound && !xenCand2ModulesFound {
		// 操作系统版本低于SUSE 12 SP1或低于openSUSE 13
		isOldSLES := fixer.offsys.distro.ID == "sles" &&
			(fixer.offsys.distro.Major <= 11 ||
				strings.Contains(fixer.offsys.distro.Pretty, "12 SP1"))

		isOldOpenSUSE := fixer.offsys.distro.ID == "opensuse" &&
			fixer.offsys.distro.Major <= 13

		installed := false
		if isOldSLES || isOldOpenSUSE {

			// 离线安装xen相关的包
			name, dir, e := fixer.x2xLib.SelectLinuxBestVirtualDriver(
				define.HPVTXen,
				runtime.GOARCH,
				fixer.offsys.distro.ID,
				k.Name,
				"")
			if e != nil {
				logger.Warnf("patchOneKernelXen: GetLinuxVirtualizationDriver: %v", e)
				return errors.Wrap(e, "xen drivers not installed")
			}
			logger.Debugf("patchOneKernelXen: Package: %s", name)
			if e = fixer.batchInjectPackagesByZypper(dir); e != nil {
				return errors.Wrapf(e, "install %s", name)
			}
			installed = true
			xenCand1ModulesFound = true
		}

		if !installed {
			return errors.Errorf("unsupported xen-based hardware")
		}
	}

	modules := make([]string, 0)

	if xenCand1ModulesFound {
		modules = append(modules, xenCand1Modules...)

		// FIXME: xen_platform_pci.dev_unplug=all 貌似只是华为云文档中的参数，并非通用参数，请调研
		//if err := fixer.fixGrubKernelArg("xen_platform_pci.dev_unplug=all", true); err != nil {
		//	return errors.Wrap(err, "xen_platform_pci.dev_unplug")
		//}
	}

	if xenCand2ModulesFound && len(modules) == 0 {
		for _, module := range xenCand2Modules {
			yes, err := fixer.kernelContainsModule(k, module)
			if err == nil && yes {
				modules = append(modules, module)
			}
		}
	}

	logger.Debugf("patchXen: modules: %v", modules)

	if len(modules) == 0 {
		logger.Debugf("patchXen: no modules need to patch")
		return nil
	}

	if err := fixer.initrdAddModule(k, modules...); err != nil {
		return err
	}

	return nil
}

func (fixer *linuxSystemFixer) fixGrubKernelArg(
	arg string,
	add bool,
) error {
	logger.Debugf(
		"fixGrubKernelArg(%s, add=%v): ++",
		arg,
		add,
	)
	defer logger.Debugf(
		"fixGrubKernelArg(%s, add=%v): --",
		arg,
		add,
	)

	data, err := os.ReadFile(fixer.offsys.grubCfg)
	if err != nil {
		return err
	}

	// 匹配：
	// kernel /vmlinuz ...
	// linux /vmlinuz ...
	// linuxefi /vmlinuz ...
	lineRe := regexp.MustCompile(
		`(?m)^(\s*(?:kernel|linux|linuxefi)\s+\S+.*)$`,
	)

	// 精确匹配 kernel arg
	argRe := regexp.MustCompile(
		`(^|\s+)` +
			regexp.QuoteMeta(arg) +
			`(\s+|$)`,
	)

	content := string(data)

	newContent := lineRe.ReplaceAllStringFunc(
		content,
		func(line string) string {
			hasArg := argRe.MatchString(line)

			// 添加
			if add {
				if hasArg {
					return line
				}

				return line + " " + arg
			}

			// 删除
			if !hasArg {
				return line
			}

			// 删除参数并清理多余空格
			line = argRe.ReplaceAllString(
				line,
				" ",
			)

			// 压缩连续空格
			line = strings.Join(
				strings.Fields(line),
				" ",
			)

			return line
		},
	)

	if newContent == content {
		logger.Debugf("fixGrubKernelArg: %s not changed", fixer.offsys.grubCfg)
		return nil
	}

	err = os.WriteFile(
		fixer.offsys.grubCfg,
		[]byte(newContent),
		0644,
	)
	if err != nil {
		return errors.Errorf(
			"write grub cfg failed: %s, err=%v",
			fixer.offsys.grubCfg,
			err,
		)
	}

	logger.Infof(
		"update grub arg success: %s, arg=%s, add=%v",
		fixer.offsys.grubCfg,
		arg,
		add,
	)

	return nil
}
