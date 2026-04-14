package recovery

import (
	"strings"

	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/ps/info"
)

// SysContextType 表示系统所处的运行上下文
type SysContextType string

const (
	SysContextOnline  SysContextType = "online_os"     // 当前运行系统
	SysContextLiveOS  SysContextType = "online_liveos" // WinPE/LiveCD 环境
	SysContextOffline SysContextType = "offline_os"    // 离线系统（未启动）
)

type SysContext struct {
	Type    SysContextType
	RootDir string
}

// SysContextTypeFromRootDir 基于系统路径，获取系统的运行上下文
func SysContextTypeFromRootDir(rootDir string) SysContextType {
	if strings.HasSuffix(rootDir, ":\\") {
		rootDir = strings.ToUpper(strings.TrimSuffix(rootDir, "\\"))
	}

	sysRoot := extend.GetSystemRoot()

	if sysRoot == rootDir {
		if info.IsMemoryOS() {
			return SysContextLiveOS
		}
		return SysContextOnline
	}

	return SysContextOffline
}

//
// 无代理恢复，从指定的物理磁盘中加载离线Linux/Windows系统（基础环境：内置KVM修复环境）
// 使用者：跨平台级恢复、异构恢复、接管
//

//
// 有代理恢复，加载LiveCD系统
// 使用者：驱动注入，异构恢复
//
