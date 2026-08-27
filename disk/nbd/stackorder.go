package nbd

// 本文件提供设备堆栈的依赖图构建与移除顺序计算。
// 两者都是纯逻辑（持有者读取函数由调用方注入），跨平台可用，
// 便于单元测试；Linux 下的 sysfs 实现见 stack.go。

import (
	"regexp"
	"sort"
)

// partitionSuffixRegexp 匹配分区设备名的后缀（如 nbd0p1、md0p1）。
var partitionSuffixRegexp = regexp.MustCompile(`p[0-9]+$`)

// buildStackGraph 从种子设备出发，用 holdersOf 逐个读取设备的直接
// 持有者（建立在其上的块设备），递归构建完整依赖图：
// 设备名 -> 其持有者列表。已访问的设备不会重复展开。
//
// 种子必须是整盘设备与它的全部分区：建在分区上的 LVM、加密等
// 上层设备只会出现在分区的 holders 里，只从整盘设备出发会漏掉。
func buildStackGraph(seeds []string, holdersOf func(string) ([]string, error)) (map[string][]string, error) {
	graph := make(map[string][]string)
	visited := make(map[string]bool)

	var visit func(name string) error
	visit = func(name string) error {
		if visited[name] {
			return nil
		}
		visited[name] = true

		holders, err := holdersOf(name)
		if err != nil {
			return err
		}

		graph[name] = holders
		for _, holder := range holders {
			if err := visit(holder); err != nil {
				return err
			}
		}

		return nil
	}

	for _, seed := range seeds {
		if err := visit(seed); err != nil {
			return nil, err
		}
	}

	return graph, nil
}

// orderForRemoval 计算自顶向下的移除顺序：若设备 B 建立在设备 A
// 之上，则 B 必须先于 A 移除。
//
// 排序依据是每个设备"上方最长依赖链"的长度：最顶层设备（没有
// 持有者）长度为 0 排最前，越靠近底层长度越大越靠后。对图中任意
// 依赖边，上层设备的长度一定小于下层，因此该顺序天然是合法的
// 移除顺序；同长度按名字排序，结果确定。图中出现环时按无环处理
// （防御性截断），不会死循环。
func orderForRemoval(graph map[string][]string) []string {
	height := make(map[string]int, len(graph))

	var longest func(name string) int
	longest = func(name string) int {
		if h, ok := height[name]; ok {
			return h
		}

		height[name] = 0 // 占位，防止依赖图中出现环时死循环
		max := 0
		for _, holder := range graph[name] {
			if h := longest(holder) + 1; h > max {
				max = h
			}
		}
		height[name] = max

		return max
	}

	names := make([]string, 0, len(graph))
	for name := range graph {
		names = append(names, name)
		longest(name)
	}

	sort.Slice(names, func(i, j int) bool {
		if height[names[i]] != height[names[j]] {
			return height[names[i]] < height[names[j]]
		}
		return names[i] < names[j]
	})

	return names
}
