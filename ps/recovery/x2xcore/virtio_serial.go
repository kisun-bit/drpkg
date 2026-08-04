package x2xcore

// 定义修复虚拟机中 Host 与 Guest 之间的通信消息类型。
// 用于控制修复流程、传递执行日志以及同步执行结果。
const (
	// SerialMessageTypeGuestReady:
	// Guest 启动完成，通知 Host 当前修复环境已就绪。
	SerialMessageTypeGuestReady = iota

	// SerialMessageTypeStartRepair:
	// Host 通知 Guest 开始执行修复任务。
	SerialMessageTypeStartRepair

	// SerialMessageTypeRepairLog:
	// Guest 向 Host 返回修复过程日志。
	SerialMessageTypeRepairLog

	// SerialMessageTypeRepairResult:
	// Guest 向 Host 返回修复任务最终执行结果。
	SerialMessageTypeRepairResult
)
