//go:build windows

package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kisun-bit/drpkg/command"
	"github.com/kisun-bit/drpkg/logger"
	"golang.org/x/sys/windows"
)

// cifsOnline 在 Windows 上通过 `net use` 建立 CIFS 连接，根目录为 UNC 路径。
func cifsOnline(a *cifsAccessor) error {
	if a.cfg.Server == "" {
		a.status = StatusError
		return fmt.Errorf("backend: cifs requires Server")
	}

	share := strings.Trim(strings.TrimPrefix(a.cfg.Share, `\`), `\`)
	unc := strings.TrimSuffix(`\\`+a.cfg.Server+`\`+share, `\`)

	if wnetShareConnected(unc) {
		// 已由其他组件建立连接，复用，Offline 时不做删除。
		a.mounted = false
		logger.Infof("backend: reuse existing cifs connection %s", unc)
	} else {
		if err := windowsNetUse(a.cfg, unc); err != nil {
			a.status = StatusError
			return err
		}
		a.mounted = true
	}

	root := filepath.Join(unc, filepath.FromSlash(a.cfg.SubPath))
	if err := os.MkdirAll(root, 0o755); err != nil {
		if a.mounted {
			_ = windowsNetUseDelete(unc)
		}
		a.status = StatusError
		return fmt.Errorf("backend: create storage dir %s: %w", root, err)
	}

	a.mountDir = unc
	a.root = root
	a.status = StatusOnline
	return nil
}

// cifsOffline 在 Windows 上断开由本访问器建立的 CIFS 连接。
func cifsOffline(a *cifsAccessor) error {
	return windowsOfflineNet(&a.netFs, windowsNetUseDelete)
}

// nfsOnline 在 Windows 上通过 NFS 客户端 mount.exe 将导出映射到空闲盘符。
func nfsOnline(a *nfsAccessor) error {
	if a.cfg.Server == "" || a.cfg.ExportPath == "" {
		a.status = StatusError
		return fmt.Errorf("backend: nfs requires Server and ExportPath")
	}

	src := `\\` + a.cfg.Server + `\` + strings.ReplaceAll(a.cfg.ExportPath, "/", `\`)

	drive, err := freeDriveLetter()
	if err != nil {
		a.status = StatusError
		return err
	}

	args := []string{src, drive + `:`}
	if a.cfg.MountOptions != "" {
		args = append([]string{"-o", a.cfg.MountOptions}, args...)
	}
	_, out, err := command.ExecuteArgs("mount.exe", args)
	if err != nil {
		a.status = StatusError
		return fmt.Errorf("backend: mount nfs %s -> %s: %w (output: %s)",
			src, drive, err, strings.TrimSpace(out))
	}
	a.mounted = true

	root := filepath.Join(drive+`:\`, filepath.FromSlash(a.cfg.SubPath))
	if err := os.MkdirAll(root, 0o755); err != nil {
		_ = windowsUmountNFS(drive)
		a.status = StatusError
		return fmt.Errorf("backend: create storage dir %s: %w", root, err)
	}

	a.mountDir = drive + `:\`
	a.root = root
	a.status = StatusOnline
	logger.Infof("backend: mounted nfs %s -> %s", src, drive)
	return nil
}

// nfsOffline 在 Windows 上卸载由本访问器映射的 NFS 盘符。
func nfsOffline(a *nfsAccessor) error {
	return windowsOfflineNet(&a.netFs, func(mountDir string) error {
		return windowsUmountNFS(strings.TrimSuffix(mountDir, `\`))
	})
}

// windowsOfflineNet 是 Windows 下 cifs/nfs 共享的解除连接/映射逻辑。
func windowsOfflineNet(n *netFs, unlink func(string) error) error {
	if n.mounted && n.mountDir != "" {
		if err := unlink(n.mountDir); err != nil {
			n.status = StatusError
			return err
		}
	}

	n.mounted = false
	n.mountDir = ""
	n.root = ""
	n.status = StatusOffline
	return nil
}

// windowsNetUse 通过 `net use` 建立 CIFS 网络连接。
func windowsNetUse(c CIFSConfig, unc string) error {
	args := []string{"use", unc}

	user := c.Username
	if c.Domain != "" && user != "" {
		user = c.Domain + `\` + user
	}
	if user != "" {
		args = append(args, "/user:"+user)
	}
	if c.Password != "" {
		args = append(args, c.Password)
	}

	_, out, err := command.ExecuteArgs("net", args)
	if err != nil {
		return fmt.Errorf("backend: net use %s: %w (output: %s)", unc, err, strings.TrimSpace(out))
	}
	logger.Infof("backend: connected cifs %s", unc)
	return nil
}

// windowsNetUseDelete 断开 CIFS 网络连接。
func windowsNetUseDelete(unc string) error {
	_, out, err := command.ExecuteArgs("net", []string{"use", unc, "/delete", "/y"})
	if err != nil {
		return fmt.Errorf("backend: net use delete %s: %w (output: %s)", unc, err, strings.TrimSpace(out))
	}
	logger.Infof("backend: disconnected cifs %s", unc)
	return nil
}

// wnetShareConnected 判断目标 UNC 是否已建立网络连接。
func wnetShareConnected(unc string) bool {
	_, out, err := command.ExecuteArgs("net", []string{"use"})
	if err != nil {
		return false
	}
	norm := strings.ToLower(strings.TrimSuffix(unc, `\`))
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(strings.ToLower(strings.TrimSpace(line)), norm) {
			return true
		}
	}
	return false
}

// windowsUmountNFS 卸载 NFS 盘符映射。
func windowsUmountNFS(drive string) error {
	_, out, err := command.ExecuteArgs("umount.exe", []string{drive + `:`})
	if err != nil {
		return fmt.Errorf("backend: umount %s: %w (output: %s)", drive, err, strings.TrimSpace(out))
	}
	logger.Infof("backend: unmounted %s", drive)
	return nil
}

// freeDriveLetter 从高位向低位返回第一个空闲盘符（不含冒号）。
func freeDriveLetter() (string, error) {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return "", fmt.Errorf("backend: query logical drives: %w", err)
	}
	for d := byte('Z'); d >= 'A'; d-- {
		if mask&(1<<uint(d-'A')) == 0 {
			return string(d), nil
		}
	}
	return "", fmt.Errorf("backend: no free drive letter for nfs mount")
}
