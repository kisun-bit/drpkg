package sysrepair

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/ps/bus/pci/universal"
	"github.com/pkg/errors"
)

// LinuxModule 表示内核模块及其依赖
type LinuxModule struct {
	Pci string

	Name     string
	Filename string // 如果是 builtin 则为空

	Dependencies []*LinuxModule
}

type modaliasFunc func(p *universal.UniPci) (string, bool)

// SearchCompatibleLinuxModules 高级版
func SearchCompatibleLinuxModules(rootDir, kernel string, pciList []string) (compatModules []*LinuxModule, incompatPci []string, err error) {
	defer func() {
		err = errors.Wrap(err, "finding compatible linux driver")
	}()

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

	// 解析依赖
	depMap, err := parseModulesDep(depFile)
	if err != nil {
		return nil, pciList, err
	}

	// 扫描所有模块路径
	nameToPath := map[string]string{}
	_ = filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".ko") || strings.HasSuffix(path, ".ko.xz") {
			name := moduleNameFromPath(path)
			nameToPath[name] = path
		}
		return nil
	})

	// PCI 预处理
	type pciInfo struct {
		raw string
		uni *universal.UniPci
	}
	var pciInfos []pciInfo
	for _, pciStr := range pciList {
		uni, err := universal.UniPciFromString(pciStr)
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

			mod, err := buildModuleTreePro(module, nameToPath, depMap, make(map[string]bool))
			if err != nil {
				return err
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
			err = match(alias, module, func(p *universal.UniPci) (string, bool) { return p.Modalias(), true })
		case strings.HasPrefix(line, "alias virtio:"):
			err = match(alias, module, func(p *universal.UniPci) (string, bool) { return p.VirtioModalias() })
		case strings.HasPrefix(line, "alias xen:"):
			err = match(alias, module, func(p *universal.UniPci) (string, bool) { return "", false })
		}
		if err != nil {
			return nil, pciList, err
		}
	}
	if err = scanner.Err(); err != nil {
		return nil, nil, err
	}

	// 输出结果
	for _, p := range pciInfos {
		if mod, ok := compatMap[p.raw]; ok {
			compatModules = append(compatModules, mod)
		} else {
			incompatPci = append(incompatPci, p.raw)
		}
	}

	return
}

// 构建依赖树，支持 builtin
func buildModuleTreePro(moduleName string, nameToPath map[string]string, depMap map[string][]string, visited map[string]bool) (*LinuxModule, error) {
	var modulePath string
	if p, ok := nameToPath[moduleName]; ok {
		modulePath = p
	}

	// builtin 驱动处理
	if modulePath != "" && visited[modulePath] {
		return nil, nil
	}
	if modulePath != "" {
		visited[modulePath] = true
	}

	mod := &LinuxModule{
		Name:     moduleName,
		Filename: modulePath,
	}

	// 依赖处理
	deps := []string{}
	if modulePath != "" {
		if d, ok := depMap[modulePath]; ok {
			deps = d
		}
	}
	for _, dep := range deps {
		childName := moduleNameFromPath(dep)
		child, err := buildModuleTreePro(childName, nameToPath, depMap, visited)
		if err != nil {
			return nil, err
		}
		if child != nil {
			mod.Dependencies = append(mod.Dependencies, child)
		}
	}

	return mod, nil
}

func matchAlias(pattern, target string) bool {
	ok, err := filepath.Match(pattern, target)
	return ok && err == nil
}

func aliasAndDriver(line string) (alias string, module string, ok bool) {
	items := strings.Fields(line)
	if len(items) != 3 {
		return "", "", false
	}
	return items[1], items[2], true
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

func moduleNameFromPath(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, ".ko")
	base = strings.TrimSuffix(base, ".xz")
	return base
}
