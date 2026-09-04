package x2xcore

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kisun-bit/drpkg/command"
	"github.com/kisun-bit/drpkg/xutil"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/platform/bus/pci/universal"
	"github.com/kisun-bit/drpkg/platform/recovery/x2xlib"
	"github.com/pkg/errors"
)

// msuAlreadyInstalledToken 是 DISM 报告目标包已安装时输出的错误码
// （CBS_E_ALREADY_INSTALLED）。错误码为十六进制常量，不随系统语言
// 本地化，可安全用于输出匹配。已安装的包视为幂等成功，跳过即可。
const msuAlreadyInstalledToken = "0x800f081e"

// dismAlreadyInstalledExit 是 DISM 报告目标包已安装时的进程返回码
// （CBS_E_ALREADY_INSTALLED），DISM 通过进程退出码直接报告该错误。
const dismAlreadyInstalledExit uint32 = 0x800F081E

// isMsuAlreadyInstalled 依据 DISM 的返回值判断目标包是否已安装：
//
//   - DISM 返回码为 dismAlreadyInstalledExit：离线系统已安装过
//     该包，视为幂等成功，跳过；
//   - 返回码非 0 但输出中包含已安装错误码：同上，作为返回码
//     在传递过程中被包装或丢失时的兜底；
//   - 其余情况：视为真实安装失败。
//
// 注意：返回码 0 表示 DISM 本次安装成功，不属于"已安装"。
func isMsuAlreadyInstalled(exit int, output string) bool {
	if uint32(exit) == dismAlreadyInstalledExit {
		return true
	}
	return strings.Contains(strings.ToLower(output), msuAlreadyInstalledToken)
}

// listMsuPackages 扫描驱动资源目录顶层的 MSU 安装包，
// 按文件名（不区分大小写）升序返回完整路径列表。
// 目录不含 MSU 时返回空列表。
func listMsuPackages(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, errors.Wrapf(err, "read driver dir %s", dir)
	}

	msus := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), x2xlib.MsuExt) {
			msus = append(msus, filepath.Join(dir, entry.Name()))
		}
	}

	sort.Slice(msus, func(i, j int) bool {
		return strings.ToLower(msus[i]) < strings.ToLower(msus[j])
	})

	return msus, nil
}

// planMsuInstallOrder 规划 MSU 安装包的安装顺序。
//
//   - 目录下不存在 order 文件：各包之间无顺序要求，
//     按文件名字母序安装（入参 msus 已排序，原样返回）；
//   - 目录下存在 order 文件：每行一个 MSU 文件名，
//     行序即安装顺序。
//
// 存在 order 文件时严格校验：目录中的每个 MSU 都必须在
// order 中恰好出现一次，且 order 引用的文件必须存在，
// 否则视为驱动库配置错误，直接报错，避免以不确定的顺序
// 安装补丁导致系统损坏。
func planMsuInstallOrder(dir string, msus []string) ([]string, error) {
	orderPath := filepath.Join(dir, x2xlib.MsuOrderFileName)

	data, err := os.ReadFile(orderPath)
	if err != nil {
		if os.IsNotExist(err) {
			return msus, nil
		}
		return nil, errors.Wrapf(err, "read order file %s", orderPath)
	}

	// 文件名（小写） -> 完整路径
	byName := make(map[string]string, len(msus))
	for _, p := range msus {
		byName[strings.ToLower(filepath.Base(p))] = p
	}

	planned := make([]string, 0, len(msus))
	used := make(map[string]bool, len(msus))

	for i, raw := range strings.Split(string(data), "\n") {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}

		key := strings.ToLower(name)

		full, ok := byName[key]
		if !ok {
			return nil, errors.Errorf(
				"order line %d references non-existent MSU package %q",
				i+1, name)
		}
		if used[key] {
			return nil, errors.Errorf(
				"order lists MSU package %q more than once", name)
		}

		used[key] = true
		planned = append(planned, full)
	}

	if len(planned) != len(msus) {
		missing := make([]string, 0)
		for name, full := range byName {
			if !used[name] {
				missing = append(missing, filepath.Base(full))
			}
		}
		sort.Strings(missing)
		return nil, errors.Errorf(
			"order file does not cover all MSU packages, missing: %s",
			strings.Join(missing, ", "))
	}

	return planned, nil
}

// injectMsuPackages 通过 DISM /Add-Package 向离线系统安装
// MSU 格式的安装包（如 Win7 NVMe 支持补丁）。
//
// 处理流程：
//  1. 在 PE 本地创建临时目录，安装包先拷贝到这里再注入，
//     注入完成后删除副本，目录在结束时统一清理。
//  2. 卸载离线 SYSTEM 注册表（DISM 要求独占访问注册表）。
//  3. 按 order 文件规划安装顺序。
//  4. 逐个安装；依据 DISM 返回码判断离线系统已安装过的包，跳过（幂等）。
func (fixer *windowsSystemFixer) injectMsuPackages(ds *x2xlib.DriverResource) error {
	logger.Debugf("injectMsuPackages: begin")
	defer logger.Debugf("injectMsuPackages: end")

	msus, err := listMsuPackages(ds.Dir)
	if err != nil {
		return err
	}
	if len(msus) == 0 {
		return errors.Errorf("no MSU packages in %s", ds.Dir)
	}

	planned, err := planMsuInstallOrder(ds.Dir, msus)
	if err != nil {
		return err
	}

	// 在 PE 本地创建临时目录：安装包先拷贝到这里再注入，
	// 避免 DISM 直接访问驱动库所在介质；注入完成后清理。
	tmpDir, err := os.MkdirTemp("", "msu-inject")
	if err != nil {
		return errors.Wrap(err, "create temp dir for MSU injection")
	}
	defer func() {
		if e := os.RemoveAll(tmpDir); e != nil {
			logger.Warnf("injectMsuPackages: remove temp dir %s failed: %v", tmpDir, e)
		}
	}()

	// DISM requires the SYSTEM hive to be unloaded.
	logger.Debugf("injectMsuPackages: unloading offline SYSTEM hive")
	if err := fixer.unloadSystemRegistry(); err != nil {
		return err
	}
	defer func() {
		logger.Debugf("injectMsuPackages: reloading offline SYSTEM hive")
		if err := fixer.loadSystemRegistry(); err != nil {
			logger.Warnf("injectMsuPackages: reload SYSTEM hive failed: %v", err)
		}
	}()

	skipped := 0
	for i, pkg := range planned {
		logger.Infof("injectMsuPackages: [%d/%d] installing %s", i+1, len(planned), pkg)

		// 拷贝到 PE 本地临时目录后注入，注入完立即删除副本
		localPkg := filepath.Join(tmpDir, filepath.Base(pkg))
		if err := xutil.CopyFile(pkg, localPkg, 0o644); err != nil {
			return errors.Wrapf(err, "copy %s -> %s", pkg, localPkg)
		}

		exit, output, e := command.ExecuteArgs(
			fixer.getDismProgram(),
			[]string{
				fmt.Sprintf("/Image:%s:\\", fixer.offsys.sysVolumeLtr),
				"/Add-Package",
				fmt.Sprintf("/PackagePath:%s", localPkg),
			},
			command.WithDebug(),
		)
		if e := os.Remove(localPkg); e != nil {
			logger.Warnf("injectMsuPackages: remove temp package %s failed: %v", localPkg, e)
		}
		if isMsuAlreadyInstalled(exit, output) {
			skipped++
			logger.Warnf("injectMsuPackages: %s already installed, skipped", pkg)
			continue
		}
		if e != nil {
			logger.Errorf("injectMsuPackages: install %s failed\n%s", pkg, output)
			return errors.Wrapf(e, "install MSU package (%s)", filepath.Base(pkg))
		}
	}

	logger.Infof(
		"injectMsuPackages: done, %d installed, %d skipped (already installed), dir=%s",
		len(planned)-skipped,
		skipped,
		ds.Dir,
	)

	return nil
}

// injectWindowsDriver 向离线系统（DriverStore 型）注入驱动库中的
// 普通硬件驱动，按驱动资源目录内容分派：
//
//   - 目录含 .msu 安装包：走 DISM /Add-Package 安装包逻辑；
//   - 否则：走现有 DISM /Add-Driver 注入逻辑。
func (fixer *windowsSystemFixer) injectWindowsDriver(ds *x2xlib.DriverResource) error {
	msus, err := listMsuPackages(ds.Dir)
	if err != nil {
		return err
	}
	if len(msus) > 0 {
		return fixer.injectMsuPackages(ds)
	}
	return fixer.injectDriversByDism(ds)
}

// injectWindowsDriverLegacy 向离线系统（传统 CDB 型）注入驱动库中的
// 普通硬件驱动，按驱动资源目录内容分派：
//
//   - 目录含 .msu 安装包：走 DISM /Add-Package 安装包逻辑；
//   - 否则：走现有文件复制 + 创建服务 + 登记 CDB 的方式注入。
func (fixer *windowsSystemFixer) injectWindowsDriverLegacy(
	ds *x2xlib.DriverResource,
	up *universal.UniPci,
) error {
	msus, err := listMsuPackages(ds.Dir)
	if err != nil {
		return err
	}
	if len(msus) > 0 {
		return fixer.injectMsuPackages(ds)
	}
	return fixer.injectNormalDriverLegacy(ds, up)
}
