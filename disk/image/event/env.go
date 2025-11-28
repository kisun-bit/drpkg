//go:build linux

package event

import (
	"path/filepath"

	"github.com/kisun-bit/drpkg/command"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/pkg/errors"
)

var (
	ioToolName  = "imgio"
	imgToolName = "img"

	ioToolPath  = ioToolName
	imgToolPath = imgToolName
)

func init() {
	fixQemuToolName()
}

// QemuToolDirSetup 配置Qemu工具目录
func QemuToolDirSetup(dir string) error {
	if !extend.IsExisted(dir) {
		return errors.Errorf("qemu tool directory (%s) does not exist", dir)
	}

	ioToolPath = filepath.Join(dir, ioToolName)
	imgToolPath = filepath.Join(dir, imgToolName)

	return checkQemuTool()
}

func fixQemuToolName() {
	if !extend.IsWindowsPlatform() {
		return
	}
	for _, name := range []*string{&ioToolName, &imgToolName} {
		*name += ".exe"
	}
}

func checkQemuTool() error {
	for _, tool := range []string{ioToolPath, imgToolPath} {
		if !extend.IsExisted(tool) {
			return errors.Errorf("qemu tool (%s) does not exist", tool)
		}
		r, o, e := command.Execute(tool + " -h")
		if r != 0 {
			return errors.Errorf("failed to execute %s, output: %s, error: %v", tool, o, e)
		}
	}
	return nil
}
