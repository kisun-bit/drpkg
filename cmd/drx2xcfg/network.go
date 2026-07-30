package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/ps/recovery/x2xcore"
)

func applyNetworkConfig() {

	configFile := filepath.Join(
		extend.ExecDir(),
		x2xcore.NetworkConfigFileName,
	)

	if !extend.IsExisted(configFile) {

		logger.Infof(
			"component=network action=apply status=skip reason=config_missing",
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
