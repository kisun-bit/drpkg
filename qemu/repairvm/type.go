package repairvm

import (
	"os/exec"

	"github.com/google/uuid"
	"github.com/kisun-bit/drpkg/ps/recovery/x2xcore"
)

type Disk struct {
	// Index 磁盘索引编号
	Index int `json:"index"`

	// Path 磁盘设备路径或镜像文件路径
	Path string `json:"path"`

	// LBA 逻辑块地址（Logical Block Address）
	LBA int64 `json:"lba"`

	// PBA 物理块地址（Physical Block Address）
	PBA int64 `json:"pba"`

	// Size 磁盘大小，单位：字节
	Size int64 `json:"size"`
}

type Option struct {
	// VmBootDiskFile 虚拟机启动磁盘镜像。
	//
	// 支持的类型包括：
	//   - QCOW2 等虚拟磁盘镜像
	VmBootDiskFile string `json:"vmBootDiskFile"`

	// OfflineSystemDisks 离线系统磁盘列表。
	//
	// 用于描述待修复系统所在的磁盘信息，
	// 支持多个磁盘场景（如系统盘、数据盘等）。
	OfflineSystemDisks []Disk `json:"offlineSystemDisks"`

	// RecoveryParams 恢复过程参数配置。
	RecoveryParams x2xcore.RecoveryParameter `json:"recoveryParams"`

	// SimulatorConfigFile 模拟器配置文件路径。
	//
	// 配置虚拟硬件架构与 QEMU 模拟器程序的映射关系。
	//
	// 示例：
	// {
	//     "amd64": "qemu-system-x86_64",
	//     "arm64": "qemu-system-aarch64"
	// }
	SimulatorConfigFile string `json:"simulatorConfig"`

	// DriverDBImageFile 驱动库文件路径。
	DriverDBImageFile string `json:"driverDBImageFile"`
}

type Vm struct {
	opt *Option

	vmBootDisk  string // 修复虚拟机：启动磁盘的磁盘文件
	vmBootImage string // 修复虚拟机：启动镜像的镜像文件
	sockFile    string // 修复虚拟机：virtio-serial
	arch        string // 修复虚拟机：架构
	simulator   string // 修复虚拟机：模拟器

	uuid_     uuid.UUID
	cacheDir  string
	cmdCaller string
	cmdArgs   []string
	cmd       *exec.Cmd
}
