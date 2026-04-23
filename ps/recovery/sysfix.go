package recovery

import "time"

type SystemFixer interface {

	//
	// 资源管理
	//

	// Initialize 初始化
	// linux: 挂载最小系统，如root、boot、efi等设备，收集离线系统信息
	// windows: 挂载离线系统的SYSTEM注册表
	Initialize() error

	// Release 释放资源
	Release() error

	//
	// 修复管理
	//

	//
	// 驱动管理
	//

	// AddDrivers 注入驱动
	AddDrivers(kernel string, drivers []string) error

	// RemoveDrivers 卸载驱动
	RemoveDrivers(kernel string, drivers []string) error

	//
	// 工具
	//

	// ExecuteInPe 离线环境下执行命令
	ExecuteInPe(cmdline string, timeout time.Duration) (exit int, output string, err error)

	// Kernels 获取离线环境的内核集合
	Kernels() ([]string, error)

	// Logs 获取修复日志
	Logs() chan string
}
