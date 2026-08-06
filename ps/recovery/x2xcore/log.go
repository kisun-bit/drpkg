package x2xcore

import "fmt"

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

type LangTpl struct {
	Zh string
	En string
}

var (
	LogTplForReadyWith0Args = LangTpl{
		Zh: "加载异构修复环境",
		En: "Heterogeneous recovery environment loaded",
	}

	LogTplForOfflineSystemReadyWith0Args = LangTpl{
		Zh: "识别离线系统的磁盘集",
		En: "Offline system disk set identified",
	}

	LogTplForResetWith0Args = LangTpl{
		Zh: "重置存储映射环境",
		En: "Resetting storage mapping environment",
	}

	LogTplForOpenLUKSWith0Args = LangTpl{
		Zh: "打开 LUKS 加密卷",
		En: "Opening LUKS encrypted volume",
	}

	LogTplForEnumFsWith0Args = LangTpl{
		Zh: "扫描文件系统设备",
		En: "Scanning filesystem devices",
	}

	LogTplForFsckFsWith0Args = LangTpl{
		Zh: "修复文件系统设备",
		En: "fscking filesystem devices",
	}

	LogTplForCleanElastioSnapWith0Args = LangTpl{
		Zh: "清理可能残留的 Elastio/Datto 快照",
		En: "Clean up potential residual Elastio/Datto snapshots",
	}

	LogTplForCleanSpecifiedPathWith1Args = LangTpl{
		Zh: "清理文件（/文件夹）：%s",
		En: "Clean up file (/folder): %s",
	}

	LogTplForSpecifySystemBootDeviceWith0Args = LangTpl{
		Zh: "确定系统启动设备",
		En: "Determining system boot device",
	}

	LogTplForPrintSystemBootDeviceWith2Args = LangTpl{
		Zh: "系统启动设备：%s（挂载点：%s）",
		En: "System boot device: %s (mount point: %s)",
	}

	LogTplForLoadRegistryWith0Args = LangTpl{
		Zh: "导入注册表",
		En: "Loading registry",
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
		Zh: "系统驱动数据库类型：CriticalDeviceDatabase",
		En: "System driver database type: CriticalDeviceDatabase",
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
		En: "Disable SELinux",
	}

	LogTplForRepairPAMWith0Args = LangTpl{
		Zh: "修复 PAM 模块",
		En: "Repair PAM module",
	}

	LogTplForRepairGrubWith0Args = LangTpl{
		Zh: "修复 GRUB 配置",
		En: "Repair GRUB configuration",
	}

	LogTplForRepairFstabWith0Args = LangTpl{
		Zh: "修复 fstab 配置",
		En: "Repair fstab configuration",
	}

	LogTplForIgnoreRepairWith1Args = LangTpl{
		Zh: "系统版本（%s）过旧，跳过硬件兼容性修复。恢复后请使用兼容硬件配置（如 IDE 控制器、低版本主板型号）尝试启动",
		En: "System version (%s) is too old. Hardware compatibility repair is skipped. After recovery, try booting with compatible hardware settings (such as IDE controller and legacy machine type).",
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

	LogTplForInjectNetworkConfigWith0Args = LangTpl{
		Zh: "写入系统网络配置",
		En: "Writing system network configuration",
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
		Zh: "检测到不兼容的非启动设备：%s（%s）",
		En: "Detected incompatible non-boot device: %s (%s)",
	}
)
