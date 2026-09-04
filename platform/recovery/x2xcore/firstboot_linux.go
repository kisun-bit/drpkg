package x2xcore

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	scriptPath = "/usr/local/bin/drfbtk.sh"
)

func InstallFirstBoot(rootfs string, scriptContent string) error {
	if hasSystemd(rootfs) {
		return installSystemd(rootfs, scriptContent)
	}

	return installRcLocal(rootfs, scriptContent)
}

func hasSystemd(rootfs string) bool {
	paths := []string{
		"usr/lib/systemd/systemd",
		"lib/systemd/systemd",
	}

	for _, p := range paths {
		if _, err := os.Stat(filepath.Join(rootfs, p)); err == nil {
			return true
		}
	}

	return false
}

func installSystemd(rootfs, scriptContent string) error {
	scriptFile := filepath.Join(rootfs, strings.TrimPrefix(scriptPath, "/"))

	if err := os.MkdirAll(filepath.Dir(scriptFile), 0755); err != nil {
		return err
	}

	if err := os.WriteFile(scriptFile, []byte(scriptContent), 0755); err != nil {
		return err
	}

	service := `[Unit]
Description=First Boot Task
ConditionPathExists=/var/lib/drfbtk.flag
After=network.target

[Service]
Type=oneshot
ExecStart=/usr/local/bin/drfbtk.sh

[Install]
WantedBy=multi-user.target
`

	serviceFile := filepath.Join(
		rootfs,
		"etc/systemd/system/drfbtk.service",
	)

	if err := os.WriteFile(serviceFile, []byte(service), 0644); err != nil {
		return err
	}

	flagFile := filepath.Join(rootfs, "var/lib/drfbtk.flag")

	if err := os.MkdirAll(filepath.Dir(flagFile), 0755); err != nil {
		return err
	}

	if err := os.WriteFile(flagFile, []byte{}, 0644); err != nil {
		return err
	}

	wantsDir := filepath.Join(
		rootfs,
		"etc/systemd/system/multi-user.target.wants",
	)

	if err := os.MkdirAll(wantsDir, 0755); err != nil {
		return err
	}

	link := filepath.Join(wantsDir, "drfbtk.service")

	_ = os.Remove(link)

	if err := os.Symlink(
		"../drfbtk.service",
		link,
	); err != nil {
		return err
	}

	return nil
}

func installRcLocal(rootfs, scriptContent string) error {
	scriptFile := filepath.Join(rootfs, strings.TrimPrefix(scriptPath, "/"))

	if err := os.MkdirAll(filepath.Dir(scriptFile), 0755); err != nil {
		return err
	}

	if err := os.WriteFile(scriptFile, []byte(scriptContent), 0755); err != nil {
		return err
	}

	rcLocal := filepath.Join(rootfs, "etc/rc.local")

	content := "#!/bin/sh\n"

	if data, err := os.ReadFile(rcLocal); err == nil {
		content = string(data)

		if !strings.Contains(content, scriptPath) {
			content += "\n" + scriptPath + "\n"
		}
	} else {
		content += "\n" + scriptPath + "\n"
	}

	if err := os.WriteFile(rcLocal, []byte(content), 0755); err != nil {
		return err
	}

	return nil
}
