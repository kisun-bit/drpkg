package main

import (
	"context"
	"fmt"

	"github.com/kisun-bit/drpkg/defs"
	"github.com/kisun-bit/drpkg/xutil"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/platform/info"
	"github.com/kisun-bit/drpkg/platform/recovery/x2xcore"
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
		//&x2xcore.FixerCreateOptions{
		//	OfflineSysDisks: offlineDisks,
		//	RecoveryParam: x2xcore.RecoveryParameter{
		//		Source: x2xcore.Platform{
		//			Arch: "amd64",
		//			Base: "virtual",
		//			Virt: "none",
		//		},
		//		Target: x2xcore.Platform{
		//			Arch: "amd64",
		//			Base: "virtual",
		//			Virt: "kvm",
		//		},
		//		X2xLibrary:         "/root/library",
		//		LuksGlobalPassword: "Jrsa1234/",
		//		Network: x2xcore.NetworkConfig{
		//			Interfaces: []x2xcore.InterfaceConfig{
		//				{
		//					MAC: "00:0c:29:b7:23:41", // vmware 张凯加密系统
		//					//MAC:     "00:0c:29:ed:76:c2", // vmware 张凯suse12sp4
		//					//MAC:     "00:50:56:ac:30:ca", // vmwaer 罗潇centos6.5uefi
		//					//MAC:     "00:50:56:ac:84:14", // vmware 罗潇ubuntu22
		//					//MAC:     "00:50:56:ac:66:16", // vmware centos4_oracle
		//					//MAC:     "00:50:56:ac:a0:5b", // vmware 罗潇最新centos-123测试
		//					Name:    "zktestif01",
		//					Enabled: true,
		//					MTU:     1500,
		//					DHCP:    false,
		//					IPAddr: []x2xcore.IPConfig{
		//						{"192.168.1.43/24"},
		//					},
		//					DNS:     []string{"8.8.4.4"},
		//					Gateway: "192.168.1.1",
		//				},
		//			},
		//			GlobalDNS: []string{"8.8.4.4", "8.8.8.8"},
		//			Routes:    nil,
		//		},
		//	},

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
				Network: x2xcore.NetworkConfig{
					Enable: true,
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
