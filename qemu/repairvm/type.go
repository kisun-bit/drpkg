//go:build linux

package repairvm

import (
	"context"
	"net"
	"os/exec"

	"github.com/google/uuid"
	"github.com/kisun-bit/drpkg/ps/recovery/x2xcore"
)

type Option struct {
	// RecoveryParams 恢复过程参数配置。
	RecoveryParams x2xcore.RecoveryParameter `json:"recoveryParams"`

	// VmBootDiskFile 修复虚拟机启动磁盘镜像。
	//
	// 支持的类型包括：
	//   - QCOW2
	//   - ISO
	VmBootDiskFile string `json:"vmBootDiskFile"`

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
	ctx    context.Context
	cancel context.CancelFunc

	opt *Option

	vmBootDisk  string // 修复虚拟机：启动磁盘的磁盘文件
	vmBootImage string // 修复虚拟机：启动镜像的镜像文件
	reqSockFile string // 修复虚拟机：virtio-serial，用于请求调用
	logSockFile string // 修复虚拟机：virtio-serial，用于日志
	arch        string // 修复虚拟机：架构
	simulator   string // 修复虚拟机：模拟器

	uuid_        uuid.UUID
	reqSockName  string
	logSockName  string
	reqSockConn  *net.Conn
	logSockConn  *net.Conn
	cacheDir     string
	cmdCaller    string
	cmdArgs      []string
	offlineDisks []string
	cmd          *exec.Cmd
}
