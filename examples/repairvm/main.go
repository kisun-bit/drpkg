//go:build linux

package main

import (
	"context"

	"github.com/kisun-bit/drpkg/defs"
	"github.com/kisun-bit/drpkg/xutil"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/platform/recovery/x2xcore"
	"github.com/kisun-bit/drpkg/qemu/repairvm"
)

func main() {
	vm, err := repairvm.Create(context.Background(),
		&repairvm.Option{
			//VmBootDiskFile: "/home/zk/runstor/plugin/imgfixer/resource/repairvm/image/windows/amd64/universal/re.windows.amd64.iso",
			//VmBootDiskFile: "/var/lib/libvirt/images/repairvm.alpine3.17.9.amd64.qcow2",
			VmBootDiskFile: "/home/zk/runstor/plugin/imgfixer/resource/repairvm/image/linux/arm64/uefi/re.linux.arm64.k4.qcow2",
			RecoveryParams: x2xcore.RecoveryParameter{
				OfflineSystemDisks: []x2xcore.Disk{
					{
						Index: 0,
						//Path:  "/data/runstor/restore/sysbackup/kvm_restore_virt_6obC762Nkr1ASmam7z/31c9aac91c6b5c4eeeba8a0ffcfe130b.qcow2",
						//Path: "/instance_ugeh/tenant_dfte/cdp_job_conf_fnxbvdsdye_42/Job_0DPB8hS2bY/qcow/restore/fdf9626717d54249807741aa85b459ae/pci-0000:00:0a.0",
						//Path: "/instance_ugeh/tenant_dfte/job_conf_jwcjoqjjne_39/Job_gHUhGaBKQD/vm-66212/13261108198209746070.qcow2",
						//Path: "/instance_ugeh/tenant_dfte/job_conf_7o0507vl47_40/Job_Q4Qq7FMC22/vm-67101/4143985988262948118.qcow2",
						//Path: "/instance_ugeh/tenant_dfte/job_conf_y49ew5uq13_41/Job_UYtiwsLLrz/vm-81115/1653524774799371602.qcow2.overlay",
						//Path: "/instance_ugeh/tenant_dfte/job_conf_y49ew5uq13_41/Job_AnAamq3OyN/vm-74628/13905887725040284033.qcow2.overlay",
						//Path: "/instance_ugeh/tenant_dfte/job_conf_ucatcf0f5t_42/Job_GOyn2QEOBR/vm-73362/12896675651414882390.qcow2.overlay",
						//Path: "/instance_ugeh/tenant_dfte/job_conf_ucatcf0f5t_42/Job_GOyn2QEOBR/vm-73372/10087003536009437921.qcow2.overlay",
						Path: "/home/zk/runstor/plugin/imgfixer/test.qcow2",
						LBA:  512,
						PBA:  512,
						Size: 42949672960,
					},
				},
				Source: x2xcore.Platform{
					Arch:      "arm64",
					CpuVendor: "",
					Base:      defs.HPVirt,
					Virt:      defs.HPVTKvm,
				},
				Target: x2xcore.Platform{
					Arch:      "arm64",
					CpuVendor: "",
					Base:      defs.HPVirt,
					Virt:      defs.HPVTKvm,
				},
				TimeoutSeconds: 0,
				//OSType:         "windows",
				OSType:                              "linux",
				X2xLibrary:                          "",
				FsckFs:                              false,
				SkipDriverRepairIfPlatformUnchanged: false,
				SourceDeviceMap:                     nil,
				LuksGlobalPassword:                  "",
				//BitlockerGlobalRecoveryKey:          "658394-314743-465025-566445-624525-500731-463716-439813",
				Network:             x2xcore.NetworkConfig{},
				RaidNotExisted:      false,
				MultipathNotExisted: true,
			},
			SimulatorConfigFile: "/var/lib/libvirt/images/s.json",
			DriverDBImageFile:   "/var/lib/libvirt/images/library.iso",
			ForceUseTcg:         false,
		})

	if err != nil {
		logger.Error("Create: ", err)
		return
	}

	defer vm.Release()

	extra, err := vm.Repair()
	if err != nil {
		logger.Error("Repair: ", err)
	}

	logger.Debugf("result: %s", xutil.Pretty(extra))
}
