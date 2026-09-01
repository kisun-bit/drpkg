package x2xcore

import (
	"fmt"

	"github.com/kisun-bit/drpkg/logger"
)

type LogLevel = string

const (
	LogInfo  LogLevel = "info"
	LogWarn           = "warn"
	LogError          = "error"
)

type LogEntry struct {
	Level LogLevel `json:"level"`
	MsgEn string   `json:"msgEn"`
	MsgZh string   `json:"msgZh"`
}

func (le *LogEntry) String() string {
	return fmt.Sprintf("LOG(\"%s\", \"%s\", \"%s\")",
		le.Level, le.MsgZh, le.MsgEn)
}

func (le *LogEntry) Println() {
	var lg = logger.Debugf
	switch le.Level {
	case LogInfo:
		lg = logger.Infof
	case LogWarn:
		lg = logger.Warnf
	case LogError:
		lg = logger.Errorf
	}

	lg(le.String())
}

type LangTpl struct {
	Zh string
	En string
}

var (
	LogTplForReadyWith0Args = LangTpl{
		Zh: "加载异构修复环境",
		En: "Loading heterogeneous recovery environment",
	}

	LogTplForOfflineSystemReadyWith0Args = LangTpl{
		Zh: "识别离线系统的磁盘集",
		En: "Identifying offline system disks",
	}

	LogTplForResetWith0Args = LangTpl{
		Zh: "重置存储映射环境",
		En: "Resetting storage mapping environment",
	}

	LogTplForOpenLUKSWith0Args = LangTpl{
		Zh: "打开 LUKS 加密卷",
		En: "Opening LUKS encrypted volumes",
	}

	LogTplForEnumFsWith0Args = LangTpl{
		Zh: "扫描文件系统设备",
		En: "Scanning filesystem devices",
	}

	LogTplForFsckFsWith0Args = LangTpl{
		Zh: "修复文件系统",
		En: "Repairing filesystems",
	}

	LogTplForCleanElastioSnapWith0Args = LangTpl{
		Zh: "清理可能残留的 Elastio/Datto 快照",
		En: "Cleaning up potential residual Elastio/Datto snapshots",
	}

	LogTplForCleanSpecifiedPathWith1Args = LangTpl{
		Zh: "清理文件（/文件夹）：%s",
		En: "Cleaning up file/directory: %s",
	}

	LogTplForSpecifySystemBootDeviceWith0Args = LangTpl{
		Zh: "确定系统启动设备",
		En: "Determining system boot device",
	}

	LogTplForPrintSystemBootDeviceWith2Args = LangTpl{
		Zh: "系统启动设备：%s（挂载点：%s）",
		En: "System boot device: %s (mount point: %s)",
	}

	LogTplForBootableKernelWith1Args = LangTpl{
		Zh: "可启动内核版本：%s",
		En: "Bootable kernel version: %s",
	}

	LogTplForLoadRegistryWith0Args = LangTpl{
		Zh: "加载注册表",
		En: "Loading registry",
	}

	LogTplForUnloadRegistryWith0Args = LangTpl{
		Zh: "卸载注册表",
		En: "Unloading registry",
	}

	LogTplForMountSystemWith0Args = LangTpl{
		Zh: "切换至离线系统环境",
		En: "Switching to offline system environment",
	}

	LogTplForPrintControlSetWith1Args = LangTpl{
		Zh: "系统运行时控制集：ControlSet00%d",
		En: "Current system control set: ControlSet00%d",
	}

	LogTplForPrintDriverDatabaseLegacyWith0Args = LangTpl{
		Zh: "系统驱动数据库类型：CDB",
		En: "System driver database type: CDB",
	}

	LogTplForPrintDriverDatabasePnpWith0Args = LangTpl{
		Zh: "系统驱动数据库类型：PNP",
		En: "System driver database type: PNP",
	}

	LogTplForPrintSystemBootKernelWith2Args = LangTpl{
		Zh: "系统启动内核：%s",
		En: "System boot kernel: %s",
	}

	LogTplForPrintSystemGrubWith2Args = LangTpl{
		Zh: "系统开机引导程序：%s（版本：%v）",
		En: "System bootloader: %s (version: %v)",
	}

	LogTplForPrintSystemBootTypeWith1Args = LangTpl{
		Zh: "系统启动类型：%s",
		En: "System boot type: %s",
	}

	LogTplForPrintDistroWith1Args = LangTpl{
		Zh: "系统发行版信息：%s",
		En: "Operating system distribution: %s",
	}

	LogTplForPrintInitrdMgrWith1Args = LangTpl{
		Zh: "系统 Initramfs 管理工具：%s",
		En: "System Initramfs management tool: %s",
	}

	LogTplForDisableSELinuxWith0Args = LangTpl{
		Zh: "禁用 SELinux",
		En: "Disabling SELinux",
	}

	LogTplForDisableAutoRebootWith0Args = LangTpl{
		Zh: "禁用自动重启",
		En: "Disabling automatic reboot",
	}

	LogTplForRepairPAMWith0Args = LangTpl{
		Zh: "修复 PAM 模块",
		En: "Repairing PAM modules",
	}

	LogTplForRepairGrubWith0Args = LangTpl{
		Zh: "修复 GRUB 配置",
		En: "Repairing GRUB configuration",
	}

	LogTplForRepairFstabWith0Args = LangTpl{
		Zh: "修复 fstab 配置",
		En: "Repairing fstab configuration",
	}

	LogTplForIgnoreRepairWith1Args = LangTpl{
		Zh: "系统版本（%s）过旧，跳过硬件修复和网络注入。恢复后请使用 IDE 等传统硬件启动。",
		En: "System version (%s) is too old. Skipping hardware repair and network injection. Please boot with legacy hardware such as IDE after recovery.",
	}

	LogTplForInjectLegacyDriversWith0Args = LangTpl{
		Zh: "以传统方式（CDB）注入虚拟化平台驱动",
		En: "Injecting virtualization platform drivers using the legacy method (CDB)",
	}

	LogTplForSkipFirstBootServiceWith1Args = LangTpl{
		Zh: "系统版本（%s）过旧，跳过首次启动配置服务与网络配置注入。",
		En: "System version (%s) is too old. Skipping first-boot configuration service and network configuration injection.",
	}

	LogTplForNoLegacyBlockDriverWith1Args = LangTpl{
		Zh: "未找到适用于系统版本（%s）的 virtio 块设备启动驱动，恢复后请使用 IDE 等模拟磁盘启动。",
		En: "No virtio block boot driver found for system version (%s). Please boot with an emulated disk such as IDE after recovery.",
	}

	LogTplForNoLegacyVirtualDriverWith2Args = LangTpl{
		Zh: "驱动库中没有适用于系统版本（%s）的 KVM 虚拟化驱动（%v），恢复后请使用 IDE 等模拟设备启动。",
		En: "No KVM virtualization driver (%v) is available in the driver library for system version (%s). Please boot with emulated devices such as IDE after recovery.",
	}

	LogTplForOptimizeUEFIWith0Args = LangTpl{
		Zh: "优化 UEFI 启动配置",
		En: "Optimizing UEFI boot configuration",
	}

	LogTplForOptimizeBCDWith0Args = LangTpl{
		Zh: "优化 BCD 引导配置",
		En: "Optimizing BCD boot configuration",
	}

	LogTplForInjectFirstBootServiceWith1Args = LangTpl{
		Zh: "注入首次启动配置服务：%s",
		En: "Injecting first-boot configuration service: %s",
	}

	LogTplForInjectNetworkToolFailedWith1Args = LangTpl{
		Zh: "注入网络配置程序失败：%v",
		En: "Failed to inject network configuration tool: %v",
	}

	LogTplForInjectNetworkConfigWith0Args = LangTpl{
		Zh: "写入系统网络配置",
		En: "Writing system network configuration",
	}

	LogTplForInjectNetworkConfigFailedWith0Args = LangTpl{
		Zh: "写入系统网络配置失败：%v",
		En: "Failed to write system network configuration: %v",
	}

	LogTplForUnconfigHVWith0Args = LangTpl{
		Zh: "解除 Hyper-V 平台驱动绑定",
		En: "Removing Hyper-V platform driver bindings",
	}

	LogTplForUnconfigKVMWith0Args = LangTpl{
		Zh: "解除 KVM 平台驱动绑定",
		En: "Removing KVM platform driver bindings",
	}

	LogTplForUnconfigXenWith0Args = LangTpl{
		Zh: "解除 Xen 平台驱动绑定",
		En: "Removing Xen platform driver bindings",
	}

	LogTplForUnconfigVmwareWith0Args = LangTpl{
		Zh: "解除 VMware 平台驱动绑定",
		En: "Removing VMware platform driver bindings",
	}

	LogTplForConfigHVWith0Args = LangTpl{
		Zh: "配置 Hyper-V 平台驱动支持",
		En: "Configuring Hyper-V platform driver support",
	}

	LogTplForConfigKVMWith0Args = LangTpl{
		Zh: "配置 KVM 平台驱动支持",
		En: "Configuring KVM platform driver support",
	}

	LogTplForConfigXenWith0Args = LangTpl{
		Zh: "配置 Xen 平台驱动支持",
		En: "Configuring Xen platform driver support",
	}

	LogTplForConfigVmwareWith0Args = LangTpl{
		Zh: "配置 VMware 平台驱动支持",
		En: "Configuring VMware platform driver support",
	}

	LogTplForIncompatibleBootPCIWith2Args = LangTpl{
		Zh: "检测到不兼容的启动设备：%s（%s）",
		En: "Detected incompatible boot device: %s (%s)",
	}

	LogTplForIncompatibleNonBootPCIWith2Args = LangTpl{
		Zh: "检测到不兼容的非启动设备：%s（%s），请在系统启动后安装相应驱动程序",
		En: "Detected incompatible non-boot device: %s (%s). Please install the appropriate driver after system startup.",
	}

	LogTplForMatchDriverWith1Args = LangTpl{
		Zh: "正在匹配硬件 %s 的驱动程序",
		En: "Matching driver for hardware %s",
	}

	LogTplForMatchDriverSuccessWith1Args = LangTpl{
		Zh: "硬件 %s 匹配到兼容驱动程序",
		En: "Found a compatible driver for hardware %s",
	}

	LogTplForUnlockBitlockerWith1Args = LangTpl{
		Zh: "正在解锁卷 %s 的 BitLocker",
		En: "Unlocking BitLocker on volume %s",
	}

	LogTplForUnlockBitlockerFailedWith2Args = LangTpl{
		Zh: "卷 %s 的 BitLocker 解锁失败：%v",
		En: "Failed to unlock BitLocker on volume %s: %v",
	}
)
