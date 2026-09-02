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
		Zh: "正在加载异构修复环境",
		En: "Loading heterogeneous recovery environment",
	}

	LogTplForOfflineSystemReadyWith0Args = LangTpl{
		Zh: "正在识别离线系统磁盘",
		En: "Identifying offline system disks",
	}

	LogTplForResetWith0Args = LangTpl{
		Zh: "正在重置存储映射环境",
		En: "Resetting storage mapping environment",
	}

	LogTplForOpenLUKSWith0Args = LangTpl{
		Zh: "正在打开 LUKS 加密卷",
		En: "Opening LUKS encrypted volumes",
	}

	LogTplForEnumFsWith0Args = LangTpl{
		Zh: "正在扫描文件系统",
		En: "Scanning filesystems",
	}

	LogTplForFsckFsWith0Args = LangTpl{
		Zh: "正在修复文件系统",
		En: "Repairing filesystems",
	}

	LogTplForCleanElastioSnapWith0Args = LangTpl{
		Zh: "正在清理可能残留的 Elastio/Datto 快照",
		En: "Cleaning up potential residual Elastio/Datto snapshots",
	}

	LogTplForCleanSpecifiedPathWith1Args = LangTpl{
		Zh: "正在清理文件或目录：%s",
		En: "Cleaning up file or directory: %s",
	}

	LogTplForSpecifySystemBootDeviceWith0Args = LangTpl{
		Zh: "正在识别系统启动设备",
		En: "Identifying system boot device",
	}

	LogTplForPrintSystemBootDeviceWith2Args = LangTpl{
		Zh: "系统启动设备：%s（挂载点：%s）",
		En: "System boot device: %s (mount point: %s)",
	}

	LogTplForBootableKernelWith1Args = LangTpl{
		Zh: "可启动内核：%s",
		En: "Bootable kernel: %s",
	}

	LogTplForLoadRegistryWith0Args = LangTpl{
		Zh: "正在加载注册表",
		En: "Loading registry",
	}

	LogTplForUnloadRegistryWith0Args = LangTpl{
		Zh: "正在卸载注册表",
		En: "Unloading registry",
	}

	LogTplForMountSystemWith0Args = LangTpl{
		Zh: "正在切换至离线系统环境",
		En: "Switching to offline system environment",
	}

	LogTplForPrintControlSetWith1Args = LangTpl{
		Zh: "当前系统控制集：ControlSet00%d",
		En: "Current system control set: ControlSet00%d",
	}

	LogTplForPrintDriverDatabaseLegacyWith0Args = LangTpl{
		Zh: "系统驱动数据库：CDB",
		En: "System driver database: CDB",
	}

	LogTplForPrintDriverDatabasePnpWith0Args = LangTpl{
		Zh: "系统驱动数据库：PNP",
		En: "System driver database: PNP",
	}

	LogTplForPrintSystemBootKernelWith1Args = LangTpl{
		Zh: "系统启动内核：%s",
		En: "System boot kernel: %s",
	}

	LogTplForPrintSystemGrubWith2Args = LangTpl{
		Zh: "系统引导程序：%s（版本：%v）",
		En: "System bootloader: %s (version: %v)",
	}

	LogTplForPrintSystemBootTypeWith1Args = LangTpl{
		Zh: "系统启动模式：%s",
		En: "System boot mode: %s",
	}

	LogTplForPrintDistroWith1Args = LangTpl{
		Zh: "系统发行版：%s",
		En: "Operating system distribution: %s",
	}

	LogTplForPrintInitrdMgrWith1Args = LangTpl{
		Zh: "Initramfs 管理工具：%s",
		En: "Initramfs management tool: %s",
	}

	LogTplForDisableSELinuxWith0Args = LangTpl{
		Zh: "正在禁用 SELinux",
		En: "Disabling SELinux",
	}

	LogTplForDisableAutoRebootWith0Args = LangTpl{
		Zh: "正在禁用自动重启",
		En: "Disabling automatic reboot",
	}

	LogTplForRepairPAMWith0Args = LangTpl{
		Zh: "正在修复 PAM 配置",
		En: "Repairing PAM configuration",
	}

	LogTplForRepairGrubWith0Args = LangTpl{
		Zh: "正在修复 GRUB 配置",
		En: "Repairing GRUB configuration",
	}

	LogTplForRepairFstabWith0Args = LangTpl{
		Zh: "正在修复 fstab 配置",
		En: "Repairing fstab configuration",
	}

	LogTplForIgnoreRepairWith1Args = LangTpl{
		Zh: "系统版本（%s）过旧，已跳过硬件修复和网络配置注入，请恢复后使用 IDE 等传统硬件启动。",
		En: "System version (%s) is too old. Hardware repair and network configuration injection were skipped. Please boot with legacy hardware such as IDE after recovery.",
	}

	LogTplForInjectLegacyDriversWith0Args = LangTpl{
		Zh: "正在以传统方式（CDB）注入虚拟化驱动",
		En: "Injecting virtualization drivers using the legacy method (CDB)",
	}

	LogTplForSkipFirstBootServiceWith1Args = LangTpl{
		Zh: "系统版本（%s）过旧，已跳过首次启动服务和网络配置注入。",
		En: "System version (%s) is too old. First-boot service and network configuration injection were skipped.",
	}

	LogTplForNoLegacyBlockDriverWith1Args = LangTpl{
		Zh: "未找到适用于系统版本（%s）的 VirtIO 块设备启动驱动，请恢复后使用 IDE 等模拟磁盘启动。",
		En: "No VirtIO block boot driver is available for system version (%s). Please boot with an emulated disk such as IDE after recovery.",
	}

	LogTplForNoLegacyVirtualDriverWith2Args = LangTpl{
		Zh: "未找到适用于系统版本（%s）的 KVM 虚拟化驱动（%v），请恢复后使用 IDE 等模拟设备启动。",
		En: "No KVM virtualization driver (%v) is available for system version (%s). Please boot with emulated devices such as IDE after recovery.",
	}

	LogTplForNonBootDriverInstalledWith2Args = LangTpl{
		Zh: "系统版本过旧，非启动驱动（%s）已放入驱动目录 %s，请在恢复后进入该目录手动安装驱动。",
		En: "System version is too old. Non-boot driver (%s) has been placed in %s. Please install the driver manually from that directory after recovery.",
	}

	LogTplForOptimizeUEFIWith0Args = LangTpl{
		Zh: "正在优化 UEFI 启动配置",
		En: "Optimizing UEFI boot configuration",
	}

	LogTplForOptimizeBCDWith0Args = LangTpl{
		Zh: "正在优化 BCD 启动配置",
		En: "Optimizing BCD boot configuration",
	}

	LogTplForInjectFirstBootServiceWith1Args = LangTpl{
		Zh: "正在注入首次启动服务：%s",
		En: "Injecting first-boot service: %s",
	}

	LogTplForInjectNetworkToolFailedWith1Args = LangTpl{
		Zh: "网络配置工具注入失败：%v",
		En: "Failed to inject network configuration tool: %v",
	}

	LogTplForInjectNetworkConfigWith0Args = LangTpl{
		Zh: "正在写入网络配置",
		En: "Writing network configuration",
	}

	LogTplForInjectNetworkConfigFailedWith1Args = LangTpl{
		Zh: "网络配置写入失败：%v",
		En: "Failed to write network configuration: %v",
	}

	LogTplForUnconfigHVWith0Args = LangTpl{
		Zh: "正在解除 Hyper-V 驱动绑定",
		En: "Removing Hyper-V driver bindings",
	}

	LogTplForUnconfigKVMWith0Args = LangTpl{
		Zh: "正在解除 KVM 驱动绑定",
		En: "Removing KVM driver bindings",
	}

	LogTplForUnconfigXenWith0Args = LangTpl{
		Zh: "正在解除 Xen 驱动绑定",
		En: "Removing Xen driver bindings",
	}

	LogTplForUnconfigVmwareWith0Args = LangTpl{
		Zh: "正在解除 VMware 驱动绑定",
		En: "Removing VMware driver bindings",
	}

	LogTplForConfigHVWith0Args = LangTpl{
		Zh: "正在配置 Hyper-V 驱动支持",
		En: "Configuring Hyper-V driver support",
	}

	LogTplForConfigKVMWith0Args = LangTpl{
		Zh: "正在配置 KVM 驱动支持",
		En: "Configuring KVM driver support",
	}

	LogTplForConfigXenWith0Args = LangTpl{
		Zh: "正在配置 Xen 驱动支持",
		En: "Configuring Xen driver support",
	}

	LogTplForConfigVmwareWith0Args = LangTpl{
		Zh: "正在配置 VMware 驱动支持",
		En: "Configuring VMware driver support",
	}

	LogTplForIncompatibleBootPCIWith2Args = LangTpl{
		Zh: "检测到不兼容的启动设备：%s（%s）",
		En: "Detected incompatible boot device: %s (%s)",
	}

	LogTplForIncompatibleNonBootPCIWith2Args = LangTpl{
		Zh: "检测到不兼容的非启动设备：%s（%s），请在系统启动后安装相应驱动。",
		En: "Detected incompatible non-boot device: %s (%s). Please install the appropriate driver after system startup.",
	}

	LogTplForMatchDriverWith1Args = LangTpl{
		Zh: "正在为硬件 %s 匹配驱动",
		En: "Matching driver for hardware %s",
	}

	LogTplForMatchDriverSuccessWith1Args = LangTpl{
		Zh: "已为硬件 %s 匹配到兼容驱动",
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
