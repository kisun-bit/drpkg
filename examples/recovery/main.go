package main

import (
	"context"

	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/ps/info"
	"github.com/kisun-bit/drpkg/ps/recovery"
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

	fixer, err := recovery.NewSysFixer(context.Background(), &recovery.FixerCreateOptions{
		OfflineSysDisks: offlineDisks,
		RecoveryParam: recovery.RecoveryParameter{
			Source: recovery.Platform{
				BootMode: "bios",
				Arch:     "amd64",
				Base:     "virtual",
				Virt:     "vmware",
			},
			Target: recovery.Platform{
				BootMode: "bios",
				Arch:     "amd64",
				Base:     "virtual",
				Virt:     "vmware",
			},
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
}
