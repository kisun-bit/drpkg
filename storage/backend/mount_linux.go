//go:build linux

package backend

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kisun-bit/drpkg/command"
	"github.com/kisun-bit/drpkg/logger"
)

// cifsOnline 在 Linux 上挂载 CIFS 共享。
func cifsOnline(a *cifsAccessor) error {
	if a.cfg.Server == "" {
		a.status = StatusError
		return fmt.Errorf("backend: cifs requires Server")
	}
	key := strings.Join([]string{"cifs", a.cfg.Server, a.cfg.Share, strconv.Itoa(a.cfg.Port)}, "|")
	mountPoint := linuxMountPoint(key)
	return linuxOnlineNet(&a.netFs, mountPoint, a.cfg.SubPath, func(mp string) error {
		return linuxMountCIFS(a.cfg, mp)
	})
}

// cifsOffline 在 Linux 上卸载由本访问器挂载的 CIFS 共享。
func cifsOffline(a *cifsAccessor) error {
	return linuxOfflineNet(&a.netFs)
}

// nfsOnline 在 Linux 上挂载 NFS 导出。
func nfsOnline(a *nfsAccessor) error {
	if a.cfg.Server == "" || a.cfg.ExportPath == "" {
		a.status = StatusError
		return fmt.Errorf("backend: nfs requires Server and ExportPath")
	}
	key := strings.Join([]string{"nfs", a.cfg.Server, a.cfg.ExportPath, strconv.Itoa(a.cfg.Port)}, "|")
	mountPoint := linuxMountPoint(key)
	return linuxOnlineNet(&a.netFs, mountPoint, a.cfg.SubPath, func(mp string) error {
		return linuxMountNFS(a.cfg, mp)
	})
}

// nfsOffline 在 Linux 上卸载由本访问器挂载的 NFS 导出。
func nfsOffline(a *nfsAccessor) error {
	return linuxOfflineNet(&a.netFs)
}

// linuxOnlineNet 是 Linux 下 cifs/nfs 共享的挂载生命周期骨架。
func linuxOnlineNet(n *netFs, mountPoint, subPath string, mountFn func(string) error) error {
	if err := os.MkdirAll(mountPoint, 0o755); err != nil {
		n.status = StatusError
		return fmt.Errorf("backend: create mount point %s: %w", mountPoint, err)
	}

	if linuxShareMounted(mountPoint) {
		// 已由其他组件挂载，复用，Offline 时不做卸载。
		n.mounted = false
		logger.Infof("backend: reuse existing mount %s", mountPoint)
	} else {
		if err := mountFn(mountPoint); err != nil {
			n.status = StatusError
			_ = os.Remove(mountPoint)
			return err
		}
		n.mounted = true
	}

	root := filepath.Join(mountPoint, subPath)
	if err := os.MkdirAll(root, 0o755); err != nil {
		if n.mounted {
			_ = linuxUnmount(mountPoint)
		}
		_ = os.Remove(mountPoint)
		n.status = StatusError
		return fmt.Errorf("backend: create storage dir %s: %w", root, err)
	}

	n.mountDir = mountPoint
	n.root = root
	n.status = StatusOnline
	return nil
}

// linuxOfflineNet 是 Linux 下 cifs/nfs 共享的卸载逻辑。
func linuxOfflineNet(n *netFs) error {
	if n.mountDir != "" && n.mounted {
		if err := linuxUnmount(n.mountDir); err != nil {
			n.status = StatusError
			return err
		}
		_ = os.Remove(n.mountDir)
	}

	n.mounted = false
	n.mountDir = ""
	n.root = ""
	n.status = StatusOffline
	return nil
}

// linuxMountPoint 根据介质标识生成稳定的挂载点路径，使同一介质在多次 Online
// 之间复用同一挂载点。
func linuxMountPoint(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(os.TempDir(), "drpkg-storage", hex.EncodeToString(sum[:16]))
}

// linuxShareMounted 判断挂载点是否已挂载。
func linuxShareMounted(mountPoint string) bool {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 5 && fields[4] == mountPoint {
			return true
		}
	}
	return false
}

// linuxMountCIFS 执行 CIFS 挂载。
func linuxMountCIFS(c CIFSConfig, mountPoint string) error {
	src := "//" + c.Server
	if c.Share != "" {
		src += "/" + strings.TrimPrefix(c.Share, "/")
	}

	opts := make([]string, 0, 5)
	if c.Username != "" {
		opts = append(opts, "username="+c.Username)
	}
	if c.Password != "" {
		opts = append(opts, "password="+c.Password)
	}
	if c.Domain != "" {
		opts = append(opts, "domain="+c.Domain)
	}
	if c.Port != 0 && c.Port != 445 {
		opts = append(opts, "port="+strconv.Itoa(c.Port))
	}
	if c.SMBVersion != "" {
		opts = append(opts, "vers="+c.SMBVersion)
	}

	args := []string{"-t", "cifs", src, mountPoint}
	if len(opts) > 0 {
		args = append(args, "-o", strings.Join(opts, ","))
	}

	_, out, err := command.ExecuteArgs("mount", args)
	if err != nil {
		return fmt.Errorf("backend: mount cifs %s -> %s: %w (output: %s)",
			src, mountPoint, err, strings.TrimSpace(out))
	}
	logger.Infof("backend: mounted cifs %s -> %s", src, mountPoint)
	return nil
}

// linuxMountNFS 执行 NFS 挂载。
func linuxMountNFS(c NFSConfig, mountPoint string) error {
	src := c.Server + ":" + c.ExportPath

	opts := make([]string, 0, 3)
	if c.Version != "" {
		opts = append(opts, "nfsvers="+c.Version)
	}
	if c.Port != 0 && c.Port != 2049 {
		opts = append(opts, "port="+strconv.Itoa(c.Port))
	}
	if c.MountOptions != "" {
		opts = append(opts, c.MountOptions)
	}

	args := []string{"-t", "nfs", src, mountPoint}
	if len(opts) > 0 {
		args = append(args, "-o", strings.Join(opts, ","))
	}

	_, out, err := command.ExecuteArgs("mount", args)
	if err != nil {
		return fmt.Errorf("backend: mount nfs %s -> %s: %w (output: %s)",
			src, mountPoint, err, strings.TrimSpace(out))
	}
	logger.Infof("backend: mounted nfs %s -> %s", src, mountPoint)
	return nil
}

// linuxUnmount 执行卸载。
func linuxUnmount(mountPoint string) error {
	_, out, err := command.ExecuteArgs("umount", []string{mountPoint})
	if err != nil {
		return fmt.Errorf("backend: umount %s: %w (output: %s)",
			mountPoint, err, strings.TrimSpace(out))
	}
	logger.Infof("backend: unmounted %s", mountPoint)
	return nil
}
