package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/kardianos/service"
	"github.com/kisun-bit/drpkg/ps/minisvc"
)

//
// 实现恢复后首次启动的自动配置
//

func main() {
	version := "6.19.00083"

	cfg := service.Config{
		Name:        "drx2xcfg",
		DisplayName: "drx2xCfg",
		Description: "Used to automate system configuration during the first boot after recovery or migration",
	}

	switch runtime.GOOS {
	case "windows":
		cfg.Option["OnFailure"] = "restart"
	case "linux":
		cfg.Dependencies = []string{"After=network.target syslog.target"}
	}

	e := minisvc.Run(minisvc.Options{
		Version: version,
		Config:  cfg,
		Lifecycle: minisvc.Lifecycle{
			Start: func() error {
				// TODO 1.读取首次启动脚本并执行；2.读取网络配置并执行；
				return nil
			},
		},
	})

	if e != nil {
		_, _ = fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
