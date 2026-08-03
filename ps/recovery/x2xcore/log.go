package x2xcore

import "time"

type LogLevel int

const (
	LogDebug LogLevel = iota
	LogInfo
	LogWarn
	LogError
)

type LogEntry struct {
	Time      time.Time
	Level     LogLevel
	MessageEn string
	MessageZh string
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

	LogTplForMountSystemWith0Args = LangTpl{
		Zh: "切换至离线系统环境",
		En: "Switching to offline system environment",
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

	LogTplForRepairEFIWith0Args = LangTpl{
		Zh: "修复 EFI 引导",
		En: "Repair EFI boot",
	}

	LogTplForRepairGrubWith0Args = LangTpl{
		Zh: "修复 GRUB 配置",
		En: "Repair GRUB configuration",
	}

	LogTplForRepairFstabWith0Args = LangTpl{
		Zh: "修复 fstab 配置",
		En: "Repair fstab configuration",
	}
)
