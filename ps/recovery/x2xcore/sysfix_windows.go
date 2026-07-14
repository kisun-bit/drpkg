package x2xcore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kisun-bit/drpkg/command"
	"github.com/kisun-bit/drpkg/define"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/ps/recovery/x2xlib"
	"github.com/pkg/errors"
	"golang.org/x/sys/windows/registry"
)

type driverDatabaseType int

const (
	drvDbUnknown driverDatabaseType = iota
	drvDbLegacy
	drvDbDeviceIds
)

type windowsSystemFixer struct {
	ctx    context.Context
	opts   *FixerCreateOptions // 恢复参数
	logs   <-chan LogEntry     // 日志缓存通道
	x2xLib *x2xlib.X2XLib      // 驱动库
	offsys offlineSystem       // 离线系统的私有信息
}

type offlineSystem struct {
	volumeLtrList      []string
	sysVolumeLtr       string // 系统卷
	hklmPath           string
	registryRootKey    string
	driverDatabaseType driverDatabaseType
	currentControlSet  int
	windowsVersion     define.WindowsVersion
	halType            define.HALType
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

	if err := fixer.loadRegistry(); err != nil {
		return errors.Wrap(err, "mount registry")
	}

	if err := fixer.detectCurrentControlSet(); err != nil {
		return errors.Wrap(err, "detect current control set")
	}

	if err := fixer.detectDriverDatabaseType(); err != nil {
		return errors.Wrap(err, "detect driver database")
	}

	if err := fixer.detectWindowsVersion(); err != nil {
		return errors.Wrap(err, "detect windows version")
	}

	if err := fixer.detectHAL(); err != nil {
		return errors.Wrap(err, "detect hal")
	}

	return nil
}

func (fixer *windowsSystemFixer) Repair() error {
	logger.Debugf("Repair: ++")
	defer logger.Debugf("Repair: --")

	if err := fixer.disableArpCheck(); err != nil {
		return errors.Wrap(err, "disable arp check")
	}

	if err := fixer.enableIDE(); err != nil {
		return errors.Wrap(err, "enable ide")
	}

	if err := fixer.enableSATA(); err != nil {
		return errors.Wrap(err, "enable sata")
	}

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

	if err := fixer.unloadRegistry(); err != nil {
		return errors.Wrap(err, "cleanup")
	}

	return nil
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

	for _, v := range vs {
		if v.DriveLetter == "" {
			existed, ltr := getFreeLtr()
			if !existed {
				return errors.New("no free ltr")
			}
			if err := AssignDriveLetter(v.DeviceID, ltr); err != nil {
				return errors.Wrapf(err, "assign drive letter for %s", v.DeviceID)
			}
			v.DriveLetter = ltr
		}
		fixer.offsys.volumeLtrList = append(fixer.offsys.volumeLtrList, v.DriveLetter)
	}
	logger.Debugf("detectSysVolume: volumes:\n%s", extend.Pretty(fixer.offsys.volumeLtrList))

	for _, v := range fixer.offsys.volumeLtrList {
		vp := v + ":\\"
		if !extend.IsRootDir(vp) {
			continue
		}
		fixer.offsys.sysVolumeLtr = v
		break
	}
	logger.Debugf("detectSysVolume: system volume: %v", fixer.offsys.sysVolumeLtr)

	return nil
}

func (fixer *windowsSystemFixer) loadRegistry() error {
	logger.Debugf("mountRegistry: ++")
	defer logger.Debugf("mountRegistry: --")

	hklmPath := filepath.Join(fixer.offsys.sysVolumeLtr+":\\", "Windows", "System32", "config", "SYSTEM")
	if !extend.IsExisted(hklmPath) {
		return errors.Wrapf(os.ErrNotExist, hklmPath)
	}
	if fixer.offsys.hklmPath == "" {
		fixer.offsys.hklmPath = hklmPath
	}
	registryRootKey := "HKLM\\OFFLINESYSTEMH0NK1"

	if e := loadReg(registryRootKey, fixer.offsys.hklmPath); e != nil {
		return errors.Wrapf(e, "load registry file %s", hklmPath)
	}
	fixer.offsys.registryRootKey = registryRootKey

	logger.Debugf("mountRegistry: %s is mounted", fixer.offsys.hklmPath)
	return nil
}

func (fixer *windowsSystemFixer) unloadRegistry() error {
	logger.Debugf("unloadRegistry: ++")
	defer logger.Debugf("unloadRegistry: --")

	if fixer.offsys.registryRootKey == "" {
		return nil
	}

	if e := unloadReg(fixer.offsys.registryRootKey); e != nil {
		return errors.Wrapf(e, "unload registry %s", fixer.offsys.registryRootKey)
	}

	return nil
}

func (fixer *windowsSystemFixer) detectCurrentControlSet() error {
	logger.Debugf("detectCurrentControlSet: ++")
	defer logger.Debugf("detectCurrentControlSet: --")

	selectKeyPath := fmt.Sprintf("%s\\Select", fixer.offsys.registryRootKey)
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, selectKeyPath, registry.READ)

	if err == nil {
		defer key.Close()

		val, _, e := key.GetIntegerValue("Current")
		if e == nil {
			fixer.offsys.currentControlSet = int(val)
			logger.Debugf("detectCurrentControlSet: current control set: %d", fixer.offsys.currentControlSet)
			return nil
		}

		err = e
	}

	if errors.Is(err, registry.ErrNotExist) {
		fixer.offsys.currentControlSet = 1
		logger.Warnf("detectCurrentControlSet: current control set does not exist, force to set 1")
		return nil
	}

	return errors.Wrapf(err, "detectCurrentControlSet")
}

func (fixer *windowsSystemFixer) detectDriverDatabaseType() error {
	logger.Debugf("detectDriverDatabaseType: ++")
	defer logger.Debugf("detectDriverDatabaseType: --")

	paths := []struct {
		path string
		typ  driverDatabaseType
	}{
		{
			fmt.Sprintf("%s\\ControlSet00%d\\Control\\CriticalDeviceDatabase",
				fixer.offsys.registryRootKey,
				fixer.offsys.currentControlSet),
			drvDbLegacy,
		},
		{
			fmt.Sprintf("%s\\DriverDatabase\\DeviceIds\\PCI",
				fixer.offsys.registryRootKey),
			drvDbDeviceIds,
		},
	}

	for _, item := range paths {
		key, err := registry.OpenKey(registry.LOCAL_MACHINE, item.path, registry.READ)
		switch {
		case err == nil:
			_ = key.Close()
			fixer.offsys.driverDatabaseType = item.typ
			logger.Debugf("detectDriverDatabaseType: %v", item.typ)
			return nil
		case errors.Is(err, registry.ErrNotExist):
			continue
		default:
			return errors.Wrap(err, "detectDriverDatabaseType")
		}
	}

	return nil
}

func (fixer *windowsSystemFixer) detectWindowsVersion() error {
	logger.Debugf("detectWindowsVersion: ++")
	defer logger.Debugf("detectWindowsVersion: --")

	offlineSoftwareHivePath := filepath.Join(
		fixer.offsys.sysVolumeLtr+":\\",
		"Windows", "System32", "config", "SOFTWARE",
	)

	const offlineSoftwareKeyName = "OfflineSoftwareReg"
	offlineSoftwareKey := "HKLM\\" + offlineSoftwareKeyName

	if err := loadReg(offlineSoftwareKey, offlineSoftwareHivePath); err != nil {
		return errors.Wrapf(err, "load registry %s", offlineSoftwareHivePath)
	}
	defer func() {
		if err := unloadReg(offlineSoftwareKey); err != nil {
			logger.Errorf("unload registry: %v", err)
		}
	}()

	currentVersionKey := fmt.Sprintf(
		`%s\Microsoft\Windows NT\CurrentVersion`,
		offlineSoftwareKeyName,
	)

	key, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		currentVersionKey,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return errors.Wrapf(err, "open registry %s", currentVersionKey)
	}
	defer key.Close()

	readString := func(name string) string {
		v, _, err := key.GetStringValue(name)
		if err != nil {
			return ""
		}
		return v
	}

	readDWORD := func(name string) uint64 {
		v, _, err := key.GetIntegerValue(name)
		if err != nil {
			return 0
		}
		return v
	}

	currentVersion := readString("CurrentVersion")
	buildStr := readString("CurrentBuildNumber")
	if buildStr == "" {
		buildStr = readString("CurrentBuild")
	}

	build, _ := strconv.Atoi(buildStr)

	major := readDWORD("CurrentMajorVersionNumber")
	//minor := readDWORD("CurrentMinorVersionNumber")

	productName := readString("ProductName")

	winVer := detectWindowsVersion(
		productName,
		currentVersion,
		build,
		major,
	)

	logger.Infof(
		"Detected Windows: %v (%s, Build=%d)",
		winVer,
		productName,
		build,
	)

	fixer.offsys.windowsVersion = winVer

	return nil
}

func (fixer *windowsSystemFixer) detectHAL() error {
	logger.Debugf("detectHAL: ++")
	defer logger.Debugf("detectHAL: --")

	keyPath := fmt.Sprintf(
		`%s\ControlSet00%d\Control\HAL`,
		fixer.offsys.registryRootKey,
		fixer.offsys.currentControlSet,
	)

	key, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		keyPath,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return err
	}
	defer key.Close()

	id, _, err := key.GetStringValue("Identifier")
	if err != nil {
		return err
	}

	switch {

	case strings.Contains(id, "ACPI Multiprocessor"):
		fixer.offsys.halType = define.HALACPIMultiprocessor
		break

	case strings.Contains(id, "ACPI Uniprocessor"):
		fixer.offsys.halType = define.HALACPIUniprocessor
		break

	case strings.Contains(id, "Standard PC"):
		fixer.offsys.halType = define.HALStandardPC
		break

	case strings.Contains(id, "MPS Multiprocessor"):
		fixer.offsys.halType = define.HALMPSMultiprocessor
		break

	case strings.Contains(id, "MPS Uniprocessor"):
		fixer.offsys.halType = define.HALMPSUniprocessor
		break

	default:
		fixer.offsys.halType = define.HALUnknown
		break
	}

	logger.Debugf("detectHAL: HAL: %v", fixer.offsys.halType)

	return nil
}

func (fixer *windowsSystemFixer) disableArpCheck() error {
	logger.Debugf("disableArpCheck: ++")
	defer logger.Debugf("disableArpCheck: --")

	// 仅当存在静态 IP 配置时才关闭 ARP Retry。
	hasStaticIP := false
	for _, iface := range fixer.opts.RecoveryParam.Network.Interfaces {
		if len(iface.IPAddr) > 0 {
			hasStaticIP = true
			break
		}
	}
	if !hasStaticIP {
		logger.Debugf("disableArpCheck: no static IP configuration, skip")
		return nil
	}

	tcpipKeyPath := fmt.Sprintf(
		"%s\\ControlSet00%d\\Services\\Tcpip\\Parameters",
		fixer.offsys.registryRootKey,
		fixer.offsys.currentControlSet,
	)

	key, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		tcpipKeyPath,
		registry.SET_VALUE,
	)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			logger.Warnf("disableArpCheck: registry key %s not found", tcpipKeyPath)
			return nil
		}
		return errors.Wrapf(err, "open registry %s", tcpipKeyPath)
	}
	defer key.Close()

	if err := key.SetDWordValue("ArpRetryCount", 0); err != nil {
		return errors.Wrap(err, "set ArpRetryCount")
	}

	logger.Debugf("disableArpCheck: ArpRetryCount=0")
	return nil
}

func (fixer *windowsSystemFixer) changeHal() error {
	logger.Debugf("changeHal: ++")
	defer logger.Debugf("changeHal: --")

	// TODO Vista之前可能需要切换hal.dll、ntoskrnl.exe
	return nil
}

func (fixer *windowsSystemFixer) setServiceStart(serviceName string, start uint32) error {
	logger.Debugf("setServiceStart: ++")
	defer logger.Debugf("setServiceStart: --")

	logger.Debugf("setServiceStart: %s -> %d", serviceName, start)

	serviceKeyPath := fmt.Sprintf(
		"%s\\ControlSet00%d\\Services\\%s",
		fixer.offsys.registryRootKey,
		fixer.offsys.currentControlSet,
		serviceName,
	)

	serviceKey, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		serviceKeyPath,
		registry.QUERY_VALUE|registry.SET_VALUE,
	)
	if err != nil {
		return errors.Wrapf(err, "open registry %s", serviceKeyPath)
	}
	defer serviceKey.Close()

	// 设置 Start
	if err = serviceKey.SetDWordValue("Start", start); err != nil {
		return errors.Wrap(err, "set Start")
	}

	// 删除 StartOverride（Win8+）
	startOverrideKeyPath := serviceKeyPath + `\StartOverride`
	if err = registry.DeleteKey(registry.LOCAL_MACHINE, startOverrideKeyPath); err != nil &&
		!errors.Is(err, registry.ErrNotExist) {
		logger.Warnf("delete %s failed: %v", startOverrideKeyPath, err)
	}

	return nil
}

func (fixer *windowsSystemFixer) enableService(serviceName string) error {
	return fixer.setServiceStart(serviceName, 0)
}

func (fixer *windowsSystemFixer) disableService(serviceName string) error {
	return fixer.setServiceStart(serviceName, 3)
}

func (fixer *windowsSystemFixer) existedService(serviceName string) bool {
	serviceKeyPath := fmt.Sprintf(
		"%s\\ControlSet00%d\\Services\\%s",
		fixer.offsys.registryRootKey,
		fixer.offsys.currentControlSet,
		serviceName,
	)

	serviceKey, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		serviceKeyPath,
		registry.READ,
	)
	if err != nil {
		return false
	}
	defer serviceKey.Close()

	return true
}

func (fixer *windowsSystemFixer) deleteService(serviceName string) error {
	serviceKeyPath := fmt.Sprintf(
		"%s\\ControlSet00%d\\Services\\%s",
		fixer.offsys.registryRootKey,
		fixer.offsys.currentControlSet,
		serviceName,
	)

	if !fixer.existedService(serviceName) {
		return nil
	}

	return deleteRegistryTree(registry.LOCAL_MACHINE, serviceKeyPath)
}

func (fixer *windowsSystemFixer) addDrivers(ds *x2xlib.DriverResource) error {
	logger.Debugf("addDrivers: --")
	defer logger.Debugf("addDrivers: --")

	logger.Debugf("addDrivers: directory: %s", ds.Dir)

	cmdline := fmt.Sprintf(`dism /Image:%s:\ /Add-Driver /Driver:%s /Recurse /ForceUnsigned`)
	_, _, e := command.Execute(cmdline, command.WithDebug())
	if e != nil {
		return errors.Wrapf(e, "add drivers(%s)", strings.Join(ds.Modules, ","))
	}
	return nil
}

func (fixer *windowsSystemFixer) enableIDE() error {
	drivers := []string{
		"atapi",
		"pciide",
		"intelide",
	}

	for _, d := range drivers {
		if fixer.existedService(d) {
			if err := fixer.enableService(d); err != nil {
				return err
			}
		}
	}

	return nil
}

func (fixer *windowsSystemFixer) enableSATA() error {
	drivers := []string{
		"storahci",
		"msahci",
		"iaStor",
		"iaStorV",
	}

	for _, d := range drivers {
		if fixer.existedService(d) {
			if err := fixer.enableService(d); err != nil {
				return err
			}
		}
	}

	return nil
}
