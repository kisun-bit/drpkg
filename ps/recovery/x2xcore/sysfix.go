package x2xcore

import (
	"path/filepath"
	"runtime"

	"github.com/kisun-bit/drpkg/define"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/ps/bus/pci/universal"
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

	//
	// 检查
	//

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
		if platform.Arch != runtime.GOARCH {
			return errors.New("FixerCreateOptions Arch is invalid")
		}
		if platform.Base != define.HPUnknown &&
			platform.Base != define.HPVirt &&
			platform.Base != define.HPBareMetal {
			return errors.New("FixerCreateOptions Base is invalid")
		}
		if platform.Virt != define.HPVTNone &&
			platform.Virt != define.HPVTVmware &&
			platform.Virt != define.HPVTKvm &&
			platform.Virt != define.HPVTXen &&
			platform.Virt != define.HPVTHyperV {
			return errors.New("FixerCreateOptions Virt is invalid")
		}
		if platform.Base == define.HPBareMetal &&
			len(platform.PciList) == 0 {
			return errors.New("FixerCreateOptions PciList is empty")
		}
	}

	if opts.RecoveryParam.X2xLibrary == "" {
		opts.RecoveryParam.X2xLibrary = filepath.Join(extend.ExecDir(), "library")
	}
	if !extend.IsExisted(opts.RecoveryParam.X2xLibrary) {
		return errors.Errorf("FixerCreateOptions X2XLibrary(%s) is empty", opts.RecoveryParam.X2xLibrary)
	}

	//
	// 修正
	//

	plats := []*Platform{&opts.RecoveryParam.Source, &opts.RecoveryParam.Target}
	for i := 0; i < len(plats); i++ {
		if plats[i].Base != "" {
			continue
		}
		plats[i].Base = define.HPBareMetal
		plats[i].Virt = define.HPVTNone
		for _, p := range plats[i].PciList {
			uniPci, err := universal.UniPciFromString(p)
			if err != nil {
				return err
			}
			if uniPci.VendorId() == 0x1af4 {
				plats[i].Base = define.HPVirt
				plats[i].Virt = define.HPVTKvm
				break
			}
			if uniPci.VendorId() == 0x5853 {
				plats[i].Base = define.HPVirt
				plats[i].Virt = define.HPVTXen
				break
			}
			if uniPci.VendorId() == 0x15ad {
				plats[i].Base = define.HPVirt
				plats[i].Virt = define.HPVTVmware
				break
			}
		}
	}

	return nil
}
