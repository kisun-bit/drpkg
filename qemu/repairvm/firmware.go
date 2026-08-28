//go:build linux

package repairvm

import (
	"fmt"
	"path/filepath"

	"github.com/kisun-bit/drpkg/define"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/pkg/errors"
)

// FirmwareSpec 是可由用户配置的 UEFI 固件描述。
//
// 对应 libvirt 的 loader/nvram 概念：
//   - Code         相当于 <loader>，只读固件代码镜像，
//     如 /usr/share/AAVMF/AAVMF_CODE.fd
//   - VarsTemplate 相当于 <nvram template='...'>，只读变量区模板，
//     如 /usr/share/AAVMF/AAVMF_VARS.fd
//
// 注意：不应配置类似 /var/lib/libvirt/qemu/nvram/<vm>_VARS.fd 的实例变量文件。
// 该文件是 libvirt 从模板拷贝出的实例私有副本，本包按同样的机制自动生成：
// 每次创建修复虚拟机时，VarsTemplate 会被拷贝到实例私有目录作为可写变量区，
// 生命周期与修复虚拟机一致，结束时随缓存目录一并清理。
type FirmwareSpec struct {
	// Code 只读固件代码镜像路径（pflash，readonly=on）。
	Code string `json:"code"`

	// VarsTemplate 只读 UEFI 变量区模板路径。
	VarsTemplate string `json:"varsTemplate"`
}

// firmwareConfig 是单个修复虚拟机实例解析后的固件配置。
type firmwareConfig struct {
	// Code 只读固件代码镜像路径。
	Code string

	// Vars 实例私有的可写变量区文件（由 VarsTemplate 拷贝而来）。
	Vars string
}

// addArgs 追加 UEFI 启动所需的 pflash 参数。
func (fc *firmwareConfig) addArgs(args []string) []string {
	if fc == nil {
		return args
	}
	return append(args,
		"-drive", fmt.Sprintf("if=pflash,format=raw,readonly=on,file=%s", fc.Code),
		"-drive", fmt.Sprintf("if=pflash,format=raw,file=%s", fc.Vars),
	)
}

// resolveFirmware 依据架构、启动模式与用户配置解析 UEFI 固件。
//
// 规则：
//   - arm64：只能通过 UEFI 启动，始终解析 UEFI 固件。
//   - amd64：默认沿用 BIOS 启动；仅当显式要求 UEFI（BootMode=uefi 或
//     配置了 Firmware）时才解析 UEFI 固件。
//
// 未显式配置固件时，按架构自动探测发行版默认固件（arm64: AAVMF/EDK2，
// amd64: OVMF）；探测失败则报错。
//
// 返回 nil 表示使用 BIOS 启动（不追加 pflash 参数）。
func resolveFirmware(arch string, bootMode define.BootMode, spec FirmwareSpec, cacheDir string) (*firmwareConfig, error) {
	uefiRequired := arch == "arm64" ||
		bootMode == define.BootModeUEFI ||
		spec.Code != "" || spec.VarsTemplate != ""

	if !uefiRequired {
		return nil, nil // BIOS
	}

	code, varsTemplate := spec.Code, spec.VarsTemplate
	if code == "" || varsTemplate == "" {
		probed, ok := probeFirmware(arch)
		if !ok {
			return nil, errors.Errorf(
				"%s requires UEFI firmware, but none was found and no explicit "+
					"firmware is configured",
				arch,
			)
		}
		code, varsTemplate = probed.Code, probed.VarsTemplate
	}

	return buildFirmwareConfig(code, varsTemplate, cacheDir)
}

// buildFirmwareConfig 校验固件路径并生成实例私有的可写变量区文件。
//
// 与 libvirt 的 nvram 处理一致：绝不直接挂载共享的只读模板，而是拷贝一份
// 实例私有副本。直接复用模板会污染模板本身，并在并发运行时相互冲突。
func buildFirmwareConfig(code, varsTemplate, cacheDir string) (*firmwareConfig, error) {
	if !extend.IsExisted(code) {
		return nil, errors.Errorf("firmware code not found: %s", code)
	}
	if !extend.IsExisted(varsTemplate) {
		return nil, errors.Errorf("firmware vars template not found: %s", varsTemplate)
	}

	vars := filepath.Join(cacheDir, filepath.Base(varsTemplate))
	if err := extend.CopyFile(varsTemplate, vars, 0666); err != nil {
		return nil, errors.Wrapf(err, "copy firmware vars template %s", varsTemplate)
	}

	logger.Debugf(
		"resolveFirmware: code=%s vars=%s (template=%s)",
		code, vars, varsTemplate,
	)

	return &firmwareConfig{Code: code, Vars: vars}, nil
}

// probeFirmware 探测常见发行版指定架构的 UEFI 固件位置。
func probeFirmware(arch string) (FirmwareSpec, bool) {
	var candidates []FirmwareSpec

	switch arch {
	case "arm64":
		candidates = []FirmwareSpec{
			// Debian / Ubuntu (aavmf package)
			{
				Code:         "/usr/share/AAVMF/AAVMF_CODE.fd",
				VarsTemplate: "/usr/share/AAVMF/AAVMF_VARS.fd",
			},
			// Fedora / RHEL (edk2-aarch64 package)
			{
				Code:         "/usr/share/edk2/aarch64/QEMU_EFI-pflash.raw",
				VarsTemplate: "/usr/share/edk2/aarch64/vars-template-pflash.raw",
			},
		}
	case "amd64":
		candidates = []FirmwareSpec{
			// Debian / Ubuntu (ovmf package)
			{
				Code:         "/usr/share/OVMF/OVMF_CODE.fd",
				VarsTemplate: "/usr/share/OVMF/OVMF_VARS.fd",
			},
			// Fedora / RHEL (edk2-ovmf package)
			{
				Code:         "/usr/share/edk2/x64/OVMF_CODE.secboot.fd",
				VarsTemplate: "/usr/share/edk2/x64/OVMF_VARS.fd",
			},
		}
	default:
		return FirmwareSpec{}, false
	}

	for _, c := range candidates {
		if extend.IsExisted(c.Code) && extend.IsExisted(c.VarsTemplate) {
			return c, true
		}
	}

	return FirmwareSpec{}, false
}
