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
		Zh: "初始化 Rescue 救援环境",
		En: "Initializing Rescue environment",
	}

	LogTplForOfflineSystemReadyWith0Args = LangTpl{
		Zh: "枚举离线系统块设备",
		En: "Enumerating offline system block devices",
	}

	LogTplForResetWith0Args = LangTpl{
		Zh: "初始化 Device Mapper 环境",
		En: "Initializing Device Mapper environment",
	}

	LogTplForOpenLUKSWith0Args = LangTpl{
		Zh: "解锁 LUKS 加密卷",
		En: "Unlocking LUKS encrypted volumes",
	}

	LogTplForEnumFsWith0Args = LangTpl{
		Zh: "扫描文件系统",
		En: "Scanning filesystems",
	}

	LogTplForFsckFsWith0Args = LangTpl{
		Zh: "修复文件系统一致性",
		En: "Repairing filesystem consistency",
	}

	LogTplForCleanElastioSnapWith0Args = LangTpl{
		Zh: "清理残留块设备快照",
		En: "Cleaning up residual block device snapshots",
	}

	LogTplForCleanBackupMetadataWith1Args = LangTpl{
		Zh: "清理备份元数据目录：%s",
		En: "Cleaning up backup metadata directory: %s",
	}

	LogTplForSpecifySystemBootDeviceWith0Args = LangTpl{
		Zh: "识别系统启动设备",
		En: "Identifying system boot device",
	}

	LogTplForPrintSystemBootDeviceWith2Args = LangTpl{
		Zh: "已识别系统启动设备：%s（挂载点：%s）",
		En: "Detected system boot device: %s (mount point: %s)",
	}

	LogTplForBootableKernelWith1Args = LangTpl{
		Zh: "已识别可启动内核：%s",
		En: "Detected bootable kernel: %s",
	}

	LogTplForLoadRegistryWith0Args = LangTpl{
		Zh: "加载离线注册表",
		En: "Loading offline registry",
	}

	LogTplForUnloadRegistryWith0Args = LangTpl{
		Zh: "卸载离线注册表",
		En: "Unloading offline registry",
	}

	LogTplForMountSystemWith0Args = LangTpl{
		Zh: "切换至离线系统环境",
		En: "Switching to offline system environment",
	}

	LogTplForPrintControlSetWith1Args = LangTpl{
		Zh: "已识别当前系统控制集：ControlSet00%d",
		En: "Detected current system control set: ControlSet00%d",
	}

	LogTplForPrintDriverDatabaseLegacyWith0Args = LangTpl{
		Zh: "已识别系统驱动数据库：CDB",
		En: "Detected system driver database: CDB",
	}

	LogTplForPrintDriverDatabasePnpWith0Args = LangTpl{
		Zh: "已识别系统驱动数据库：PNP",
		En: "Detected system driver database: PNP",
	}

	LogTplForPrintSystemBootKernelWith1Args = LangTpl{
		Zh: "已识别系统启动内核：%s",
		En: "Detected system boot kernel: %s",
	}

	LogTplForPrintSystemGrubWith2Args = LangTpl{
		Zh: "已识别系统引导程序：%s（版本：v%v）",
		En: "Detected system bootloader: %s (version: v%v)",
	}

	LogTplForPrintSystemBootTypeWith1Args = LangTpl{
		Zh: "已识别系统启动模式：%s",
		En: "Detected system boot mode: %s",
	}

	LogTplForPrintDistroWith1Args = LangTpl{
		Zh: "已识别系统发行版：%s",
		En: "Detected system distribution: %s",
	}

	LogTplForPrintInitrdMgrWith1Args = LangTpl{
		Zh: "已识别 Initramfs 管理工具：%s",
		En: "Detected Initramfs management tool: %s",
	}

	LogTplForDisableSELinuxWith0Args = LangTpl{
		Zh: "禁用 SELinux",
		En: "Disabling SELinux",
	}

	LogTplForDisableAutoRebootWith0Args = LangTpl{
		Zh: "禁用系统故障自动重启",
		En: "Disabling automatic reboot on system failure",
	}

	LogTplForRepairPAMWith0Args = LangTpl{
		Zh: "修复 PAM 认证配置",
		En: "Repairing PAM authentication configuration",
	}

	LogTplForRepairGrubWith0Args = LangTpl{
		Zh: "修复 GRUB 启动配置",
		En: "Repairing GRUB boot configuration",
	}

	LogTplForRepairFstabWith0Args = LangTpl{
		Zh: "修复 fstab 挂载配置",
		En: "Repairing fstab mount configuration",
	}

	LogTplForInjectLegacyDriversWith0Args = LangTpl{
		Zh: "采用传统模式（CDB）注入虚拟化驱动",
		En: "Injecting virtualization drivers using legacy mode (CDB)",
	}

	LogTplForSkipFirstBootServiceWith1Args = LangTpl{
		Zh: "系统版本（%s）过旧，已跳过首次启动服务和网络配置部署",
		En: "System version (%s) is too old. Skipped first-boot service and network configuration deployment",
	}

	LogTplForNoLegacyBlockDriverWith1Args = LangTpl{
		Zh: "未找到与系统版本（%s）兼容的 VirtIO 启动驱动。恢复后请使用 IDE、Legacy NIC 等兼容硬件启动",
		En: "No VirtIO boot driver compatible with system version (%s) was found. Boot using compatible hardware such as IDE and Legacy NIC after recovery",
	}

	LogTplForNoLegacyVirtualDriverWith2Args = LangTpl{
		Zh: "未找到适用于系统版本（%s）的 KVM 虚拟化驱动（%v）。恢复后请使用 IDE、Legacy NIC 等兼容硬件启动",
		En: "No KVM virtualization driver compatible with system version (%s) was found (%v). Boot using compatible hardware such as IDE and Legacy NIC after recovery",
	}

	LogTplForIntelIdeNotAvailableWith1Args = LangTpl{
		Zh: "启用 IDE/ATA 启动驱动失败：%v。若系统启动盘采用 IDE/ATA 模式，恢复后可能无法正常启动",
		En: "Failed to enable IDE/ATA boot driver: %v. The system may fail to boot after recovery if the boot disk uses IDE/ATA mode",
	}

	LogTplForNonBootDriverInstalledWith2Args = LangTpl{
		Zh: "系统版本过旧，已将非启动驱动（%s）部署至驱动目录：%s。请在恢复后手动安装",
		En: "System version is too old. Non-boot driver (%s) has been deployed to driver directory: %s. Install it manually after recovery",
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
		Zh: "部署首次启动服务：%s",
		En: "Deploying first-boot service: %s",
	}

	LogTplForInjectNetworkToolFailedWith1Args = LangTpl{
		Zh: "网络配置工具部署失败：%v",
		En: "Failed to deploy network configuration tool: %v",
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
		En: "KVM driver support configuration completed",
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
		Zh: "检测到不兼容的非启动设备：%s（%s）。请在系统启动后安装对应驱动",
		En: "Detected incompatible non-boot device: %s (%s). Install the corresponding driver after system startup",
	}

	LogTplForMatchDriverWith1Args = LangTpl{
		Zh: "检查硬件兼容性：%s",
		En: "Checking hardware compatibility: %s",
	}

	LogTplForMatchDriverDbWith2Args = LangTpl{
		Zh: "已匹配硬件 %s 的兼容驱动：%s",
		En: "Matched compatible driver for hardware %s: %s",
	}

	LogTplForMatchDriverSuccessWith1Args = LangTpl{
		Zh: "硬件 %s 的兼容性修复完成",
		En: "Compatibility repair completed for hardware %s",
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
		En: "System repair completed",
	}

	LogTplForRepairFailedWith1Args = LangTpl{
		Zh: "系统修复失败：%v",
		En: "System repair failed: %v",
	}

	LogTplForUnsupportedHardwareWith2Args = LangTpl{
		Zh: "检测到不支持的硬件设备：%s（%s），该设备为非启动设备，无需执行兼容性修复",
		En: "Detected unsupported hardware device: %s (%s). The device is not a boot device, so compatibility repair is not required",
	}

	LogTplForHyperVLowVersionWith0Args = LangTpl{
		Zh: "当前 Linux 版本可能不兼容 Hyper-V。恢复后请使用 IDE、Legacy NIC 等兼容硬件启动",
		En: "The current Linux version may be incompatible with Hyper-V. Boot using compatible hardware such as IDE and Legacy NIC after recovery",
	}

	LogTplForKVMPatchFailedWith1Args = LangTpl{
		Zh: "内核 VirtIO 补丁失败：%v。恢复后请使用 IDE、Legacy NIC 等兼容硬件启动",
		En: "VirtIO kernel patch failed: %v. Boot using compatible hardware such as IDE and Legacy NIC after recovery",
	}

	LogTplForUdevNoUuidWith0Args = LangTpl{
		Zh: "udev 不支持 UUID，无法自动更新 grub.cfg 和 fstab，恢复后系统可能无法正常启动",
		En: "udev does not support UUID. grub.cfg and fstab cannot be updated automatically, and the system may fail to boot after recovery",
	}

	LogTplForFstabUnknownLineWith1Args = LangTpl{
		Zh: "fstab 存在无法识别的配置项：%s，恢复后系统可能无法正常启动",
		En: "Unrecognized fstab configuration entry: %s. The system may fail to boot after recovery",
	}

	LogTplForNoEfiFirmwareWith0Args = LangTpl{
		Zh: "未找到 EFI 固件启动项，启动后需在 UEFI Shell 中手动选择 EFI 文件",
		En: "No EFI firmware boot entry found. Manually select the EFI file in UEFI Shell after boot",
	}

	LogTplForXenPatchFailedWith1Args = LangTpl{
		Zh: "内核 Xen 补丁失败：%v。恢复后请使用 IDE、Legacy NIC 等兼容硬件启动",
		En: "Xen kernel patch failed: %v. Boot using compatible hardware such as IDE and Legacy NIC after recovery",
	}

	LogTplForVmwarePatchFailedWith1Args = LangTpl{
		Zh: "内核 VMware 补丁失败：%v。恢复后请使用 IDE、Legacy NIC 等兼容硬件启动",
		En: "VMware kernel patch failed: %v. Boot using compatible hardware such as IDE and Legacy NIC after recovery",
	}

	LogTplForMdraidModuleMissingWith0Args = LangTpl{
		Zh: "离线系统缺少 mdraid 模块，无法组装软件 RAID。恢复时请将存储控制器设置为非 RAID 模式（ATA/IDE 或 AHCI）",
		En: "The offline system is missing the mdraid module and cannot assemble software RAID. Set the storage controller to non-RAID mode (ATA/IDE or AHCI) during recovery",
	}

	LogTplForNestedRaidWith0Args = LangTpl{
		Zh: "检测到多重 RAID：源磁盘带 RAID 签名，且目标环境再次配置了 RAID，系统将无法启动。请取消目标环境的 RAID 配置或调整恢复参数",
		En: "Nested RAID detected: the source disk carries a RAID signature while the target environment is configured with RAID again. The system will fail to boot. Remove the target RAID configuration or adjust recovery parameters",
	}
)
