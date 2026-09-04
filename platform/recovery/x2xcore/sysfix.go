package x2xcore

import (
	"fmt"

	"github.com/kisun-bit/drpkg/defs"
	"github.com/kisun-bit/drpkg/xutil"
	"github.com/kisun-bit/drpkg/platform/bus/pci/universal"
	"github.com/pkg/errors"
)

// SysFixer 系统修复器
type SysFixer interface {
	// Prepare 准备修复环境（挂载/加载离线系统）
	Prepare() error

	// Repair 执行修复流程
	Repair() error

	// CustomProcess 自定义的流程
	CustomProcess(func() error) error

	// Cleanup 清理修复环境（卸载/释放资源）
	Cleanup() error

	// GetLog 获取日志
	GetLog() (LogEntry, bool)

	// GetPreferHostConfig 获取推荐配置
	GetPreferHostConfig(defs.HPVirtType) (PreferConfig, error)
}

type FixerCreateOptions struct {
	// OfflineSysDisks 离线系统磁盘设备列表。
	// 表示离线系统磁盘在修复虚拟机内部对应的设备路径，例如 /dev/sdb、/dev/sdc 等。
	OfflineSysDisks []string `json:"offlineSysDisks"`

	// RecoveryParam 恢复参数
	RecoveryParam RecoveryParameter `json:"recoveryParam"`

	// InRepairVM 标识当前程序是否运行在 QEMU 动态启动的修复虚拟机中。
	InRepairVM bool `json:"inRepairVM"`
}

type PreferConfig struct {
	Chipset     string `json:"chipset"`     // 芯片组
	Video       string `json:"video"`       // 显卡类型
	DiskBus     string `json:"diskBus"`     // 磁盘总线
	NetworkType string `json:"networkType"` // 网卡类型

	// TODO 更多
}

func CheckAndFillFixerCreateOptions(opts *FixerCreateOptions) error {

	if opts == nil {
		return errors.New("FixerCreateOptions is nil")
	}
	if len(opts.OfflineSysDisks) == 0 {
		return errors.New("FixerCreateOptions OfflineSysDisks is empty")
	}
	for _, disk := range opts.OfflineSysDisks {
		if !xutil.IsExisted(disk) {
			return errors.Errorf("FixerCreateOptions disk(%s) does not exist", disk)
		}
	}

	return CheckAndFillRecoveryParameter(&opts.RecoveryParam)
}

func CheckAndFillRecoveryParameter(rp *RecoveryParameter) error {

	if rp.Source.Arch != rp.Target.Arch {
		return errors.Errorf("source and target are not same")
	}

	if rp.OSType != "windows" && rp.OSType != "linux" {
		return errors.Errorf("unsupported os: %s", rp.OSType)
	}

	if rp.Source.Virt == "" {
		rp.Source.Virt = defs.HPVTNone
	}
	if rp.Target.Virt == "" {
		rp.Target.Virt = defs.HPVTNone
	}

	for _, platform := range []Platform{rp.Source, rp.Target} {
		//if platform.Arch != runtime.GOARCH {
		//	return errors.New("FixerCreateOptions Arch is invalid")
		//}
		if platform.Base != defs.HPUnknown &&
			platform.Base != defs.HPVirt &&
			platform.Base != defs.HPBareMetal {
			return errors.New("FixerCreateOptions Base is invalid")
		}
		if platform.Virt != defs.HPVTNone &&
			platform.Virt != defs.HPVTVmware &&
			platform.Virt != defs.HPVTKvm &&
			platform.Virt != defs.HPVTXen &&
			platform.Virt != defs.HPVTHyperV {
			return errors.New("FixerCreateOptions Virt is invalid")
		}
		if platform.Base == defs.HPBareMetal &&
			len(platform.PciList) == 0 {
			return errors.New("FixerCreateOptions PciList is empty")
		}
	}

	if rp.X2xLibrary == "" {
		//rp.X2xLibrary = filepath.Join(xutil.ExecDir(), "library")
		dir, err := FindX2xLibraryDir()
		if err == nil {
			rp.X2xLibrary = dir
		}
	}
	//if !xutil.IsExisted(rp.X2xLibrary) {
	//	return errors.Errorf("FixerCreateOptions X2XLibrary(%s) is empty", rp.X2xLibrary)
	//}

	//
	// 修正
	//

	plats := []*Platform{&rp.Source, &rp.Target}
	for i := 0; i < len(plats); i++ {
		if plats[i].Base != "" {
			continue
		}
		plats[i].Base = defs.HPBareMetal
		plats[i].Virt = defs.HPVTNone
		for _, p := range plats[i].PciList {
			uniPci, err := universal.UniPciFromString(p)
			if err != nil {
				return err
			}
			if uniPci.VendorId() == 0x1af4 {
				plats[i].Base = defs.HPVirt
				plats[i].Virt = defs.HPVTKvm
				break
			}
			if uniPci.VendorId() == 0x5853 {
				plats[i].Base = defs.HPVirt
				plats[i].Virt = defs.HPVTXen
				break
			}
			if uniPci.VendorId() == 0x15ad {
				plats[i].Base = defs.HPVirt
				plats[i].Virt = defs.HPVTVmware
				break
			}
		}
	}

	usedInterfaceNames := make(map[string]struct{})
	for i := 0; i < len(rp.Network.Interfaces); i++ {
		if rp.Network.Interfaces[i].Name != "" {
			continue
		}
		for idx := 0; ; idx++ {
			name := fmt.Sprintf("eth%d", idx)
			if _, ok := usedInterfaceNames[name]; !ok {
				rp.Network.Interfaces[i].Name = name
				usedInterfaceNames[name] = struct{}{}
				break
			}
		}
	}

	return nil
}
