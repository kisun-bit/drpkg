package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/ps/recovery/x2xcore"
)

func executeFirstBootScript() {

	script := filepath.Join(
		extend.ExecDir(),
		x2xcore.FirstBootProcPowershellScriptName,
	)

	if !extend.IsExisted(script) {

		logger.Infof(
			"component=powershell action=execute status=skip reason=file_missing",
		)

		return
	}

	start := time.Now()

	ctx, cancel := context.WithTimeout(
		context.Background(),
		15*time.Minute,
	)

	defer cancel()

	logger.Infof(
		"component=powershell action=execute script=%s",
		script,
	)

	cmd := exec.CommandContext(
		ctx,
		"powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy",
		"Bypass",
		"-File",
		script,
	)

	out, err := cmd.CombinedOutput()

	logger.Debugf(
		"component=powershell output=%s",
		string(out),
	)

	if ctx.Err() != nil {

		logger.Errorf(
			"component=powershell status=timeout",
		)

		return
	}

	if err != nil {

		logger.Errorf(
			"component=powershell status=failed error=%v",
			err,
		)

		return
	}

	logger.Infof(
		"component=powershell status=success duration=%s",
		time.Since(start),
	)

}
