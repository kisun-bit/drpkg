package x2xcore

import (
	"context"

	"github.com/kisun-bit/drpkg/ps/recovery/x2xlib"
)

// 关闭arp探测

type windowsSystemFixer struct {
	ctx    context.Context
	opts   *FixerCreateOptions // 恢复参数
	logs   <-chan LogEntry     // 日志缓存通道
	x2xLib *x2xlib.X2XLib      // 驱动库
	offsys offlineSystem       // 离线系统的私有信息
}

type offlineSystem struct {
}
