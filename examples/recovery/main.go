package main

import (
	"context"

	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/ps/info"
	"github.com/kisun-bit/drpkg/ps/recovery/x2xcore"
)

func main() {
	disks, err := info.QueryDisks()
	if err != nil {
		logger.Fatalf("QueryDisks: %v\n", err)
	}
	offlineDisks := make([]string, 0)
	for _, disk := range disks {
		offlineDisks = append(offlineDisks, disk.Device)
	}

	fixer, err := x2xcore.NewSysFixer(context.Background(), &x2xcore.FixerCreateOptions{
		OfflineSysDisks: offlineDisks,
		RecoveryParam: x2xcore.RecoveryParameter{
			Source: x2xcore.Platform{
				Arch: "amd64",
				Base: "virtual",
				Virt: "none",
			},
			Target: x2xcore.Platform{
				Arch: "amd64",
				Base: "virtual",
				Virt: "kvm",
			},
			X2xLibrary:         "/root/library",
			LuksGlobalPassword: "Jrsa1234/",
		},
	})

	if err != nil {
		logger.Fatalf("NewSysFixer: %v\n", err)
	}
	defer fixer.Cleanup()

	if err = fixer.Prepare(); err != nil {
		logger.Errorf("Prepare: %v\n", err)
		return
	}

	if err = fixer.Repair(); err != nil {
		logger.Errorf("Repair: %v\n", err)
		return
	}
}
