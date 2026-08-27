//go:build linux

package nbd

// 本文件实现设备堆栈（device stack）清理：当一个 nbd 设备被映射
// 出上层块设备时（分区 /dev/nbdNpP、LVM 逻辑卷、dm-crypt、md/raid
// 等），直接执行 `qemu-nbd --disconnect` 会失败。断开前必须先把
// 这些上层依赖全部移除，断开后还要等待分区节点从内核消失。
//
// 依赖发现不依赖 lvm2/cryptsetup 等工具，而是直接读内核的
// /sys/class/block/<dev>/holders 关系（整盘与分区在 class/block
// 下都有独立条目，统一处理）：
//
//	/sys/class/block/nbd2/holders     -> 直接建在整盘上的 dm 设备
//	/sys/class/block/nbd2p1/holders   -> 建在分区上的 LVM、dm-crypt 等
//	.../holders/dm-3/holders/dm-4     -> dm 之上再叠 dm（加密卷上的 LVM）
//
// 注意：分区不会出现在整盘设备的 holders 里，建在分区上的上层设备
// 只会出现在分区自己的 holders 中，因此遍历种子必须同时包含整盘
// 设备与它的全部分区。
//
// 移除顺序为"先顶层后底层"：按依赖图中每个设备上方最长链的长度
// 排序（纯逻辑见 stackorder.go），先停用 swap、再 dmsetup remove
// 移除 dm 设备、mdadm --stop 停止 md 阵列；分区节点在父设备断开
// 或停止后由内核自动回收，等待消失即可。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kisun-bit/drpkg/logger"
	"github.com/pkg/errors"
)

const (
	dmsetupCaller = "dmsetup"
	mdadmCaller   = "mdadm"
	swapoffCaller = "swapoff"

	// StackRemoveTimeout 是单个堆栈设备的移除超时；
	// PartitionsSettleTimeout 是断开后等待分区节点消失的上限。
	StackRemoveTimeout      = 15 * time.Second
	PartitionsSettleTimeout = DeviceReadyTimeout
)

// StackCleanupResult 汇总一次设备堆栈清理的实际动作，
// 便于调用方记录日志或审计。
type StackCleanupResult struct {
	// RemovedDM 是按移除顺序（自顶向下）被移除的 dm 设备名（dm-0 风格）。
	RemovedDM []string
	// RemovedMD 是被停止的 md 设备名（md0 风格）。
	RemovedMD []string
	// SwappedOff 是被停用的 swap 分区节点路径。
	SwappedOff []string
	// Partitions 是清理时在该设备上发现的分区节点路径；
	// 分区无需主动移除，设备断开后由内核自动回收。
	Partitions []string
}

// CleanupDeviceStack 清理指定 nbd 设备之上的全部块设备堆栈：
// 整盘或分区之上叠加的 dm 设备（LVM、dm-crypt、linear 等）与
// md 设备会被移除，作为 swap 使用的分区/设备会先被停用。
//
// 该操作是破坏性的：被发现的逻辑卷、加密映射、raid 阵列会被停用，
// 其上未同步的数据可能丢失。Disconnect/Release/Close 会在断开前
// 自动调用本方法，一般无需手工调用。
//
// 无法移除的设备（例如仍被挂载占用、命令失败）不会中断整体流程，
// 而是记录警告并汇总到返回的错误中。
func (m *Manager) CleanupDeviceStack(index int) (*StackCleanupResult, error) {
	if err := m.checkInit(); err != nil {
		return nil, err
	}

	devName := fmt.Sprintf("nbd%d", index)
	partPrefix := devName + "p"

	// 遍历种子：整盘设备 + 它的全部分区。
	// 建在分区上的 LVM、加密等设备只出现在分区自己的 holders 里。
	partitions := listBlockNames(partPrefix)
	seeds := append([]string{devName}, partitions...)

	graph, err := buildStackGraph(seeds, readHolders)
	if err != nil {
		return nil, errors.Wrapf(err, "scan stack of %s", devName)
	}

	result := &StackCleanupResult{}
	for _, part := range partitions {
		result.Partitions = append(result.Partitions, filepath.Join(devDir, part))
	}

	order := orderForRemoval(graph)

	// 整盘与分区之上都没有上层设备时无事可做。
	if len(order) == len(seeds) {
		return result, nil
	}

	logger.Debugf("nbd%d device stack (top-down): %s", index, formatStack(order))

	// 1. 停用堆栈内所有充当 swap 的分区/设备（/proc/swaps 以 /dev 路径呈现）。
	result.SwappedOff = disableStackSwaps(order)

	// 2. 按自顶向下顺序移除 dm/md 设备；分区无需主动移除。
	var errs []string
	var removalTargets []string

	for _, name := range order {
		if name == devName || partitionSuffixRegexp.MatchString(name) {
			// 整盘由 qemu-nbd 断开；分区（含 md 的分区）在父设备
			// 断开/停止后由内核自动回收。
			continue
		}
		removalTargets = append(removalTargets, name)

		switch {
		case strings.HasPrefix(name, "dm-"):
			if err = removeDM(name); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", name, err))
				continue
			}
			result.RemovedDM = append(result.RemovedDM, name)

		case strings.HasPrefix(name, "md"):
			if err = stopMD(name); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", name, err))
				continue
			}
			result.RemovedMD = append(result.RemovedMD, name)

		default:
			// 未知类型的持有者（既不是 dm 也不是 md）：记录警告，不阻塞流程。
			logger.Warnf("nbd%d: skip unknown holder device %s", index, name)
		}
	}

	// 3. 复核：应移除但仍存在的设备会导致断开失败，汇总报错。
	if remaining := filterExisting(removalTargets); len(remaining) > 0 {
		errs = append(errs, fmt.Sprintf("devices still in use: %s", formatStack(remaining)))
	}

	if len(errs) > 0 {
		return result, errors.Errorf("cleanup stack of %s failed: %s",
			devName, strings.Join(errs, "; "))
	}

	return result, nil
}

// readHolders 读取 /sys/class/block/<name>/holders 下的直接持有者。
// 设备不存在时返回空列表（不视为错误）。
func readHolders(name string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(sysClassBlockDir, name, "holders"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}

	sort.Strings(names)

	return names, nil
}

// listBlockNames 返回 /sys/class/block 下匹配前缀的设备名（升序）。
func listBlockNames(prefix string) []string {
	entries, err := os.ReadDir(sysClassBlockDir)
	if err != nil {
		return nil
	}

	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			names = append(names, e.Name())
		}
	}

	sort.Strings(names)

	return names
}

// filterExisting 过滤出仍然存在于 /sys/class/block 的设备。
func filterExisting(names []string) []string {
	var remaining []string
	for _, name := range names {
		if pathExists(filepath.Join(sysClassBlockDir, name)) {
			remaining = append(remaining, name)
		}
	}

	return remaining
}

// disableStackSwaps 停用堆栈设备对应的 swap 分区。
// /proc/swaps 中记录的是 /dev 路径；dm 设备以其 /dev/dm-N 路径匹配。
func disableStackSwaps(names []string) []string {
	swaps := readProcSwaps()
	if len(swaps) == 0 {
		return nil
	}

	var swappedOff []string

	for _, name := range names {
		target := filepath.Join(devDir, name)
		if !swaps[target] {
			continue
		}

		if err := runTool(context.Background(), swapoffCaller+" "+target); err != nil {
			logger.Warnf("swapoff %s: %v", target, err)
			continue
		}

		logger.Debugf("swapoff %s", target)
		swappedOff = append(swappedOff, target)
	}

	return swappedOff
}

// readProcSwaps 解析 /proc/swaps，返回处于激活状态的 swap 路径集合。
// 首行是标题（Filename Type Size Used Priority），解析失败返回空。
func readProcSwaps() map[string]bool {
	data, err := os.ReadFile("/proc/swaps")
	if err != nil {
		return nil
	}

	set := make(map[string]bool)

	for i, line := range strings.Split(string(data), "\n") {
		if i == 0 {
			continue // 标题行
		}

		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] == "" || fields[1] != "partition" {
			continue
		}

		set[fields[0]] = true
	}

	return set
}

// removeDM 移除一个 dm 设备；设备繁忙时先重试一次强制移除。
func removeDM(name string) error {
	target := filepath.Join(devDir, name)

	ctx, cancel := context.WithTimeout(context.Background(), StackRemoveTimeout)
	defer cancel()

	if err := runTool(ctx, dmsetupCaller+" remove "+target); err == nil {
		return nil
	}

	// 仍被打开（如 udev 规则延迟释放句柄）时强制移除。
	if err := runTool(ctx, dmsetupCaller+" remove --force "+target); err != nil {
		return err
	}

	logger.Debugf("dmsetup remove %s (forced)", name)

	return nil
}

// stopMD 停止一个 md 阵列。
func stopMD(name string) error {
	target := filepath.Join(devDir, name)

	ctx, cancel := context.WithTimeout(context.Background(), StackRemoveTimeout)
	defer cancel()

	return runTool(ctx, mdadmCaller+" --stop "+target)
}

// waitPartitionsGone 等待 nbd 设备的全部分区节点从 /dev 消失。
// 断开连接后内核需要短暂时间回收分区节点；若超时仍有分区残留，
// 通常意味着仍有上层依赖未被清理。
func waitPartitionsGone(index int, timeout time.Duration) error {
	prefix := fmt.Sprintf("nbd%dp", index)
	deadline := time.Now().Add(timeout)

	for {
		remaining := listPartitionNodes(prefix)
		if len(remaining) == 0 {
			return nil
		}

		if time.Now().After(deadline) {
			return errors.Errorf(
				"wait partitions of nbd%d gone timeout (%v): %v", index, timeout, remaining)
		}

		time.Sleep(DevicePollInterval)
	}
}

// listPartitionNodes 返回 /dev 下匹配前缀的分区节点路径（升序）。
func listPartitionNodes(prefix string) []string {
	entries, err := os.ReadDir(devDir)
	if err != nil {
		return nil
	}

	var nodes []string

	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			nodes = append(nodes, filepath.Join(devDir, e.Name()))
		}
	}

	sort.Strings(nodes)

	return nodes
}

// formatStack 把设备列表格式化为 "dm-4 -> dm-3 -> nbd2p1" 的可读形式。
func formatStack(names []string) string {
	return strings.Join(names, " -> ")
}
