package main

import (
	"context"
	"fmt"

	"github.com/kisun-bit/drpkg/defs"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/platform/info"
	"github.com/kisun-bit/drpkg/platform/recovery/x2xcore"
	"github.com/kisun-bit/drpkg/xutil"
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

	fixer, err := x2xcore.NewSysFixer(context.Background(),
		&x2xcore.FixerCreateOptions{
			OfflineSysDisks: offlineDisks,
			RecoveryParam: x2xcore.RecoveryParameter{
				Source: x2xcore.Platform{
					Arch:      "amd64",
					CpuVendor: "",
					Base:      "virtual",
					Virt:      "vmware",
					PciList:   nil,
				},
				Target: x2xcore.Platform{
					Arch:      "amd64",
					CpuVendor: "",
					Base:      "virtual",
					Virt:      "kvm",
					PciList:   nil,
				},
				OSType: "linux",
				Network: x2xcore.NetworkConfig{
					Enable: false,
					Interfaces: []x2xcore.InterfaceConfig{
						{
							MAC:     "52:54:00:3c:50:bd",
							Name:    "zktest",
							Enabled: true,
							DHCP:    false,
							IPAddr: []x2xcore.IPConfig{
								{
									Address: "192.168.122.204/24",
								},
							},
							DNS:     nil,
							Gateway: "192.168.122.1",
						},
					},
				},
			},
			InRepairVM: false,
		},
		nil)

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

	cfg, _ := fixer.GetPreferHostConfig(defs.HPVTKvm)
	fmt.Println(xutil.Pretty(cfg))
}
