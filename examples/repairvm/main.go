//go:build linux

package main

import (
	"context"
	"time"

	"github.com/kisun-bit/drpkg/define"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/ps/recovery/x2xcore"
	"github.com/kisun-bit/drpkg/qemu/repairvm"
)

func main() {
	vm, err := repairvm.Create(context.Background(),
		&repairvm.Option{
			VmBootDiskFile: "/var/lib/libvirt/images/repairvm.win10.amd64.iso",
			//VmBootDiskFile: "/var/lib/libvirt/images/repairvm.alpine3.17.9.amd64.qcow2",
			OfflineSystemDisks: []repairvm.Disk{
				{
					Index: 0,
					//Path:  "/data/runstor/restore/sysbackup/kvm_restore_virt_6obC762Nkr1ASmam7z/31c9aac91c6b5c4eeeba8a0ffcfe130b.qcow2",
					//Path: "/instance_ugeh/tenant_dfte/cdp_job_conf_fnxbvdsdye_42/Job_0DPB8hS2bY/qcow/restore/fdf9626717d54249807741aa85b459ae/pci-0000:00:0a.0",
					//Path: "/instance_ugeh/tenant_dfte/job_conf_jwcjoqjjne_39/Job_gHUhGaBKQD/vm-66212/13261108198209746070.qcow2",
					//Path: "/instance_ugeh/tenant_dfte/job_conf_7o0507vl47_40/Job_Q4Qq7FMC22/vm-67101/4143985988262948118.qcow2",
					//Path: "/instance_ugeh/tenant_dfte/job_conf_y49ew5uq13_41/Job_UYtiwsLLrz/vm-81115/1653524774799371602.qcow2.overlay",
					Path: "/instance_ugeh/tenant_dfte/job_conf_y49ew5uq13_41/Job_AnAamq3OyN/vm-74628/13905887725040284033.qcow2.overlay",
					LBA:  512,
					PBA:  512,
					Size: 42949672960,
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
				TimeoutSeconds: 0,
				OSType:         "windows",
				//OSType:                              "linux",
				X2xLibrary:                          "",
				FsckFs:                              false,
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

	time.Sleep(10 * time.Minute)
}
