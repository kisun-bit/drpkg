package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/kardianos/service"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/ps/minisvc"
	"github.com/kisun-bit/drpkg/ps/recovery/x2xcore"

	"go.uber.org/zap/zapcore"
)

const (
	serviceName = "drx2xcfg"
	version     = "6.19.00083"
)

func main() {

	initLogger()

	logger.Infof(
		"service=%s version=%s event=start",
		serviceName,
		version,
	)

	cfg := buildServiceConfig()

	err := minisvc.Run(minisvc.Options{

		Version: version,

		Config: cfg,

		Lifecycle: minisvc.Lifecycle{

			Start: func() error {

				switch runtime.GOOS {

				case "windows":

					return runWindowsFirstBoot()

				case "linux":

					logger.Infof(
						"platform=linux event=firstboot_not_implemented",
					)

					return nil

				default:

					logger.Warnf(
						"unsupported platform=%s",
						runtime.GOOS,
					)

					return nil
				}
			},
		},
	})

	if err != nil {

		_, _ = fmt.Fprintln(
			os.Stderr,
			err,
		)

		os.Exit(1)
	}
}

func initLogger() {
	logFile := filepath.Join(
		extend.ExecDir(),
		fmt.Sprintf("%s.default.log", x2xcore.FirstBootProcFilePrefix),
	)

	lg := logger.NewLogger(
		serviceName,
		zapcore.DebugLevel,
		logger.NewFileLogWriter(
			logFile,
			30<<20,
			7,
			0,
		),
	)

	logger.SetupDefaultLogger(lg)
}

func buildServiceConfig() service.Config {

	cfg := service.Config{

		Name: serviceName,

		DisplayName: serviceName,

		Description: "Used to automate system configuration during the first boot after recovery or migration",
	}

	switch runtime.GOOS {

	case "windows":

		cfg.Option = map[string]interface{}{
			"OnFailure": "restart",
		}

	case "linux":

		cfg.Dependencies = []string{
			"After=network.target syslog.target",
		}
	}

	return cfg
}

func markFirstBootCompleted(
	marker string,
) {

	content := fmt.Sprintf(
		"first boot configuration completed at %s\n",
		time.Now().Format(time.RFC3339),
	)

	err := os.WriteFile(
		marker,
		[]byte(content),
		0644,
	)

	if err != nil {

		logger.Errorf(
			"component=firstboot action=mark status=failed error=%v",
			err,
		)

		return
	}

	logger.Infof(
		"component=firstboot action=mark status=completed file=%s",
		marker,
	)

	renameProcessedFiles()

}

func renameProcessedFiles() {

	files := []string{

		x2xcore.FirstBootProcPowershellScriptName,
		x2xcore.FirstBootProcBatScriptName,
		x2xcore.FirstBootProcShellScriptName,

		x2xcore.FirstBootProcNetworkConfigFileName,
	}

	for _, name := range files {

		src := filepath.Join(
			extend.ExecDir(),
			name,
		)

		if !extend.IsExisted(src) {

			continue
		}

		dst := src + ".processed"

		if err := os.Rename(
			src,
			dst,
		); err != nil {

			logger.Warnf(
				"component=firstboot action=archive file=%s status=failed error=%v",
				src,
				err,
			)

			continue
		}

		logger.Infof(
			"component=firstboot action=archive file=%s status=success",
			src,
		)

	}

}
