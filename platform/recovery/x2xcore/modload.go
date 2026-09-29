package x2xcore

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
)

type DepGraph map[string][]string
type AliasMap map[string][]string

// name -> best path（核心）
type ModuleIndex map[string]string

// ================================
// 工具函数
// ================================

func matchAlias(pattern, target string) bool {
	// 忽略大小写，兼容不同发行版的 modules.alias 格式（CentOS 7 用大写，新系统用小写）
	ok, err := filepath.Match(strings.ToLower(pattern), strings.ToLower(target))
	return ok && err == nil
}

func moduleName(name string) string {
	name = strings.TrimSuffix(name, ".ko.xz")
	name = strings.TrimSuffix(name, ".ko.zst")
	name = strings.TrimSuffix(name, ".ko")
	return name
}

// 优先级：updates > extra > kernel
func modulePriority(p string) int {
	switch {
	case strings.HasPrefix(p, "updates/"):
		return 3
	case strings.HasPrefix(p, "extra/"):
		return 2
	default:
		return 1
	}
}

// ================================
// 解析 modules.dep
// ================================

func ParseModulesDep(path string) (DepGraph, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	graph := make(DepGraph)
	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.Split(line, ":")
		module := strings.TrimSpace(parts[0])

		var deps []string
		if len(parts) > 1 {
			deps = strings.Fields(parts[1])
		}

		graph[module] = deps
	}

	return graph, scanner.Err()
}

// ================================
// 解析 modules.alias
// ================================

func ParseModulesAlias(path string) (AliasMap, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	aliasMap := make(AliasMap)
	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if !strings.HasPrefix(line, "alias") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}

		pattern := fields[1]
		module := moduleName(fields[2])

		aliasMap[pattern] = append(aliasMap[pattern], module)
	}

	return aliasMap, scanner.Err()
}

func ResolveAlias(aliasMap AliasMap, device string) []string {
	var result []string

	for pattern, modules := range aliasMap {
		if matchAlias(pattern, device) {
			result = append(result, modules...)
		}
	}

	return result
}

// ================================
// 构建 module index（核心）
// ================================

func BuildModuleIndex(graph DepGraph) ModuleIndex {

	index := make(ModuleIndex)

	for p := range graph {

		name := moduleName(path.Base(p))

		if old, ok := index[name]; ok {
			if modulePriority(p) > modulePriority(old) {
				index[name] = p
			}
		} else {
			index[name] = p
		}
	}

	return index
}

// ================================
// DFS 依赖解析
// ================================

func ResolveDeps(graph DepGraph, modules []string) ([]string, error) {

	visited := map[string]bool{}
	temp := map[string]bool{}
	var result []string

	var dfs func(string) error

	dfs = func(m string) error {

		if visited[m] {
			return nil
		}

		if temp[m] {
			return errors.Errorf("cycle detected: %s", m)
		}

		temp[m] = true

		for _, dep := range graph[m] {
			if err := dfs(dep); err != nil {
				return err
			}
		}

		temp[m] = false
		visited[m] = true
		result = append(result, m)

		return nil
	}

	for _, m := range modules {
		if err := dfs(m); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// ================================
// Loader
// ================================

// parseModulesBuiltin 解析 modules.builtin 文件，返回内置模块名集合
func parseModulesBuiltin(filePath string) map[string]bool {
	f, err := os.Open(filePath)
	if err != nil {
		return nil
	}
	defer f.Close()

	builtin := make(map[string]bool)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// line 格式：kernel/drivers/ata/ahci.ko
		// 使用 path.Base 处理 Linux 路径分隔符
		name := moduleName(path.Base(line))
		builtin[name] = true
	}
	return builtin
}

// parseModulesBuiltinModinfo 解析 modules.builtin.modinfo 文件，提取内置模块的 alias
// 格式：kernel/drivers/ata/ahci.ko.alias=pci:v00008086d00008d02sv*sd*bc01sc06i01*
func parseModulesBuiltinModinfo(filePath string) (AliasMap, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	aliasMap := make(AliasMap)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// 只处理 .alias= 行
		const aliasSuffix = ".alias="
		idx := strings.Index(line, aliasSuffix)
		if idx < 0 {
			continue
		}
		pattern := line[idx+len(aliasSuffix):]
		// 提取模块路径部分并去掉 .ko 后缀
		modPath := strings.TrimSuffix(line[:idx], ".ko")
		module := moduleName(path.Base(modPath))
		aliasMap[pattern] = append(aliasMap[pattern], module)
	}
	return aliasMap, scanner.Err()
}

type Loader struct {
	root        string
	kernel      string
	moduleRoot  string
	depGraph    DepGraph
	aliasMap    AliasMap
	moduleIndex ModuleIndex
	builtinMods map[string]bool // 编译进内核的模块名集合
}

// 初始化
func NewModuleLoader(root, kernel string) (*Loader, error) {

	moduleRoot := filepath.Join(root, "lib/modules", kernel)

	depFile := filepath.Join(moduleRoot, "modules.dep")
	aliasFile := filepath.Join(moduleRoot, "modules.alias")
	builtinFile := filepath.Join(moduleRoot, "modules.builtin")
	builtinModinfoFile := filepath.Join(moduleRoot, "modules.builtin.modinfo")

	depGraph, err := ParseModulesDep(depFile)
	if err != nil {
		return nil, err
	}

	aliasMap, _ := ParseModulesAlias(aliasFile)

	// 解析编译进内核的模块
	builtinMods := parseModulesBuiltin(builtinFile)

	// 合并内置模块的 alias（如果 modules.builtin.modinfo 存在）
	if builtinAliases, err := parseModulesBuiltinModinfo(builtinModinfoFile); err == nil {
		for pattern, modules := range builtinAliases {
			aliasMap[pattern] = append(aliasMap[pattern], modules...)
		}
	}

	index := BuildModuleIndex(depGraph)

	return &Loader{
		root:        root,
		kernel:      kernel,
		moduleRoot:  moduleRoot,
		depGraph:    depGraph,
		aliasMap:    aliasMap,
		moduleIndex: index,
		builtinMods: builtinMods,
	}, nil
}

func (l *Loader) String() string {
	return fmt.Sprintf("Loader(root=%s,kernel=%s)", l.root, l.kernel)
}

func (l *Loader) KernelVersion() string {
	return l.kernel
}

// ================================
// 按模块名加载
// ================================

func (l *Loader) LoadModuleByName(name string) ([]string, error) {

	name = moduleName(name)

	fullPath, ok := l.moduleIndex[name]
	if !ok {
		return nil, errors.Errorf("module not found: %s", name)
	}

	return l.build([]string{fullPath})
}

// ================================
// 按设备加载（核心能力）
// ================================

func (l *Loader) LoadByDevice(device string) ([]string, error) {

	modules := ResolveAlias(l.aliasMap, device)

	if len(modules) == 0 {
		return nil, errors.Wrapf(os.ErrNotExist, "no module for device: %s", device)
	}

	var fullModules []string

	for _, m := range modules {
		if p, ok := l.moduleIndex[m]; ok {
			fullModules = append(fullModules, p)
		} else if l.builtinMods != nil && l.builtinMods[m] {
			// 模块已编译进内核，无需额外加载
			continue
		}
	}

	return l.build(fullModules)
}

// ================================
// 构建最终加载顺序
// ================================

func (l *Loader) build(modules []string) ([]string, error) {
	order, err := ResolveDeps(l.depGraph, modules)
	if err != nil {
		return nil, err
	}

	var result []string

	for _, m := range order {

		full := filepath.Join(l.moduleRoot, m)

		if _, err = os.Stat(full); err != nil {
			return nil, errors.Wrapf(err, "failed to access %s", full)
		}

		result = append(result, full)
	}

	return result, nil
}
