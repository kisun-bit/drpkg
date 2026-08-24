package repairvm

import "github.com/kisun-bit/drpkg/ps/recovery/x2xcore"

var (
	LogTplReceiveRepairRequest = x2xcore.LangTpl{
		Zh: "已接收用户发起的系统修复请求",
		En: "System repair request received",
	}

	LogTplRepairRequestDetails = x2xcore.LangTpl{
		Zh: "修复请求详情：磁盘数量：%d，CPU架构：%s，系统类型：%s，强制修复文件系统：%v，源硬件平台：%s，目标硬件平台：%s",
		En: "Repair request details: disk count: %d, CPU architecture: %s, system type: %s, force filesystem repair: %v, source hardware platform: %s, target hardware platform: %s",
	}

	LogTplCreateRepairVM = x2xcore.LangTpl{
		Zh: "正在创建临时修复虚拟机",
		En: "Creating temporary repair virtual machine",
	}

	LogTplReleaseRepairVM = x2xcore.LangTpl{
		Zh: "正在释放临时修复虚拟机资源",
		En: "Releasing temporary repair virtual machine resources",
	}

	LogTplWaitRepairVMReady = x2xcore.LangTpl{
		Zh: "正在等待修复虚拟机启动并完成修复服务初始化",
		En: "Waiting for the repair virtual machine to start and initialize the repair service",
	}

	LogTplCreateCommunicationChannel = x2xcore.LangTpl{
		Zh: "正在建立并监听主机与修复虚拟机之间的双向通信通道",
		En: "Establishing and listening on the bidirectional communication channel between the host and repair virtual machine",
	}

	LogTplSendRepairRequest = x2xcore.LangTpl{
		Zh: "正在向修复虚拟机发送系统修复请求",
		En: "Sending the system repair request to the repair virtual machine",
	}
)
