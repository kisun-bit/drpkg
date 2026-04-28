package recovery

import (
	"runtime"

	"github.com/kisun-bit/drpkg/extend"
	"github.com/pkg/errors"
)

// SysFixer 系统修复器
type SysFixer interface {
	// Prepare 准备修复环境（挂载/加载离线系统）
	Prepare() error

	// Repair 执行修复流程
	Repair() error

	// Cleanup 清理修复环境（卸载/释放资源）
	Cleanup() error

	// GetLog 获取日志
	GetLog() (LogEntry, bool)
}

type FixerCreateOptions struct {
	// OfflineSysDisks 离线系统磁盘集合
	OfflineSysDisks []string

	// RecoveryParam 恢复参数
	RecoveryParam RecoveryParameter
}

func CheckFixerCreateOptions(opts *FixerCreateOptions) error {
	if opts == nil {
		return errors.New("FixerCreateOptions is nil")
	}
	if len(opts.OfflineSysDisks) == 0 {
		return errors.New("FixerCreateOptions OfflineSysDisks is empty")
	}
	for _, disk := range opts.OfflineSysDisks {
		if !extend.IsExisted(disk) {
			return errors.Errorf("FixerCreateOptions disk(%s) does not exist", disk)
		}
	}
	for _, platform := range []Platform{opts.RecoveryParam.Source, opts.RecoveryParam.Target} {
		if platform.BootMode != BootModeUEFI &&
			platform.BootMode != BootModeBIOS {
			return errors.New("FixerCreateOptions BootMode is invalid")
		}
		if platform.Arch != runtime.GOARCH {
			return errors.New("FixerCreateOptions Arch is invalid")
		}
		if platform.Base != HPUnknown &&
			platform.Base != HPVirt &&
			platform.Base != HPBareMetal {
			return errors.New("FixerCreateOptions Base is invalid")
		}
		if platform.Virt != HPVTNone &&
			platform.Virt != HPVTVmware &&
			platform.Virt != HPVTQemuKvm &&
			platform.Virt != HPVTXen &&
			platform.Virt != HPVTHyperV {
			return errors.New("FixerCreateOptions Virt is invalid")
		}
		if platform.Base == HPBareMetal &&
			len(platform.PciList) == 0 {
			return errors.New("FixerCreateOptions PciList is empty")
		}
	}
	return nil
}
