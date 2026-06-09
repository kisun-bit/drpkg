package x2xcore

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/kisun-bit/drpkg/command"
	"github.com/kisun-bit/drpkg/define"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/pkg/errors"
)

type efiImageName struct {
	Default string
	Grub    string
	Shim    string
}

func getEfiImageName() (efiImageName, bool) {
	tag := ""

	switch runtime.GOARCH {
	case define.ArchAmd64:
		tag = "x64"
	case define.ArchArm64:
		tag = "aa64"
	case define.Arch386:
		tag = "ia32"
	case define.ArchLoong64:
		tag = "loongarch64"
	case define.ArchRiscv64:
		tag = "riscv64"
	}

	if tag == "" {
		return efiImageName{}, false
	}

	return efiImageName{
			fmt.Sprintf("boot%s.efi", tag),
			fmt.Sprintf("grub%s.efi", tag),
			fmt.Sprintf("shim%s.efi", tag)},
		true
}

// Mount 挂载设备到指定挂载点
func Mount(ctx context.Context, device string, mountpoint string, readonly bool) (supported bool, err error) {
	logger.Debugf("Mount() device=%s mountpoint=%s readonly=%v", device, mountpoint, readonly)

	// 确保 mountpoint 存在
	if err = os.MkdirAll(mountpoint, 0755); err != nil {
		return false, fmt.Errorf("create mountpoint failed: %v", err)
	}

	// 检测设备是否支持挂载
	yes, fsType := SupportMount(device)
	if !yes {
		return false, nil
	}

	// 检查是否已经挂载（幂等）
	if isMounted(mountpoint) {
		logger.Debugf("Mount() already mounted: %s", mountpoint)
		return true, nil
	}

	// 尝试基础 mount
	if ok := tryMount(ctx, device, mountpoint, "", readonly); ok {
		return true, nil
	}

	// 带 fs-type 再试一次
	if ok := tryMount(ctx, device, mountpoint, fsType, readonly); ok {
		return true, nil
	}

	// 尝试修复文件系统
	repairCmd, _ := DetectFSRepairCmdline(device)
	if repairCmd != "" {
		logger.Warnf("Mount() running fs repair: %s", repairCmd)

		_, out, err := command.ExecuteWithContext(ctx, repairCmd)
		if err != nil {
			logger.Warnf("Mount() fs repair failed: %v output=%s", err, out)
		}
	}

	// repair 后再次 mount（优先 fsType）
	if ok := tryMount(ctx, device, mountpoint, fsType, readonly); ok {
		return true, nil
	}

	// 最后 fallback mount
	if ok := tryMount(ctx, device, mountpoint, "", readonly); ok {
		return true, nil
	}

	return false, errors.Errorf("mount failed: device=%s mountpoint=%s", device, mountpoint)
}

// Mount 取消设备的挂载
func Umount(deviceOrMountpoint string, recursive bool) error {
	logger.Debugf("Umount() target=%s", deviceOrMountpoint)

	// 1. 普通卸载
	cmd := fmt.Sprintf("umount %s", deviceOrMountpoint)
	if recursive {
		cmd = fmt.Sprintf("umount -R %s", deviceOrMountpoint)
	}
	_, output, err := command.Execute(cmd)
	if err != nil {
		if strings.Contains(output, "not mounted") {
			return nil
		}
		if strings.Contains(output, "busy") {
			logger.Warnf("Umount() failed: busy=%s output=%s. retry after 3s...", deviceOrMountpoint, output)
			// 避免挂载后立即卸载报错target is busy
			time.Sleep(3 * time.Second)
			_, _, e := command.Execute(cmd)
			if e == nil {
				return nil
			}
		}
		return errors.Wrapf(err, "umount %s", deviceOrMountpoint)
	}

	return nil
}

func DeactivateVgs() error {
	logger.Debugf("DeactivateVgs() ++")
	defer logger.Debugf("DeactivateVgs() --")

	_, output, err := command.Execute("vgchange -an", command.WithDebug())
	if err == nil {
		return nil
	}

	return errors.Wrapf(err, "deactivateVg failed: %s", output)
}

func ActivateVgs() error {
	logger.Debugf("ActivateVgs() ++")
	defer logger.Debugf("ActivateVgs() --")

	e := os.RemoveAll("/etc/lvm/devices/system.devices")
	logger.Debugf("ActivateVgs() Remove system.devices: %v", e)

	rescanLvmCmd := "pvscan; vgscan; vgchange -ay"
	_, output, err := command.Execute(rescanLvmCmd, command.WithDebug())
	if err == nil {
		return nil
	}

	return errors.Wrapf(err, "scan lvm failed: %s", output)
}

func Kconfig(kCfgPath string) (configs map[string]string, err error) {
	f, err := os.Open(kCfgPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	configs = make(map[string]string)

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		items := strings.Split(line, "=")
		if len(items) != 2 {
			continue
		}

		key := strings.ToUpper(items[0])
		value := strings.Trim(items[1], `"'`)
		if value == "Y" || value == "M" || value == "N" {
			value = strings.ToLower(value)
		}
		configs[key] = value
	}

	return configs, scanner.Err()
}

func VmbusExisted() (bool, error) {
	items, err := os.ReadDir("/sys/bus/vmbus/devices")
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	return len(items) > 0, nil
}

func DetectGrub(root string) (int, string) {
	globs := []string{
		"boot/*/grub.cfg",
		"boot/*/grub.conf",
		"boot/*/menu.lst",
		"boot/burg/burg.cfg",
		"boot/efi/EFI/*/grub.cfg",
		"boot/efi/EFI/*/grub.conf",
		"boot/efi/EFI/*/elilo.conf",
		"etc/grub2.cfg",
		"etc/grub2-efi.cfg",
		"etc/grub.conf",
		"etc/grub.cfg",
		"etc/elilo.conf",
	}

	type result struct {
		ver  int
		path string
	}
	var found []result

	for _, g := range globs {
		paths, _ := filepath.Glob(filepath.Join(root, g))

		for _, p := range paths {
			absPath := resolve(p)
			content, err := readFileHead(absPath, 512<<10)
			if err != nil {
				continue
			}

			// EFI stub 跟踪
			if strings.Contains(content, "configfile ") {
				if next := parseConfigfile(content, root); next != "" {
					if v, rp := detectSingle(next); v != -1 {
						return v, rp
					}
				}
			}

			if v := detectContent(content); v != -1 {
				found = append(found, result{v, absPath})
			}
		}
	}

	// 优先 grub2
	for _, r := range found {
		if r.ver == 2 {
			return r.ver, r.path
		}
	}
	for _, r := range found {
		if r.ver == 1 {
			return r.ver, r.path
		}
	}

	return -1, ""
}

func resolve(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func detectContent(content string) int {
	if strings.Contains(content, "menuentry ") {
		return 2
	}
	if strings.Contains(content, "title ") {
		return 1
	}
	return -1
}

func detectSingle(path string) (int, string) {
	absPath := resolve(path)

	content, err := readFileHead(absPath, 512<<10)
	if err != nil {
		return -1, ""
	}

	return detectContent(content), absPath
}

func readFileHead(path string, n int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	buf := make([]byte, n)
	nr, err := f.Read(buf)
	if err != nil && nr == 0 {
		return "", err
	}

	return string(buf[:nr]), nil
}

// parseConfigfile 从 grub.cfg 内容中解析 configfile 指向的真实路径
func parseConfigfile(content, root string) string {
	var prefix string

	lines := strings.Split(content, "\n")

	// 先解析 prefix（如果有）
	// 例如：set prefix=($root)/grub2
	rePrefix := regexp.MustCompile(`set\s+prefix\s*=\s*\([^)]+\)(.*)`)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "set prefix") {
			if m := rePrefix.FindStringSubmatch(line); len(m) == 2 {
				prefix = strings.TrimSpace(m[1])
			}
		}
	}

	// 解析 configfile
	reConfig := regexp.MustCompile(`configfile\s+(.+)`)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "configfile") {
			continue
		}

		m := reConfig.FindStringSubmatch(line)
		if len(m) != 2 {
			continue
		}

		path := strings.TrimSpace(m[1])

		// 去掉变量
		path = strings.ReplaceAll(path, "$prefix", prefix)

		// 去掉可能的变量残留（简单处理）
		if strings.Contains(path, "$") {
			continue
		}

		// 绝对路径
		if strings.HasPrefix(path, "/") {
			return filepath.Join(root, path)
		}

		// 相对路径（基于 prefix）
		if prefix != "" {
			return filepath.Join(root, prefix, path)
		}
	}

	return ""
}

func IsMountPointByMountInfo(path string) (bool, error) {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return false, err
	}

	path = filepath.Clean(path)

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}

		mountPoint := fields[4]

		if mountPoint == path {
			return true, nil
		}
	}

	return false, nil
}

func tryMount(ctx context.Context, device, mountpoint, fsType string, readonly bool) bool {
	var cmd string

	if fsType != "" {
		cmd = fmt.Sprintf("mount -t %s %s %s", fsType, device, mountpoint)
	} else {
		cmd = fmt.Sprintf("mount %s %s", device, mountpoint)
	}

	if readonly {
		if fsType != "" {
			cmd = fmt.Sprintf("mount -o ro -t %s %s %s", fsType, device, mountpoint)
		} else {
			cmd = fmt.Sprintf("mount -o ro %s %s", device, mountpoint)
		}
	}

	_, out, err := command.ExecuteWithContext(ctx, cmd)
	if err != nil {
		logger.Warnf("Mount failed cmd=%s err=%v out=%s", cmd, err, out)
		return false
	}

	logger.Infof("Mount success: %s -> %s (fs=%s)", device, mountpoint, fsType)
	return true
}

func isMounted(mountpoint string) bool {
	cmd := fmt.Sprintf("mount | grep ' %s '", mountpoint)
	_, out, err := command.ExecuteWithContext(context.Background(), cmd)
	return err == nil && strings.Contains(out, mountpoint)
}

func isValidFstabDevice(device string) bool {
	if strings.HasPrefix(device, "/dev/disk/by-partuuid") ||
		strings.HasPrefix(device, "/dev/disk/by-uuid") ||
		strings.HasPrefix(device, "/dev/disk/by-label") ||
		strings.HasPrefix(strings.ToUpper(device), "PARTUUID=") ||
		strings.HasPrefix(strings.ToUpper(device), "UUID=") ||
		strings.HasPrefix(strings.ToUpper(device), "LABEL=") ||
		strings.HasPrefix(device, "/dev/mapper") ||
		strings.HasPrefix(device, "/skole") {
		return true
	}
	if strings.HasPrefix(device, "/dev/") {
		r, _, e := command.Execute("lvdisplay " + device)
		if e == nil || r == 0 {
			return true
		}
	}
	return false
}

func DetectSystemd(root string) bool {

	// 1. systemd 主程序
	if extend.IsExisted(filepath.Join(root,
		"usr/lib/systemd/systemd")) {
		return true
	}

	if extend.IsExisted(filepath.Join(root,
		"lib/systemd/systemd")) {
		return true
	}

	// 2. systemctl
	if extend.IsExisted(filepath.Join(root,
		"usr/bin/systemctl")) {
		return true
	}

	// 3. systemd 配置目录
	if extend.IsExisted(filepath.Join(root,
		"etc/systemd")) {
		return true
	}

	return false
}
