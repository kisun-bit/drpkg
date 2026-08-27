//go:build linux

package nbd

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/kisun-bit/drpkg/command"
)

// ConnectOptions 描述一次设备连接的参数。
//
// Backend（本地磁盘镜像文件）与 Remote（远端 NBD 服务）二选一；
// 同时指定时优先使用 Backend。
type ConnectOptions struct {
	// Backend 是本地磁盘镜像文件路径（raw/qcow2 等 qemu 支持的格式）。
	Backend string

	// Remote 是远端 NBD 服务地址，格式 host[:port]（默认端口 10809）。
	Remote string

	// Format 是镜像格式（如 raw、qcow2）；为空时由 qemu-nbd 自动探测。
	// 出于安全考虑，禁止使用 host_device / host_cdrom 直通宿主设备。
	Format string

	// ReadOnly 以只读方式连接（--read-only）。
	ReadOnly bool

	// Cache 设置缓存策略：none / writeback / writethrough；为空使用默认。
	Cache string

	// Discard 设置 discard 策略：ignore / unmap；为空使用默认。
	Discard string

	// ExtraArgs 是透传给 qemu-nbd 的额外参数（插在后端路径之前）。
	ExtraArgs []string
}

// buildConnectCmd 构造 qemu-nbd 连接命令行。
// 后端路径使用单引号包裹以兼容空格（与仓库内 qemublk 包风格一致）。
func buildConnectCmd(tool string, index int, opt *ConnectOptions) string {
	var sb strings.Builder

	sb.WriteString(tool)
	sb.WriteString(" --connect=")
	sb.WriteString(DevicePathPrefix)
	sb.WriteString(strconv.Itoa(index))

	if opt.Format != "" {
		sb.WriteString(" --format=")
		sb.WriteString(opt.Format)
	}

	if opt.ReadOnly {
		sb.WriteString(" --read-only")
	}

	if opt.Cache != "" {
		sb.WriteString(" --cache=")
		sb.WriteString(opt.Cache)
	}

	if opt.Discard != "" {
		sb.WriteString(" --discard=")
		sb.WriteString(opt.Discard)
	}

	for _, arg := range opt.ExtraArgs {
		sb.WriteString(" ")
		sb.WriteString(arg)
	}

	if opt.Backend != "" {
		sb.WriteString(fmt.Sprintf(" '%s'", opt.Backend))
	} else {
		// 远端模式：qemu-nbd -C /dev/nbdN -- host[:port]
		sb.WriteString(" -- ")
		sb.WriteString(opt.Remote)
	}

	return sb.String()
}

// commandExecute 通过项目统一的命令执行器运行外部命令（Linux 下经
// `sh -c` 解释），保持与仓库其他工具封装一致的错误包装行为。
func commandExecute(ctx context.Context, cmdline string) (int, string, error) {
	return command.ExecuteWithContext(ctx, cmdline)
}
