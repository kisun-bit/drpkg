package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/platform/recovery/x2xcore"
	"github.com/kisun-bit/drpkg/xutil"
)

func applyNetworkConfig() {

	configFile := filepath.Join(
		xutil.ExecDir(),
		x2xcore.FirstBootProcNetworkConfigFileName,
	)

	if !xutil.IsExisted(configFile) {

		logger.Infof(
			"component=network action=apply status=skip reason=config_missing(%s)",
			configFile,
		)

		return
	}

	logger.Infof(
		"component=network action=load config=%s",
		configFile,
	)

	data, err := os.ReadFile(
		configFile,
	)

	if err != nil {

		logger.Errorf(
			"component=network action=read status=failed error=%v",
			err,
		)

		return
	}

	var cfg x2xcore.NetworkConfig

	if err = json.Unmarshal(
		data,
		&cfg,
	); err != nil {

		logger.Errorf(
			"component=network action=parse status=failed error=%v",
			err,
		)

		return
	}

	start := time.Now()

	logger.Infof(
		"component=network action=apply start",
	)

	if err = x2xcore.ApplyNetworkConfig(
		cfg,
	); err != nil {

		logger.Errorf(
			"component=network action=apply status=failed error=%v",
			err,
		)

		return
	}

	logger.Infof(
		"component=network action=apply status=success duration=%s",
		time.Since(start),
	)

}
