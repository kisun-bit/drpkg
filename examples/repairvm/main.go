package main

import (
	"context"

	"github.com/kisun-bit/drpkg/define"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/ps/recovery/x2xcore"
	"github.com/kisun-bit/drpkg/qemu/repairvm"
)

func main() {
	vm, err := repairvm.Create(context.Background(),
		&repairvm.Option{
			VmBootDiskFile: "/var/lib/libvirt/images/rs_winpe.x86_64.iso",
			OfflineSystemDisks: []repairvm.Disk{
				{
					Index: 0,
					Path:  "/var/lib/libvirt/images/xp.qcow2",
					LBA:   512,
					PBA:   512,
					Size:  107374182400,
				},
			},
			RecoveryParams: x2xcore.RecoveryParameter{
				Source: x2xcore.Platform{
					Arch:      "amd64",
					CpuVendor: "",
					Base:      define.HPVirt,
					Virt:      define.HPVTKvm,
				},
				Target: x2xcore.Platform{
					Arch:      "amd64",
					CpuVendor: "",
					Base:      define.HPVirt,
					Virt:      define.HPVTKvm,
				},
				TimeoutSeconds:                      0,
				OSType:                              "windows",
				X2xLibrary:                          "",
				FsckFs:                              true,
				SkipDriverRepairIfPlatformUnchanged: false,
				SourceDeviceMap:                     nil,
				LuksGlobalPassword:                  "",
				BitlockerGlobalPassword:             "",
				Network:                             x2xcore.NetworkConfig{},
				RaidNotExisted:                      false,
				MultipathNotExisted:                 true,
			},
			SimulatorConfigFile: "/var/lib/libvirt/images/s.json",
			DriverDBImageFile:   "/var/lib/libvirt/images/library.iso",
		})

	if err != nil {
		logger.Error("Create: ", err)
	}

	defer vm.Release()

	if err = vm.Repair(); err != nil {
		logger.Error("Repair: ", err)
	}
}
