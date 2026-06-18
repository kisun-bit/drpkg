package x2xcore

import (
	"context"

	"github.com/kisun-bit/drpkg/define"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/ps/recovery/x2xlib"
	"github.com/pkg/errors"
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
	volumes   []string
	sysVolume string // 系统卷
}

//
// 如何判断一个离线Windows是否能够兼容某硬件？
//
// 路径1：HKEY_LOCAL_MACHINE\SYSTEM\CurrentControlSet\Control\CriticalDeviceDatabase
//     举例：
//     HKEY_LOCAL_MACHINE\SYSTEM\CurrentControlSet\Control\CriticalDeviceDatabase\PCI#VEN_1AF4&DEV_1001
//     ClassGUID        REG_SZ  {4D36E97B-E325-11CE-BFC1-08002BE10318}
//     DriverPackageId  REG_SZ  viostor.inf_amd64_neutral_c8a073b64be3602f
//     Service          REG_SZ  viostor
//     说明：
//     DriverPackageId的值对应C:\Windows\System32\DriverStore\FileRepository
//     Service的值就是我们关心的驱动服务的值
//
// 路径2：HKEY_LOCAL_MACHINE\SYSTEM\DriverDatabase\DeviceIds\PCI
//     举例：
//     HKEY_LOCAL_MACHINE\SYSTEM\DriverDatabase\DeviceIds\PCI\VEN_1AF4&DEV_1001&SUBSYS_00021AF4&REV_00
//     oem35.inf
//     说明：
//     oem35.inf表示驱动安装脚本。
//     然后去HKEY_LOCAL_MACHINE\SYSTEM\DriverDatabase\DriverInfFiles\oem35.inf下，得到
//     (默认)            REG_MULTI_SZ  viostor.inf_amd64_aa6c91b5db55ab62
//     Active           REG_MULTI_SZ  viostor.inf_amd64_aa6c91b5db55ab62
//     而viostor.inf_amd64_aa6c91b5db55ab62就代表驱动库id
//     接着找HKEY_LOCAL_MACHINE\SYSTEM\DriverDatabase\DriverPackages\viostor.inf_amd64_aa6c91b5db55ab62下
//     可以得到驱动的详细信息：
//     SignerScore       REG_DWORD     d000005
//     ......更多信息
//     另外需要注意的是，尽量对所有符合条件的驱动都拿到，然后取SignerScore最高者的服务名（服务名通过解析得到即可）
//     那么viostor就是我们关心的驱动服务的值
//
// 若成功取得驱动服务，说明该离线Windows兼容此硬件，我们只需要去将其设置为开机启动即可，否则说明此Windows不兼容此硬件
// 设置开机启动的步骤为：
// 1. 删除StartOverride项（若存在），如：HKEY_LOCAL_MACHINE\SYSTEM\CurrentControlSet\Services\stornvme\StartOverride
// 2. 将Start的数据改成0，如：HKEY_LOCAL_MACHINE\SYSTEM\CurrentControlSet\Services\stornvme下的Start
//

func (fixer *windowsSystemFixer) Prepare() error {
	logger.Debugf("Prepare: ++")
	defer logger.Debugf("Prepare: --")

	if err := fixer.importForeignDisk(); err != nil {
		return errors.Wrap(err, "import foreign disk")
	}

	if err := fixer.detectSysVolume(); err != nil {
		return errors.Wrap(err, "detect system volume")
	}

	return errors.New("not implemented")
}

func (fixer *windowsSystemFixer) Repair() error {
	logger.Debugf("Repair: ++")
	defer logger.Debugf("Repair: --")

	return errors.New("not implemented")
}

func (fixer *windowsSystemFixer) CustomProcess(f func() error) error {
	logger.Debugf("CustomProcess: ++")
	defer logger.Debugf("CustomProcess: --")

	return f()
}

func (fixer *windowsSystemFixer) Cleanup() error {
	logger.Debugf("Cleanup: ++")
	defer logger.Debugf("Cleanup: --")

	return errors.New("not implemented")
}

func (fixer *windowsSystemFixer) GetLog() (LogEntry, bool) {
	return LogEntry{}, false
}

func (fixer *windowsSystemFixer) GetPreferHostConfig(define.HPVirtType) (PreferConfig, error) {
	return PreferConfig{}, errors.New("not implemented")
}

func (fixer *windowsSystemFixer) importForeignDisk() error {
	logger.Debugf("importForeignDisk: ++")
	defer logger.Debugf("importForeignDisk: --")

	return ImportForeignDisk()
}

func (fixer *windowsSystemFixer) detectSysVolume() error {
	logger.Debugf("detectSysVolume: ++")
	defer logger.Debugf("detectSysVolume: --")

	vs, e := ListLocalVolumes()
	if e != nil {
		return e
	}
	_ = vs
	return errors.New("not implemented")
}
