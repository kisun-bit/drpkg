//go:build linux

package x2xcore

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kisun-bit/drpkg/command"
	"github.com/kisun-bit/drpkg/xutil"
	"github.com/kisun-bit/drpkg/logger"
)

// ======================================================================
// 主入口：完整挂载 Btrfs 文件系统
// ======================================================================

// MountBtrfsFull 将 Btrfs 文件系统完整挂载到 mountpoint。
//
// 流程：
//
//  1. 创建临时 top-level mountpoint。
//  2. 以 subvolid=5 挂载 Btrfs，获得完整文件系统视图。
//  3. 枚举所有 subvolume。
//  4. 识别系统根 subvolume。
//  5. 卸载临时 top-level mount。
//  6. 将系统根 subvolume 挂载到最终 mountpoint。
//  7. 根据 fstab / 启发式规则挂载其他 subvolume。
//
// 返回值：是否成功识别并挂载了系统根。
func MountBtrfsFull(
	ctx context.Context,
	device string,
	mountpoint string,
	readonly bool,
) (bool, error) {
	logger.Debugf(
		"MountBtrfsFull() device=%s mountpoint=%s readonly=%v",
		device,
		mountpoint,
		readonly,
	)

	mountpoint = filepath.Clean(mountpoint)

	// ------------------------------------------------------------------
	// 0. 确保最终 mountpoint 存在。
	// ------------------------------------------------------------------
	if err := os.MkdirAll(mountpoint, 0755); err != nil {
		return false, fmt.Errorf(
			"create mountpoint %s failed: %v",
			mountpoint,
			err,
		)
	}

	// ------------------------------------------------------------------
	// 1. 创建临时 top-level mountpoint。
	// ------------------------------------------------------------------
	topLevelDir, err := os.MkdirTemp(
		filepath.Dir(mountpoint),
		".btrfs-top-*",
	)
	if err != nil {
		return false, fmt.Errorf(
			"create btrfs temporary mountpoint failed: %v",
			err,
		)
	}

	logger.Debugf(
		"MountBtrfsFull() temporary top-level mountpoint=%s",
		topLevelDir,
	)

	topLevelMounted := false

	// 注意：
	// 这里绝对不要调用 Umount()。
	//
	// 因为 Umount() 会判断 Btrfs，并可能进入 UmountBtrfsFull()。
	// 这里的 topLevelDir 是本函数自己创建的临时 mount，
	// 只需要卸载它自己。
	cleanupTopLevel := func() {
		if topLevelMounted {
			if err := umountOnce(topLevelDir, false); err != nil {
				logger.Warnf(
					"MountBtrfsFull() cleanup top-level mount %s failed: %v",
					topLevelDir,
					err,
				)
			}

			topLevelMounted = false
		}

		if err := os.RemoveAll(topLevelDir); err != nil {
			logger.Warnf(
				"MountBtrfsFull() remove temporary mountpoint %s failed: %v",
				topLevelDir,
				err,
			)
		}
	}

	defer cleanupTopLevel()

	// ------------------------------------------------------------------
	// 2. 挂载 top-level subvolume (subvolid=5)。
	// ------------------------------------------------------------------
	if !tryMountBtrfsSubvolume(
		ctx,
		device,
		topLevelDir,
		"subvolid=5,rescue=usebackuproot",
		readonly,
	) {
		return false, fmt.Errorf(
			"mount btrfs top-level failed: device=%s mountpoint=%s",
			device,
			topLevelDir,
		)
	}

	topLevelMounted = true

	logger.Debugf(
		"MountBtrfsFull() top-level mounted: %s -> %s",
		device,
		topLevelDir,
	)

	// ------------------------------------------------------------------
	// 3. 列出所有 subvolume。
	// ------------------------------------------------------------------
	subvolumes, err := listBtrfsSubvolumes(
		ctx,
		topLevelDir,
	)
	if err != nil {
		return false, fmt.Errorf(
			"list subvolumes failed: %v",
			err,
		)
	}

	logger.Debugf(
		"MountBtrfsFull() found %d subvolumes",
		len(subvolumes),
	)

	// ------------------------------------------------------------------
	// 4. 识别系统根 subvolume。
	// ------------------------------------------------------------------
	rootSubvol, rootFound := findRootSubvolume(
		ctx,
		topLevelDir,
		subvolumes,
		readonly,
	)

	// ------------------------------------------------------------------
	// 5. 卸载临时 top-level。
	// ------------------------------------------------------------------
	if err := umountOnce(topLevelDir, false); err != nil {
		return false, fmt.Errorf(
			"unmount temporary btrfs top-level %s failed: %v",
			topLevelDir,
			err,
		)
	}

	topLevelMounted = false

	logger.Debugf(
		"MountBtrfsFull() temporary top-level unmounted: %s",
		topLevelDir,
	)

	// ------------------------------------------------------------------
	// 6. 找到系统根 subvolume。
	// ------------------------------------------------------------------
	if rootFound {
		logger.Infof(
			"MountBtrfsFull() root subvolume: id=%d path=%s",
			rootSubvol.ID,
			rootSubvol.Path,
		)

		opts := fmt.Sprintf(
			"subvolid=%d,rescue=usebackuproot",
			rootSubvol.ID,
		)

		if !tryMountBtrfsSubvolume(
			ctx,
			device,
			mountpoint,
			opts,
			readonly,
		) {
			return false, fmt.Errorf(
				"mount root subvolume failed: device=%s id=%d path=%s mountpoint=%s",
				device,
				rootSubvol.ID,
				rootSubvol.Path,
				mountpoint,
			)
		}

		// ------------------------------------------------------------------
		// 7. 挂载其他 subvolume。
		// ------------------------------------------------------------------
		if err := mountRemainingSubvolumes(
			ctx,
			device,
			mountpoint,
			subvolumes,
			rootSubvol,
			readonly,
		); err != nil {
			logger.Warnf(
				"MountBtrfsFull() mount sub-volumes failed: %v",
				err,
			)

			// 根卷已经成功挂载，因此不认为整体失败。
		}

		return true, nil
	}

	// ------------------------------------------------------------------
	// 8. 没有找到系统根，退化到 top-level。
	// ------------------------------------------------------------------
	logger.Warnf(
		"MountBtrfsFull() no root subvolume found, mounting top-level",
	)

	if !tryMountBtrfsSubvolume(
		ctx,
		device,
		mountpoint,
		"subvolid=5,rescue=usebackuproot",
		readonly,
	) {
		return false, fmt.Errorf(
			"mount btrfs top-level failed: device=%s mountpoint=%s",
			device,
			mountpoint,
		)
	}

	return false, nil
}

// ======================================================================
// 识别系统根 subvolume
// ======================================================================

func findRootSubvolume(
	ctx context.Context,
	topLevelDir string,
	subvolumes []btrfsSubvolume,
	readonly bool,
) (*btrfsSubvolume, bool) {
	if len(subvolumes) == 0 {
		return nil, false
	}

	candidates := prioritizeRootSubvolumes(subvolumes)

	for i := range candidates {
		subvol := &candidates[i]

		select {
		case <-ctx.Done():
			return nil, false
		default:
		}

		// 通过 top-level 路径直接探测内容。
		probePath := filepath.Join(topLevelDir, filepath.Clean(subvol.Path))

		info, err := os.Stat(probePath)
		if err != nil || !info.IsDir() {
			continue
		}

		if xutil.IsLinuxRoot(probePath) {
			return subvol, true
		}
	}

	return nil, false
}

// ======================================================================
// 挂载其余子卷到 mountpoint 下对应子目录
// ======================================================================

func mountRemainingSubvolumes(
	ctx context.Context,
	device string,
	mountpoint string,
	allSubvolumes []btrfsSubvolume,
	rootSubvol *btrfsSubvolume,
	readonly bool,
) error {
	// 过滤掉根卷本身。
	var remaining []btrfsSubvolume
	for _, sv := range allSubvolumes {
		if sv.ID != rootSubvol.ID {
			remaining = append(remaining, sv)
		}
	}

	if len(remaining) == 0 {
		return nil
	}

	// ------------------------------------------------------------------
	// 确定每个子卷的挂载路径。
	// 优先使用 /etc/fstab，回退到启发式推断。
	// ------------------------------------------------------------------
	fstabMounts := parseFstab(mountpoint)

	type mountTask struct {
		subvol     btrfsSubvolume
		targetPath string // 相对于 mountpoint 的路径
	}

	var tasks []mountTask

	for _, sv := range remaining {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		target := resolveMountPath(sv, rootSubvol, fstabMounts)
		if target == "" {
			logger.Debugf(
				"mountRemainingSubvolumes() skip subvolume: id=%d path=%s (no target)",
				sv.ID, sv.Path,
			)
			continue
		}

		tasks = append(tasks, mountTask{subvol: sv, targetPath: target})
	}

	// ------------------------------------------------------------------
	// 按路径深度排序：先挂载浅层，再挂载深层。
	// 例如 /home 必须在 /home/user 之前。
	// ------------------------------------------------------------------
	sort.Slice(tasks, func(i, j int) bool {
		di := strings.Count(tasks[i].targetPath, "/")
		dj := strings.Count(tasks[j].targetPath, "/")
		if di != dj {
			return di < dj
		}
		return tasks[i].targetPath < tasks[j].targetPath
	})

	// ------------------------------------------------------------------
	// 依次挂载。
	// ------------------------------------------------------------------
	for _, task := range tasks {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		fullTarget := filepath.Join(mountpoint, task.targetPath)

		// 创建目标目录。
		if err := os.MkdirAll(fullTarget, 0755); err != nil {
			logger.Warnf(
				"mountRemainingSubvolumes() mkdir %s failed: %v",
				fullTarget, err,
			)
			continue
		}

		opts := fmt.Sprintf("subvolid=%d", task.subvol.ID)
		if !tryMountBtrfsSubvolume(ctx, device, fullTarget, opts+",rescue=usebackuproot", readonly) {
			logger.Warnf(
				"mountRemainingSubvolumes() mount failed: id=%d path=%s target=%s",
				task.subvol.ID, task.subvol.Path, fullTarget,
			)
			continue
		}

		logger.Infof(
			"mountRemainingSubvolumes() mounted: id=%d %s -> %s",
			task.subvol.ID, task.subvol.Path, fullTarget,
		)
	}

	return nil
}

// ======================================================================
// 确定子卷挂载路径
// ======================================================================

// fstabEntry 表示 /etc/fstab 中的一条 Btrfs subvolume 挂载记录。
type fstabEntry struct {
	SubvolPath string // subvol=xxx 或 subvolid=xxx
	MountPoint string // 挂载点（相对于 /）
}

// resolveMountPath 确定一个子卷应该挂载到 mountpoint 下的哪个相对路径。
//
// 策略优先级：
//  1. /etc/fstab 中明确指定的路径
//  2. 启发式：去除根卷路径前缀
func resolveMountPath(
	sv btrfsSubvolume,
	rootSubvol *btrfsSubvolume,
	fstabMounts []fstabEntry,
) string {
	// 策略 1：从 fstab 匹配。
	for _, entry := range fstabMounts {
		// fstab 中可能写 subvol=@home 或 subvolid=257。
		if entry.SubvolPath == sv.Path {
			return strings.TrimPrefix(entry.MountPoint, "/")
		}
		if entry.SubvolPath == fmt.Sprintf("subvolid=%d", sv.ID) {
			return strings.TrimPrefix(entry.MountPoint, "/")
		}
	}

	// 策略 2：启发式前缀去除。
	//
	// 根卷路径为 "@"，子卷路径为 "@home" → 挂载到 "home"
	// 根卷路径为 "@"，子卷路径为 "@/var/log" → 挂载到 "var/log"
	// 根卷路径为 "root"，子卷路径为 "home" → 挂载到 "home"
	//
	rootPath := strings.TrimSuffix(filepath.Clean(rootSubvol.Path), "/")
	subPath := strings.TrimSuffix(filepath.Clean(sv.Path), "/")

	// 情况 A：子卷路径以根卷路径 + "/" 开头（嵌套子卷）。
	//   root="@", sub="@/var" → "var"
	if strings.HasPrefix(subPath, rootPath+"/") {
		return strings.TrimPrefix(subPath, rootPath+"/")
	}

	// 情况 B：子卷路径以根卷路径为前缀（无分隔符，如 @ → @home）。
	//   root="@", sub="@home" → "home"
	if rootPath != "" && rootPath != "." && strings.HasPrefix(subPath, rootPath) {
		remainder := strings.TrimPrefix(subPath, rootPath)
		remainder = strings.TrimPrefix(remainder, "/")
		if remainder != "" {
			return remainder
		}
	}

	// 情况 C：子卷路径与根卷路径无公共前缀（平级子卷）。
	//   root="root", sub="home" → "home"
	//   root="@",    sub="@home" 已在情况 B 处理。
	//
	// 直接使用子卷路径的最后一段或完整路径。
	base := filepath.Base(subPath)
	if base == "" || base == "." || base == "/" {
		return ""
	}

	// 如果路径只有一层（如 "home"），直接用。
	if !strings.Contains(subPath, "/") {
		return subPath
	}

	// 多层路径（如 "data/backup"），保留完整相对路径。
	return subPath
}

// ======================================================================
// 解析 /etc/fstab
// ======================================================================

// parseFstab 从已挂载的根卷中解析 /etc/fstab，
// 提取 Btrfs subvolume 相关的挂载条目。
func parseFstab(rootMountpoint string) []fstabEntry {
	fstabPath := filepath.Join(rootMountpoint, "etc", "fstab")

	data, err := os.ReadFile(fstabPath)
	if err != nil {
		logger.Debugf("parseFstab() cannot read %s: %v", fstabPath, err)
		return nil
	}

	var entries []fstabEntry

	// 匹配 btrfs 类型的行，提取 subvol/subvolid 选项和挂载点。
	// 格式: <device> <mountpoint> btrfs <options> <dump> <pass>
	re := regexp.MustCompile(
		`^(\S+)\s+(\S+)\s+btrfs\s+(\S+)`,
	)

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		m := re.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		mountPoint := m[2]
		options := m[3]

		// 提取 subvol= 或 subvolid= 选项。
		subvolPath := extractOption(options, "subvol")
		if subvolPath == "" {
			subvolID := extractOption(options, "subvolid")
			if subvolID != "" {
				subvolPath = "subvolid=" + subvolID
			}
		}

		if subvolPath == "" {
			continue
		}

		entries = append(entries, fstabEntry{
			SubvolPath: subvolPath,
			MountPoint: mountPoint,
		})
	}

	logger.Debugf("parseFstab() found %d btrfs entries", len(entries))
	return entries
}

// extractOption 从逗号分隔的挂载选项中提取指定 key 的值。
func extractOption(options, key string) string {
	for _, opt := range strings.Split(options, ",") {
		if strings.HasPrefix(opt, key+"=") {
			return strings.TrimPrefix(opt, key+"=")
		}
	}
	return ""
}

// ======================================================================
// 底层辅助函数（沿用之前的实现）
// ======================================================================

type btrfsSubvolume struct {
	ID   uint64
	Path string
}

func listBtrfsSubvolumes(
	ctx context.Context,
	mountpoint string,
) ([]btrfsSubvolume, error) {
	cmd := exec.CommandContext(ctx, "btrfs", "subvolume", "list", "-R", mountpoint)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf(
			"btrfs subvolume list failed: %v, output=%s", err, string(output),
		)
	}
	return parseBtrfsSubvolumeList(string(output))
}

func parseBtrfsSubvolumeList(output string) ([]btrfsSubvolume, error) {
	re := regexp.MustCompile(`^ID\s+(\d+)\s+.*\bpath\s+(.+)$`)
	var result []btrfsSubvolume

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		m := re.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		id, err := strconv.ParseUint(m[1], 10, 64)
		if err != nil {
			continue
		}
		path := strings.TrimSpace(m[2])
		if path == "" {
			continue
		}
		result = append(result, btrfsSubvolume{ID: id, Path: path})
	}
	return result, nil
}

func prioritizeRootSubvolumes(subvolumes []btrfsSubvolume) []btrfsSubvolume {
	var result []btrfsSubvolume
	used := make(map[uint64]bool)

	preferred := []string{"@", "@root", "root"}
	for _, name := range preferred {
		for _, sv := range subvolumes {
			if used[sv.ID] {
				continue
			}
			if filepath.Clean(sv.Path) == name {
				result = append(result, sv)
				used[sv.ID] = true
			}
		}
	}

	for _, sv := range subvolumes {
		if used[sv.ID] {
			continue
		}
		lower := strings.ToLower(filepath.Base(sv.Path))
		if strings.Contains(lower, "root") {
			result = append(result, sv)
			used[sv.ID] = true
		}
	}

	for _, sv := range subvolumes {
		if !used[sv.ID] {
			result = append(result, sv)
			used[sv.ID] = true
		}
	}

	return result
}

func tryMountBtrfsSubvolume(
	ctx context.Context,
	device, mountpoint, option string,
	readonly bool,
) bool {
	options := option

	if readonly {
		if options != "" {
			options += ","
		}
		options += "ro"
	}

	args := []string{
		"mount",
		"-t",
		"btrfs",
	}

	if options != "" {
		args = append(args, "-o", options)
	}

	args = append(args, device, mountpoint)

	logger.Debugf(
		"tryMountBtrfsSubvolume() command=mount device=%s mountpoint=%s options=%s",
		device,
		mountpoint,
		options,
	)

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	output, err := cmd.CombinedOutput()

	if err != nil {
		logger.Debugf(
			"mount failed: device=%s mountpoint=%s options=%s err=%v output=%s",
			device,
			mountpoint,
			options,
			err,
			strings.TrimSpace(string(output)),
		)
		return false
	}

	return true
}

// Umount 取消设备或挂载点的挂载。
//
// 支持：
//   - 挂载点，例如 /mnt/data
//   - 设备，例如 /dev/sda2
//   - Btrfs 子卷/子挂载点
//   - recursive=true 的普通递归卸载
//
// 对 Btrfs：
//  1. 找到实际挂载点
//  2. 获取该挂载点下所有子挂载
//  3. 按路径深度从深到浅依次卸载
//  4. 最后卸载根挂载点
//
// 注意：Btrfs 卸载流程内部只能调用 umountOnce()，
// 不能再次调用 Umount()，否则会产生递归。
func Umount(deviceOrMountpoint string, recursive bool) error {
	logger.Debugf(
		"Umount() target=%s recursive=%v",
		deviceOrMountpoint,
		recursive,
	)

	// 1. 判断传入参数是挂载点还是设备。
	mountpoint, mounted, err := findMountpoint(deviceOrMountpoint)
	if err != nil {
		return fmt.Errorf(
			"find mountpoint for %s failed: %v",
			deviceOrMountpoint,
			err,
		)
	}

	// 没有找到对应挂载。
	if !mounted {
		// 兼容原有行为：
		// 直接执行一次 umount，让系统自己判断是否已经卸载。
		return umountOnce(deviceOrMountpoint, recursive)
	}

	logger.Debugf(
		"Umount() target=%s mountpoint=%s",
		deviceOrMountpoint,
		mountpoint,
	)

	// 2. 判断实际挂载点是不是 Btrfs。
	isBtrfs, err := IsBtrfsMountpoint(mountpoint)
	if err != nil {
		return fmt.Errorf(
			"check btrfs mountpoint %s failed: %v",
			mountpoint,
			err,
		)
	}

	if isBtrfs {
		logger.Warnf(
			"Umount() mountpoint=%s is btrfs, use full btrfs unmount",
			mountpoint,
		)

		return UmountBtrfsFull(mountpoint)
	}

	// 3. 普通文件系统。
	return umountOnce(mountpoint, recursive)
}

// ======================================================================
// Btrfs 卸载
// ======================================================================

// UmountBtrfsFull 逆序卸载 mountpoint 下所有子挂载，
// 最后卸载 mountpoint 本身。
//
// 例如：
//
//	/mnt/btrfs
//	/mnt/btrfs/subvol1
//	/mnt/btrfs/subvol1/data
//	/mnt/btrfs/subvol2
//
// 卸载顺序：
//
//	/mnt/btrfs/subvol1/data
//	/mnt/btrfs/subvol1
//	/mnt/btrfs/subvol2
//	/mnt/btrfs
//
// 注意：这里不能调用 Umount()，否则 Btrfs 挂载点会再次进入
// UmountBtrfsFull()，造成无限递归。
func UmountBtrfsFull(mountpoint string) error {
	mountpoint = filepath.Clean(mountpoint)

	logger.Debugf(
		"UmountBtrfsFull() mountpoint=%s",
		mountpoint,
	)

	// 获取 mountpoint 下所有子挂载点。
	mounts, err := listMountsUnder(mountpoint)
	if err != nil {
		return fmt.Errorf(
			"list mounts under %s failed: %v",
			mountpoint,
			err,
		)
	}

	// 从最深层开始卸载。
	for _, childMount := range mounts {
		logger.Debugf(
			"UmountBtrfsFull() unmount child=%s",
			childMount,
		)

		if err := umountOnce(childMount, false); err != nil {
			return fmt.Errorf(
				"unmount child mount %s failed: %v",
				childMount,
				err,
			)
		}
	}

	// 最后卸载根挂载点。
	logger.Debugf(
		"UmountBtrfsFull() unmount root=%s",
		mountpoint,
	)

	if err := umountOnce(mountpoint, false); err != nil {
		return fmt.Errorf(
			"unmount root mountpoint %s failed: %v",
			mountpoint,
			err,
		)
	}

	return nil
}

// ======================================================================
// 底层 umount
// ======================================================================

// umountOnce 执行一次实际的 umount 操作。
//
// 该函数只负责执行 umount，绝对不能包含 Btrfs 判断，
// 也不能调用 Umount()，否则容易形成递归。
func umountOnce(deviceOrMountpoint string, recursive bool) error {
	logger.Debugf(
		"umountOnce() target=%s recursive=%v",
		deviceOrMountpoint,
		recursive,
	)

	cmd := fmt.Sprintf(
		"umount %s",
		shellQuote(deviceOrMountpoint),
	)

	if recursive {
		cmd = fmt.Sprintf(
			"umount -R %s",
			shellQuote(deviceOrMountpoint),
		)
	}

	_, output, err := command.Execute(cmd)

	if err == nil {
		return nil
	}

	// 已经没有挂载，视为成功。
	if strings.Contains(output, "not mounted") {
		return nil
	}

	// target is busy，等待后重试一次。
	if strings.Contains(strings.ToLower(output), "busy") {
		logger.Warnf(
			"umountOnce() failed: busy=%s output=%s, retry after 3s...",
			deviceOrMountpoint,
			output,
		)

		time.Sleep(3 * time.Second)

		_, retryOutput, retryErr := command.Execute(cmd)

		if retryErr == nil {
			return nil
		}

		if strings.Contains(
			strings.ToLower(retryOutput),
			"not mounted",
		) {
			return nil
		}

		return fmt.Errorf(
			"umount %s failed after retry: %v, output=%s",
			deviceOrMountpoint,
			retryErr,
			retryOutput,
		)
	}

	return fmt.Errorf(
		"umount %s failed: %v, output=%s",
		deviceOrMountpoint,
		err,
		output,
	)
}

// ======================================================================
// Btrfs mountpoint 判断
// ======================================================================

// IsBtrfsMountpoint 判断指定路径是否是一个 Btrfs 挂载点。
//
// 注意：
//   - 只判断该路径本身是不是挂载点。
//   - 不判断该路径是否位于某个 Btrfs 文件系统内部。
//
// 例如：
//
//	/mnt/btrfs              -> true
//	/mnt/btrfs/subvolume    -> 如果没有单独 mount，则 false
//	/mnt/btrfs/subvolume2   -> 如果是独立 Btrfs mount，则 true
func IsBtrfsMountpoint(mountpoint string) (bool, error) {
	mountpoint = filepath.Clean(mountpoint)

	mounts, err := readMounts()
	if err != nil {
		return false, err
	}

	for _, m := range mounts {
		if m.Mountpoint == mountpoint {
			return m.FSType == "btrfs", nil
		}
	}

	return false, nil
}

// ======================================================================
// 查找设备对应的挂载点
// ======================================================================

// findMountpoint 判断 target 是挂载点还是设备，并返回对应的挂载点。
//
// 例如：
//
//	/dev/sda2     -> /mnt/data
//	/mnt/data     -> /mnt/data
//	/dev/mapper/x -> /mnt/data
//
// 返回：
//
//	mountpoint, mounted, error
func findMountpoint(target string) (string, bool, error) {
	target = filepath.Clean(target)

	mounts, err := readMounts()
	if err != nil {
		return "", false, err
	}

	// 先判断 target 本身是不是挂载点。
	for _, m := range mounts {
		if m.Mountpoint == target {
			return m.Mountpoint, true, nil
		}
	}

	// 再判断 target 是不是设备。
	for _, m := range mounts {
		if m.Source == target {
			return m.Mountpoint, true, nil
		}
	}

	return "", false, nil
}

// ======================================================================
// 获取指定挂载点下面的所有挂载点
// ======================================================================

// listMountsUnder 返回 mountpoint 下所有子挂载点。
//
// 注意：
//   - 不包含 mountpoint 自身。
//   - 只返回真正的子挂载点。
//   - 按路径深度从深到浅排序。
//
// 例如：
//
//	mountpoint = /mnt/data
//
// 返回：
//
//	/mnt/data/a/b
//	/mnt/data/a
//	/mnt/data/b
func listMountsUnder(mountpoint string) ([]string, error) {
	mountpoint = filepath.Clean(mountpoint)

	mounts, err := readMounts()
	if err != nil {
		return nil, err
	}

	prefix := mountpoint + "/"

	result := make([]string, 0)

	for _, m := range mounts {
		mntPath := m.Mountpoint

		// 不包含自身。
		if mntPath == mountpoint {
			continue
		}

		// 只匹配真正的子路径。
		if strings.HasPrefix(mntPath, prefix) {
			result = append(result, mntPath)
		}
	}

	// 路径越深越先卸载。
	sort.Slice(result, func(i, j int) bool {
		depthI := mountPathDepth(result[i])
		depthJ := mountPathDepth(result[j])

		if depthI != depthJ {
			return depthI > depthJ
		}

		// 深度相同的时候，保证顺序稳定。
		return result[i] > result[j]
	})

	return result, nil
}

// ======================================================================
// /proc/mounts 解析
// ======================================================================

type mountInfo struct {
	Source     string
	Mountpoint string
	FSType     string
}

// readMounts 读取并解析 /proc/mounts。
//
// /proc/mounts 中的路径可能使用以下转义：
//
//	\040 -> 空格
//	\011 -> Tab
//	\012 -> 换行
//	\134 -> 反斜杠
func readMounts() ([]mountInfo, error) {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return nil, fmt.Errorf(
			"read /proc/mounts failed: %v",
			err,
		)
	}

	result := make([]mountInfo, 0)

	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)

		if len(fields) < 3 {
			continue
		}

		source := decodeMountPath(fields[0])
		mountpoint := filepath.Clean(
			decodeMountPath(fields[1]),
		)
		fsType := fields[2]

		result = append(result, mountInfo{
			Source:     source,
			Mountpoint: mountpoint,
			FSType:     fsType,
		})
	}

	return result, nil
}

// decodeMountPath 解码 /proc/mounts 中的路径转义。
func decodeMountPath(path string) string {
	replacer := strings.NewReplacer(
		`\040`, " ",
		`\011`, "\t",
		`\012`, "\n",
		`\134`, `\`,
	)

	return replacer.Replace(path)
}

// mountPathDepth 返回挂载点路径深度。
func mountPathDepth(path string) int {
	path = filepath.Clean(path)

	if path == "/" {
		return 0
	}

	return strings.Count(path, "/")
}

// shellQuote 对 shell 参数进行单引号转义。
//
// command.Execute 当前如果本身支持参数列表，
// 最好直接改成 command.Execute("umount", args...)，
// 这里是为了兼容你当前传入完整 shell command 的方式。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
