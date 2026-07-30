package main

import (
	"path/filepath"
	"time"

	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/ps/recovery/x2xcore"
)

func runWindowsFirstBoot() error {

	marker := filepath.Join(
		extend.ExecDir(),
		".firstboot.completed",
	)

	if extend.IsExisted(marker) {

		logger.Infof(
			"component=firstboot status=completed action=skip",
		)

		return nil
	}

	logger.Infof(
		"component=firstboot action=start",
	)

	importForeignDisk()

	executeFirstBootScript()

	applyNetworkConfig()

	markFirstBootCompleted(marker)

	logger.Infof(
		"component=firstboot status=finished",
	)

	return nil
}

func importForeignDisk() {

	start := time.Now()

	logger.Infof(
		"component=disk action=import_foreign start",
	)

	err := x2xcore.ImportForeignDisk()

	if err != nil {

		logger.Errorf(
			"component=disk action=import_foreign status=failed error=%v",
			err,
		)

		return
	}

	logger.Infof(
		"component=disk action=import_foreign status=success duration=%s",
		time.Since(start),
	)

}
