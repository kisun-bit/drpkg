package recovery

import (
	"bufio"
	"os"
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
	ok, err := filepath.Match(pattern, target)
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

	for path := range graph {

		name := moduleName(filepath.Base(path))

		if old, ok := index[name]; ok {
			if modulePriority(path) > modulePriority(old) {
				index[name] = path
			}
		} else {
			index[name] = path
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

type Loader struct {
	root        string
	kernel      string
	moduleRoot  string
	depGraph    DepGraph
	aliasMap    AliasMap
	moduleIndex ModuleIndex
}

// 初始化
func NewModuleLoader(root, kernel string) (*Loader, error) {

	moduleRoot := filepath.Join(root, "lib/modules", kernel)

	depFile := filepath.Join(moduleRoot, "modules.dep")
	aliasFile := filepath.Join(moduleRoot, "modules.alias")

	depGraph, err := ParseModulesDep(depFile)
	if err != nil {
		return nil, err
	}

	aliasMap, _ := ParseModulesAlias(aliasFile)

	index := BuildModuleIndex(depGraph)

	return &Loader{
		root:        root,
		kernel:      kernel,
		moduleRoot:  moduleRoot,
		depGraph:    depGraph,
		aliasMap:    aliasMap,
		moduleIndex: index,
	}, nil
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
		return nil, errors.Errorf("no module for device: %s", device)
	}

	var fullModules []string

	for _, m := range modules {
		if p, ok := l.moduleIndex[m]; ok {
			fullModules = append(fullModules, p)
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

		full := filepath.Join(l.root, m)

		if _, err = os.Stat(full); err != nil {
			return nil, errors.Wrapf(err, "failed to access %s", full)
		}

		result = append(result, full)
	}

	return result, nil
}
