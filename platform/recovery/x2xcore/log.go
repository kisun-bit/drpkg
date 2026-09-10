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
		Zh: "识别离线系统磁盘",
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
		Zh: "扫描文件系统",
		En: "Scanning filesystems",
	}

	LogTplForFsckFsWith0Args = LangTpl{
		Zh: "修复文件系统",
		En: "Repairing filesystems",
	}

	LogTplForCleanElastioSnapWith0Args = LangTpl{
		Zh: "清理残留的 Elastio/Datto 快照",
		En: "Cleaning up leftover Elastio/Datto snapshots",
	}

	LogTplForCleanBackupMetadataWith1Args = LangTpl{
		Zh: "清理元数据目录：%s",
		En: "Cleaning up metadata directory: %s",
	}

	LogTplForSpecifySystemBootDeviceWith0Args = LangTpl{
		Zh: "识别系统启动设备",
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
		En: "System distribution: %s",
	}

	LogTplForPrintInitrdMgrWith1Args = LangTpl{
		Zh: "Initramfs 管理工具：%s",
		En: "Initramfs management tool: %s",
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
		Zh: "修复 PAM 配置",
		En: "Repairing PAM configuration",
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
		Zh: "系统版本（%s）过旧，已跳过硬件修复和网络配置注入，请恢复后使用 IDE、Legacy NIC 等兼容硬件启动",
		En: "System version (%s) is too old. Hardware repair and network configuration injection were skipped. After recovery, boot using compatible hardware such as IDE and Legacy NIC.",
	}

	LogTplForInjectLegacyDriversWith0Args = LangTpl{
		Zh: "以传统方式（CDB）注入虚拟化驱动",
		En: "Injecting virtualization drivers using the legacy method (CDB)",
	}

	LogTplForSkipFirstBootServiceWith1Args = LangTpl{
		Zh: "系统版本（%s）过旧，已跳过首次启动服务和网络配置注入",
		En: "System version (%s) is too old. First-boot service and network configuration injection were skipped.",
	}

	LogTplForNoLegacyBlockDriverWith1Args = LangTpl{
		Zh: "未找到适用于系统版本（%s）的 VirtIO 块设备启动驱动，请恢复后使用 IDE、Legacy NIC 等兼容硬件启动",
		En: "No VirtIO block boot driver is available for system version (%s). After recovery, boot using compatible hardware such as IDE and Legacy NIC.",
	}

	LogTplForNoLegacyVirtualDriverWith2Args = LangTpl{
		Zh: "未找到适用于系统版本（%s）的 KVM 虚拟化驱动（%v），请恢复后使用 IDE、Legacy NIC 等兼容硬件启动",
		En: "No KVM virtualization driver is available for system version (%s) (%v). After recovery, boot using compatible hardware such as IDE and Legacy NIC.",
	}

	LogTplForIntelIdeNotAvailableWith1Args = LangTpl{
		Zh: "确保 Intel IDE 引导驱动失败：%v。若系统使用 Intel IDE 启动盘，恢复后可能无法正常启动",
		En: "Failed to ensure the Intel IDE boot driver is available: %v. If the system boots from an Intel IDE disk, it may fail to boot after recovery.",
	}

	LogTplForNonBootDriverInstalledWith2Args = LangTpl{
		Zh: "系统版本过旧，非启动驱动（%s）已放入驱动目录 %s，请在恢复后进入该目录手动安装驱动",
		En: "System version is too old. Non-boot driver (%s) was placed in driver directory %s. Install the driver manually from this directory after recovery.",
	}

	LogTplForOptimizeUEFIWith0Args = LangTpl{
		Zh: "优化 UEFI 启动配置",
		En: "Optimizing UEFI boot configuration",
	}

	LogTplForOptimizeBCDWith0Args = LangTpl{
		Zh: "优化 BCD 启动配置",
		En: "Optimizing BCD boot configuration",
	}

	LogTplForInjectFirstBootServiceWith1Args = LangTpl{
		Zh: "注入首次启动服务：%s",
		En: "Injecting first-boot service: %s",
	}

	LogTplForInjectNetworkToolFailedWith1Args = LangTpl{
		Zh: "网络配置工具注入失败：%v",
		En: "Failed to inject network configuration tool: %v",
	}

	LogTplForInjectNetworkConfigWith0Args = LangTpl{
		Zh: "写入网络配置",
		En: "Writing network configuration",
	}

	LogTplForInjectNetworkConfigFailedWith1Args = LangTpl{
		Zh: "网络配置写入失败：%v",
		En: "Failed to write network configuration: %v",
	}

	LogTplForUnconfigHVWith0Args = LangTpl{
		Zh: "解除 Hyper-V 驱动绑定",
		En: "Removing Hyper-V driver bindings",
	}

	LogTplForUnconfigKVMWith0Args = LangTpl{
		Zh: "解除 KVM 驱动绑定",
		En: "Removing KVM driver bindings",
	}

	LogTplForUnconfigXenWith0Args = LangTpl{
		Zh: "解除 Xen 驱动绑定",
		En: "Removing Xen driver bindings",
	}

	LogTplForUnconfigVmwareWith0Args = LangTpl{
		Zh: "解除 VMware 驱动绑定",
		En: "Removing VMware driver bindings",
	}

	LogTplForConfigHVWith0Args = LangTpl{
		Zh: "配置 Hyper-V 驱动支持",
		En: "Configuring Hyper-V driver support",
	}

	LogTplForConfigKVMWith0Args = LangTpl{
		Zh: "配置 KVM 驱动支持",
		En: "Configuring KVM driver support",
	}

	LogTplForKVMDriverDbWith1Args = LangTpl{
		Zh: "已匹配 KVM 兼容驱动：%s",
		En: "Matched KVM-compatible driver: %s",
	}

	LogTplForConfigKVMSuccessWith0Args = LangTpl{
		Zh: "KVM 驱动支持配置完成",
		En: "KVM driver support configured successfully",
	}

	LogTplForConfigXenWith0Args = LangTpl{
		Zh: "配置 Xen 驱动支持",
		En: "Configuring Xen driver support",
	}

	LogTplForConfigVmwareWith0Args = LangTpl{
		Zh: "配置 VMware 驱动支持",
		En: "Configuring VMware driver support",
	}

	LogTplForIncompatibleBootPCIWith2Args = LangTpl{
		Zh: "检测到不兼容的启动设备：%s（%s）",
		En: "Detected incompatible boot device: %s (%s)",
	}

	LogTplForIncompatibleNonBootPCIWith2Args = LangTpl{
		Zh: "检测到不兼容的非启动设备：%s（%s），请在系统启动后安装相应驱动",
		En: "Detected incompatible non-boot device: %s (%s). Install the appropriate driver after system startup.",
	}

	LogTplForMatchDriverWith1Args = LangTpl{
		Zh: "为硬件 %s 进行兼容性检查",
		En: "Checking compatibility for hardware %s",
	}

	LogTplForMatchDriverDbWith2Args = LangTpl{
		Zh: "硬件 %s 的兼容驱动：%s",
		En: "Compatible driver for hardware %s: %s",
	}

	LogTplForMatchDriverSuccessWith1Args = LangTpl{
		Zh: "硬件 %s 已完成兼容性修复",
		En: "Compatibility fix completed for hardware %s",
	}

	LogTplForUnlockBitlockerWith1Args = LangTpl{
		Zh: "解锁卷 %s 的 BitLocker",
		En: "Unlocking BitLocker on volume %s",
	}

	LogTplForUnlockBitlockerFailedWith2Args = LangTpl{
		Zh: "卷 %s 的 BitLocker 解锁失败：%v",
		En: "Failed to unlock BitLocker on volume %s: %v",
	}

	LogTplForRepairSuccessWith0Args = LangTpl{
		Zh: "系统修复完成",
		En: "System repair completed successfully",
	}

	LogTplForRepairFailedWith1Args = LangTpl{
		Zh: "系统修复失败，原因：%v",
		En: "System repair failed: %v",
	}

	LogTplForUnsupportedHardwareWith2Args = LangTpl{
		Zh: "不支持的硬件设备：%s（%s），该设备为非启动设备，无需进行兼容性修复",
		En: "Unsupported hardware device: %s (%s). The device is not a boot device, so compatibility repair is not required.",
	}

	LogTplForHyperVLowVersionWith0Args = LangTpl{
		Zh: "当前 Linux 版本可能不兼容 Hyper-V，请恢复后使用 IDE、Legacy NIC 等兼容硬件启动",
		En: "The current Linux version may be incompatible with Hyper-V. After recovery, boot using compatible hardware such as IDE and Legacy NIC.",
	}

	LogTplForKVMPatchFailedWith1Args = LangTpl{
		Zh: "内核 VirtIO 补丁失败：%v，请恢复后使用 IDE、Legacy NIC 等兼容硬件启动",
		En: "Kernel VirtIO patch failed: %v. After recovery, boot using compatible hardware such as IDE and Legacy NIC.",
	}

	LogTplForUdevNoUuidWith0Args = LangTpl{
		Zh: "udev 不支持 UUID，grub.cfg 和 fstab 无法自动更新，恢复后系统可能无法正常启动",
		En: "udev does not support UUID. grub.cfg and fstab cannot be updated automatically, and the system may fail to boot after recovery.",
	}

	LogTplForFstabUnknownLineWith1Args = LangTpl{
		Zh: "fstab 存在无法识别的配置行：%s，恢复后系统可能无法正常启动",
		En: "Unrecognized fstab configuration line: %s. The system may fail to boot after recovery.",
	}

	LogTplForNoEfiFirmwareWith0Args = LangTpl{
		Zh: "未找到 EFI 固件入口，启动后需在 UEFI Shell 中手动选择 EFI 文件",
		En: "No EFI firmware entry found. Manually select the EFI file in UEFI Shell after boot.",
	}

	LogTplForXenPatchFailedWith1Args = LangTpl{
		Zh: "内核 Xen 补丁失败：%v，请恢复后使用 IDE、Legacy NIC 等兼容硬件启动",
		En: "Kernel Xen patch failed: %v. After recovery, boot using compatible hardware such as IDE and Legacy NIC.",
	}
)
