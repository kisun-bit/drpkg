package info

import (
	"github.com/kisun-bit/drpkg/sys/info/storage"
	"github.com/kisun-bit/drpkg/sys/ioctl"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

type CommonInfo struct {
	CPU       []cpu.InfoStat            `json:"cpu"`
	Memory    []mem.VirtualMemoryStat   `json:"memory"`
	Nic       []ioctl.EthernetExtraInfo `json:"nic"`
	RouteList []ioctl.RouteGeneral      `json:"route_list"`
	Hostname  string                    `json:"hostname"`
	GOOS      string                    `json:"goos"`
	GOARCH    string                    `json:"goarch"`
	IsLiveOS  bool                      `json:"is_live_os"`
	BootMode  string                    `json:"boot_mode"`
	OsName    string                    `json:"os_name"`
	OsVersion string                    `json:"os_version"`
}

type InfoLinux[T storage.LinuxHardDiskGPT | storage.LinuxHardDiskMBR | storage.LinuxHardDiskNoPartitionTable] struct {
	CommonInfo
	ReleaseName       string           `json:"release_name"`
	ReleaseVersion    string           `json:"release_version"`
	ReleaseID         string           `json:"release_id"`
	ReleaseVersionID  string           `json:"release_version_id"`
	ReleasePrettyName string           `json:"release_pretty_name"`
	DefaultKernel     string           `json:"default_kernel"`
	KernelImg         string           `json:"kernel_img"`
	Initrd            string           `json:"initrd"`
	Kernels           []string         `json:"kernels"`
	GRUBVersion       int              `json:"grub_version"`
	GRUBInstallPath   string           `json:"grub_install_path"`
	GRUBMkConfigPath  string           `json:"grub_mkconfig_path"`
	GRUBTarget        string           `json:"grub_target"`
	LVM               storage.LinuxLVM `json:"lvm"`
	Storage           []T              `json:"storage"`
	SWAP              []storage.Swap   `json:"swap"`
}

type InfoWindows[T storage.WindowsHardDiskGPT | storage.WindowsHardDiskMBR | storage.WindowsHardDiskRAW] struct {
	CommonInfo
	Storage []T `json:"storage"`
}
