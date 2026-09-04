//go:build linux

package qemublk

import (
	"path/filepath"

	"github.com/kisun-bit/drpkg/command"
	"github.com/kisun-bit/drpkg/xutil"
	"github.com/pkg/errors"
)

var (
	ioToolName  = "imgio"
	imgToolName = "qemu-img"

	ioToolPath  = ioToolName
	imgToolPath = imgToolName
)

func init() {
	fixQemuToolName()
}

// QemuToolDirSetup 配置Qemu工具目录
func QemuToolDirSetup(dir string) error {
	if !xutil.IsExisted(dir) {
		return errors.Errorf("qemu tool directory (%s) does not exist", dir)
	}

	ioToolPath = filepath.Join(dir, ioToolName)
	imgToolPath = filepath.Join(dir, imgToolName)

	return checkQemuTool()
}

func fixQemuToolName() {
	if !xutil.IsWindowsPlatform() {
		return
	}
	for _, name := range []*string{&ioToolName, &imgToolName} {
		*name += ".exe"
	}
}

func checkQemuTool() error {
	for _, tool := range []string{ioToolPath, imgToolPath} {
		if !xutil.IsExisted(tool) {
			return errors.Errorf("qemu tool (%s) does not exist", tool)
		}
		r, o, e := command.Execute(tool + " -h")
		if r != 0 {
			return errors.Errorf("failed to execute %s, output: %s, error: %v", tool, o, e)
		}
	}
	return nil
}
