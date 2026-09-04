//go:build linux

package repairvm

import (
	"bytes"
	"context"
	"net"
	"os/exec"

	"github.com/google/uuid"
	"github.com/kisun-bit/drpkg/defs"
	"github.com/kisun-bit/drpkg/platform/recovery/x2xcore"
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

	// Firmware UEFI 固件配置。
	//
	// arm64 虚拟机只能以 UEFI 方式启动：留空时自动探测发行版默认的
	// AAVMF/EDK2 固件（/usr/share/AAVMF、/usr/share/edk2 等），探测
	// 失败则创建报错；amd64 默认使用 BIOS 启动，仅当配置了 Firmware
	// 时才使用 UEFI。
	//
	// 注意：VarsTemplate 必须配置模板文件（如
	// /usr/share/AAVMF/AAVMF_VARS.fd），不要配置 libvirt 的实例变量
	// 文件（/var/lib/libvirt/qemu/nvram/<vm>_VARS.fd）——实例私有副本
	// 由本包从模板自动拷贝生成，生命周期与修复虚拟机一致。
	Firmware FirmwareSpec `json:"firmware"`

	// BootMode 修复虚拟机的启动模式（可选）。
	//
	// 取值 defs.BootModeBIOS / defs.BootModeUEFI。留空时由架构
	// 决定：arm64 强制 UEFI，amd64 默认 BIOS（配置了 Firmware 时
	// 使用 UEFI）。显式指定 UEFI 但未配置 Firmware 的 amd64 场景，
	// 同样走固件探测。
	BootMode defs.BootMode `json:"bootMode"`

	// ForceUseTcg 是否强制启用TCG
	ForceUseTcg bool `json:"forceUseTcg"`
}

type Vm struct {
	ctx    context.Context
	cancel context.CancelFunc

	opt *Option

	vmBootDisk  string          // 修复虚拟机：启动磁盘的磁盘文件
	vmBootImage string          // 修复虚拟机：启动镜像的镜像文件
	reqSockFile string          // 修复虚拟机：virtio-serial，用于请求调用
	logSockFile string          // 修复虚拟机：virtio-serial，用于日志
	arch        string          // 修复虚拟机：架构
	simulator   string          // 修复虚拟机：模拟器
	machine     string          // 修复虚拟机：机型（q35 / virt）
	firmware    *firmwareConfig // 修复虚拟机：UEFI 固件（nil 表示 BIOS 启动）

	logs chan x2xcore.LogEntry // 修复日志

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
	cmdStdout    bytes.Buffer
}
