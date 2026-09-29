package repairvm

import "github.com/kisun-bit/drpkg/platform/recovery/x2xcore"

var (
	LogTplReceiveRepairRequest = x2xcore.LangTpl{
		Zh: "已接收系统修复请求",
		En: "System repair request received",
	}

	LogTplRepairRequestDetails = x2xcore.LangTpl{
		Zh: "修复请求详情：磁盘数量：%d，CPU 架构：%s，系统类型：%s，强制文件系统修复：%v，源硬件平台：%s，目标硬件平台：%s",
		En: "Repair request details: disk count: %d, CPU architecture: %s, force filesystem repair: %v, source hardware platform: %s, target hardware platform: %s",
	}

	LogTplCreateRepairVM = x2xcore.LangTpl{
		Zh: "正在创建 Rescue 救援环境",
		En: "Creating Rescue Environment",
	}

	LogTplReleaseRepairVM = x2xcore.LangTpl{
		Zh: "正在释放 Rescue 救援环境资源",
		En: "Releasing Rescue Environment resources",
	}

	LogTplWaitRepairVMReady = x2xcore.LangTpl{
		Zh: "正在等待 Rescue 救援环境启动并完成修复服务初始化",
		En: "Waiting for Rescue Environment to boot and initialize the repair service",
	}

	LogTplCreateCommunicationChannel = x2xcore.LangTpl{
		Zh: "正在建立主机与 Rescue 救援环境之间的双向通信通道",
		En: "Establishing a bidirectional communication channel with the Rescue Environment",
	}

	LogTplSendRepairRequest = x2xcore.LangTpl{
		Zh: "正在向 Rescue 救援环境下发系统修复请求",
		En: "Sending the system repair request to the Rescue Environment",
	}
)
