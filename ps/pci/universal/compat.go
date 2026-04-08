package universal

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kisun-bit/drpkg/extend"
	"github.com/pkg/errors"
)

type LinuxModule struct {
	Pci string

	Name     string
	Filename string

	Dependencies []*LinuxModule
}

type modaliasFunc func(p *UniPci) (string, bool)

// SearchCompatibleLinuxModules 基于指定内核版本，从 modules.alias / modules.dep 中
// 解析并匹配给定 PCI 设备列表所对应的 Linux 内核驱动模块。
//
// 功能说明：
//  1. 解析 modules.alias，获取“硬件 modalias → 驱动模块”的映射关系
//  2. 根据输入的 PCI 设备列表，生成对应的 modalias（支持 pci / virtio 等）
//  3. 匹配 alias 规则，找到可驱动该设备的内核模块
//  4. 结合 modules.dep 构建完整的模块依赖树（类似 modprobe 行为）
//  5. 输出：
//     - 已匹配的驱动模块（包含依赖关系）
//     - 未匹配到驱动的 PCI 设备
//
// 参数说明：
//
//	rootDir  : Linux 根目录（例如挂载的离线系统 / 镜像路径）
//	kernel   : 内核版本（对应 /lib/modules/<kernel>/）
//	pciList  : 通用 PCI 设备列表（ UniPci 的字符串形式，如：PCI\V5853\D0001\SV5853\SD0001\BC01\SC00\I00\REV02）
//
// 返回值：
//
//	compatModules : 成功匹配到驱动的模块列表（每个模块包含完整依赖树）
//	incompatPci   : 未匹配到任何驱动的 PCI 设备列表
//	err           : 错误信息
//
// 匹配逻辑：
//   - 支持 alias 类型：
//   - alias pci:*     → 标准 PCI 设备
//   - alias virtio:*  → virtio 虚拟设备
//   - alias xen:*     → xen 虚拟设备
//   - 匹配规则基于 Linux modalias 通配符（matchAlias）
//
// 注意事项：
//  1. 仅做“静态匹配”，不保证驱动一定可加载（可能缺固件 / 内核配置不支持）
//  2. builtin 驱动（编译进内核）不会出现在 modules.dep 中，但可能出现在 alias 中
//  3. 同一个 PCI 设备只会匹配第一个成功的驱动（不会返回多个候选驱动）
//  4. 性能复杂度约为：O(alias数量 × PCI数量)
//
// 典型用途：
//   - 离线系统驱动兼容性检测
//   - 虚拟机 / 裸机迁移前驱动校验
//   - 灾备 / 恢复系统的驱动预加载分析
func SearchCompatibleLinuxModules(
	rootDir string,
	kernel string,
	pciList []string,
) (compatModules []*LinuxModule, incompatPci []string, err error) {

	defer func() {
		err = errors.Wrap(err, "finding compatible linux driver")
	}()

	// 参数校验
	if len(pciList) == 0 {
		return nil, pciList, nil
	}
	if !extend.IsExisted(rootDir) {
		return nil, pciList, errors.New("root dir does not exist")
	}
	if kernel == "" {
		return nil, pciList, errors.New("kernel is empty")
	}

	baseDir := filepath.Join(rootDir, "lib/modules", kernel)
	aliasFile := filepath.Join(baseDir, "modules.alias")
	depFile := filepath.Join(baseDir, "modules.dep")

	depMap, err := parseModulesDep(depFile)
	if err != nil {
		return nil, pciList, err
	}

	// PCI预处理
	type pciInfo struct {
		raw string
		uni *UniPci
	}

	var pciInfos []pciInfo
	for _, pciStr := range pciList {
		uni, err := UniPciFromString(pciStr)
		if err != nil {
			return nil, pciList, err
		}
		pciInfos = append(pciInfos, pciInfo{pciStr, uni})
	}

	// 打开 alias 文件
	f, err := os.Open(aliasFile)
	if err != nil {
		return nil, pciList, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)

	compatMap := make(map[string]*LinuxModule)

	// 通用匹配函数
	match := func(alias, module string, getModalias modaliasFunc) error {
		for _, p := range pciInfos {
			if _, ok := compatMap[p.raw]; ok {
				continue
			}

			modalias, ok := getModalias(p.uni)
			if !ok || !matchAlias(alias, modalias) {
				continue
			}

			mod, err := buildModuleTree(baseDir, module, depMap, make(map[string]bool))
			if err != nil {
				return err
			}
			if mod == nil {
				continue
			}

			mod.Pci = p.raw
			compatMap[p.raw] = mod
		}
		return nil
	}

	for scanner.Scan() {
		line := scanner.Text()

		alias, module, ok := aliasAndDriver(line)
		if !ok {
			continue
		}

		switch {
		case strings.HasPrefix(line, "alias pci:"):
			err = match(alias, module, func(p *UniPci) (string, bool) {
				return p.Modalias(), true
			})

		case strings.HasPrefix(line, "alias virtio"):
			err = match(alias, module, func(p *UniPci) (string, bool) {
				return p.VirtioModalias()
			})

		case strings.HasPrefix(line, "alias xen:"):
			// TODO 后续扩展 xen
		}

		if err != nil {
			return nil, pciList, err
		}
	}

	if err = scanner.Err(); err != nil {
		return nil, nil, err
	}

	// 整理结果
	for _, p := range pciInfos {
		if mod, ok := compatMap[p.raw]; ok {
			compatModules = append(compatModules, mod)
		} else {
			incompatPci = append(incompatPci, p.raw)
		}
	}

	return
}

func matchAlias(pattern, target string) bool {
	// modules.alias 用的是类似 glob 的匹配（*）
	// 直接用 filepath.Match 不完全兼容，但够用

	ok, err := filepath.Match(pattern, target)
	if err != nil {
		return false
	}
	return ok
}

func aliasAndDriver(line string) (alias string, module string, ok bool) {
	items := strings.Fields(line)
	if len(items) != 3 {
		return "", "", false
	}

	alias = items[1]
	module = items[2]

	return alias, module, true
}

func parseModulesDep(path string) (map[string][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	result := make(map[string][]string)

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()

		// 格式：
		// kernel/drivers/.../xxx.ko: dep1.ko dep2.ko
		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			continue
		}

		mod := strings.TrimSpace(parts[0])
		deps := strings.Fields(parts[1])

		result[mod] = deps
	}

	return result, scanner.Err()
}

func buildModuleTree(baseDir, moduleName string, depMap map[string][]string, visited map[string]bool) (*LinuxModule, error) {
	// 找到模块路径
	modulePath := findModuleFile(baseDir, moduleName)
	if modulePath == "" {
		return nil, fmt.Errorf("module %s not found", moduleName)
	}

	if visited[modulePath] {
		return nil, nil
	}
	visited[modulePath] = true

	mod := &LinuxModule{
		Name:     moduleName,
		Filename: modulePath,
	}

	deps := depMap[modulePath]

	for _, dep := range deps {
		child, err := buildModuleTree(baseDir, moduleNameFromPath(dep), depMap, visited)
		if err != nil {
			return nil, err
		}
		if child != nil {
			mod.Dependencies = append(mod.Dependencies, child)
		}
	}

	return mod, nil
}

func findModuleFile(baseDir, moduleName string) string {
	var result string

	_ = filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if strings.HasSuffix(path, moduleName+".ko") ||
			strings.HasSuffix(path, moduleName+".ko.xz") {
			result = path
			return filepath.SkipDir
		}
		return nil
	})

	return result
}

func moduleNameFromPath(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, ".ko")
	base = strings.TrimSuffix(base, ".xz")
	return base
}
