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
	"github.com/thoas/go-funk"
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
	efiVolumeLtr       string
	bootMode           define.BootMode
	hklmPath           string
	registryRootKey    string
	registryRootLoaded bool
	driverDatabaseType driverDatabaseType
	currentControlSet  int
	windowsVersion     define.WindowsVersion
	halType            define.HALType
}

func (fixer *windowsSystemFixer) Prepare() error {
	logger.Debugf("Prepare: ++")
	defer logger.Debugf("Prepare: --")

	if err := fixer.importForeignDisk(); err != nil {
		return errors.Wrap(err, "import foreign disk")
	}

	if err := fixer.detectSysVolume(); err != nil {
		return errors.Wrap(err, "detect system volume")
	}

	if err := fixer.loadSystemRegistry(); err != nil {
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

	if err := fixer.detectBootMode(); err != nil {
		return errors.Wrap(err, "detect boot mode")
	}

	return nil
}

func (fixer *windowsSystemFixer) Repair() error {
	logger.Debugf("Repair: ++")
	defer logger.Debugf("Repair: --")

	if err := fixer.disableArpCheck(); err != nil {
		return errors.Wrap(err, "disable arp check")
	}

	if err := fixer.disableAutoReboot(); err != nil {
		return errors.Wrap(err, "disable auto reboot")
	}

	if err := fixer.enableIDE(); err != nil {
		return errors.Wrap(err, "enable ide")
	}

	if err := fixer.enableSATA(); err != nil {
		return errors.Wrap(err, "enable sata")
	}

	if err := fixer.fixUefi(); err != nil {
		return errors.Wrap(err, "fix uefi")
	}

	if err := fixer.fixBCD(); err != nil {
		return errors.Wrap(err, "fix bcd")
	}

	var unconfigFun = fixer.unconfigBareMetal
	switch fixer.opts.RecoveryParam.Source.Virt {
	case define.HPVTXen:
		unconfigFun = fixer.unconfigXen
	case define.HPVTVmware:
		unconfigFun = fixer.unconfigVmware
	case define.HPVTKvm:
		unconfigFun = fixer.unconfigKvm
	case define.HPVTHyperV:
		unconfigFun = fixer.unconfigHyperV
	case define.HPVTParallels:
		unconfigFun = fixer.unconfigParallel
	}

	var configFun = fixer.configBareMetal
	switch fixer.opts.RecoveryParam.Target.Virt {
	case define.HPVTXen:
		configFun = fixer.configXen
	case define.HPVTVmware:
		configFun = fixer.configVmware
	case define.HPVTKvm:
		configFun = fixer.configKvm
	case define.HPVTHyperV:
		configFun = fixer.configHyperV
	case define.HPVTParallels:
		configFun = fixer.configParallel
	}

	if err := unconfigFun(); err != nil {
		return errors.Wrapf(err, "unconfig %s", fixer.opts.RecoveryParam.Source.Virt)
	}

	if err := configFun(); err != nil {
		return errors.Wrapf(err, "config %s", fixer.opts.RecoveryParam.Target.Virt)
	}

	return nil
}

func (fixer *windowsSystemFixer) CustomProcess(f func() error) error {
	logger.Debugf("CustomProcess: ++")
	defer logger.Debugf("CustomProcess: --")

	return f()
}

func (fixer *windowsSystemFixer) Cleanup() error {
	logger.Debugf("Cleanup: ++")
	defer logger.Debugf("Cleanup: --")

	if err := fixer.unloadSystemRegistry(); err != nil {
		return errors.Wrap(err, "cleanup")
	}

	return nil
}

func (fixer *windowsSystemFixer) GetLog() (LogEntry, bool) {
	select {
	case entry := <-fixer.logs:
		return entry, true
	default:
		return LogEntry{}, false
	}
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

func (fixer *windowsSystemFixer) loadSystemRegistry() error {
	logger.Debugf("mountRegistry: ++")
	defer logger.Debugf("mountRegistry: --")

	if fixer.offsys.registryRootLoaded {
		return nil
	}

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

	fixer.offsys.registryRootLoaded = true
	return nil
}

func (fixer *windowsSystemFixer) unloadSystemRegistry() error {
	logger.Debugf("unloadRegistry: ++")
	defer logger.Debugf("unloadRegistry: --")

	if fixer.offsys.registryRootKey == "" {
		return nil
	}

	if !fixer.offsys.registryRootLoaded {
		return nil
	}

	if e := unloadReg(fixer.offsys.registryRootKey); e != nil {
		return errors.Wrapf(e, "unload registry %s", fixer.offsys.registryRootKey)
	}

	fixer.offsys.registryRootLoaded = false

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

func (fixer *windowsSystemFixer) detectBootMode() error {
	logger.Debugf("detectBootMode: ++")
	defer logger.Debugf("detectBootMode: --")

	for _, v := range fixer.offsys.volumeLtrList {
		if !extend.IsEfiDir(v + ":\\") {
			continue
		}
		fixer.offsys.bootMode = define.BootModeUEFI
		fixer.offsys.efiVolumeLtr = v
		logger.Debugf("detectBootMode: uefi")
		return nil
	}

	fixer.offsys.bootMode = define.BootModeBIOS
	logger.Debugf("detectBootMode: bios")
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

// disableAutoReboot 禁止蓝屏后自动重启。
// 等价于：
// HKLM\SYSTEM\CurrentControlSet\Control\CrashControl\AutoReboot = 0
func (fixer *windowsSystemFixer) disableAutoReboot() error {
	logger.Debugf("disableAutoReboot: ++")
	defer logger.Debugf("disableAutoReboot: --")

	keyPath := fmt.Sprintf(
		"%s\\ControlSet00%d\\Control\\CrashControl",
		fixer.offsys.registryRootKey,
		fixer.offsys.currentControlSet)

	key, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		keyPath,
		registry.SET_VALUE,
	)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// 某些系统可能没有该项，直接忽略。
			return nil
		}
		return errors.Wrap(err, "open CrashControl failed")
	}
	defer key.Close()

	if err := key.SetDWordValue("AutoReboot", 0); err != nil {
		return errors.Wrap(err, "set AutoReboot failed")
	}

	logger.Infof("disabled Windows crash auto reboot")
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
		if start <= 1 {
			return errors.Wrapf(err, "open registry %s", serviceKeyPath)
		}
		if errors.Is(err, registry.ErrNotExist) {
			logger.Warnf("setServiceStart: registry key %s not found", serviceKeyPath)
			return nil
		}
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

func (fixer *windowsSystemFixer) disableClassFilters(serviceNames ...string) error {
	clsRoot := fmt.Sprintf(`%s\ControlSet00%d\Control\Class`,
		fixer.offsys.registryRootKey, fixer.offsys.currentControlSet)

	logger.Debugf("disableClassFilters: scanning %s, remove filters=%v",
		clsRoot, serviceNames)

	clsKey, err := registry.OpenKey(registry.LOCAL_MACHINE, clsRoot, registry.READ)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			logger.Debugf("disableClassFilters: class root not found: %s", clsRoot)
			return nil
		}
		return errors.Wrapf(err, "open %s failed", clsRoot)
	}
	defer clsKey.Close()

	subKeys, err := clsKey.ReadSubKeyNames(-1)
	if err != nil {
		return errors.Wrapf(err, "read subkeys of %s failed", clsRoot)
	}

	logger.Debugf("disableClassFilters: found %d class keys", len(subKeys))

	var modified int

	for _, sub := range subKeys {
		path := fmt.Sprintf(`%s\ControlSet00%d\Control\Class\%s`,
			fixer.offsys.registryRootKey,
			fixer.offsys.currentControlSet,
			sub)

		logger.Debugf("disableClassFilters: checking %s", path)

		key, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.ALL_ACCESS)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				logger.Debugf("disableClassFilters: class key not found: %s", path)
				continue
			}
			return errors.Wrapf(err, "open %s failed", path)
		}

		changed1, err := filterMultiSzValue(key, "UpperFilters", serviceNames, path)
		if err != nil {
			key.Close()
			return err
		}
		if changed1 {
			modified++
		}

		changed2, err := filterMultiSzValue(key, "LowerFilters", serviceNames, path)
		key.Close()
		if err != nil {
			return err
		}
		if changed2 {
			modified++
		}
	}

	logger.Debugf(
		"disableClassFilters: finished, scanned=%d modified=%d",
		len(subKeys),
		modified,
	)

	return nil
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

// injectDriversByDism 基于 DISM 向离线 Windows 系统注入驱动。
//
// 处理流程：
//  1. 卸载离线 SYSTEM 注册表，因为 DISM 要求独占访问注册表。
//  2. 查询离线 DriverStore 中已安装的第三方驱动。
//  3. 如果待注入的驱动已存在，则先卸载旧驱动，再重新注入。
//     某些离线系统可能存在 DriverStore 中已有驱动文件，但 Services
//     注册表项缺失的情况，此时直接重新注入不会恢复对应的驱动服务。
//     先卸载再重新注入，可使 DISM 重新创建驱动服务及相关注册表项。
//  4. 将指定目录下的驱动递归注入到离线系统。
func (fixer *windowsSystemFixer) injectDriversByDism(ds *x2xlib.DriverResource) error {
	logger.Debugf("injectDriversByDism: begin")
	defer logger.Debugf("injectDriversByDism: end")

	// DISM requires the SYSTEM hive to be unloaded.
	logger.Debugf("injectDriversByDism: unloading offline SYSTEM hive")
	if err := fixer.unloadSystemRegistry(); err != nil {
		return err
	}
	defer func() {
		logger.Debugf("injectDriversByDism: reloading offline SYSTEM hive")
		if err := fixer.loadSystemRegistry(); err != nil {
			logger.Warnf("injectDriversByDism: reload SYSTEM hive failed: %v", err)
		}
	}()

	// ----------------------------------------------------------------------
	// Query existing drivers
	// ----------------------------------------------------------------------

	logger.Debugf("injectDriversByDism: querying existing drivers")

	listCmd := fmt.Sprintf(
		`dism /Image:%s:\ /Get-Drivers`,
		fixer.offsys.sysVolumeLtr,
	)

	_, output, err := command.Execute(listCmd)
	if err != nil {
		return errors.Wrap(err, "query existing drivers")
	}

	driverStores := parseDriverStore(output)

	logger.Debugf(
		"injectDriversByDism: found %d third-party drivers",
		len(driverStores),
	)

	logger.Debugf(
		"injectDriversByDism: DriverStore:\n%s",
		extend.Pretty(driverStores),
	)

	// ----------------------------------------------------------------------
	// Find drivers that need to be removed.
	// ----------------------------------------------------------------------

	var publishedNames []string

	for _, driver := range driverStores {
		base := strings.ToLower(filepath.Base(driver.OriginFileName))
		module := strings.TrimSuffix(base, filepath.Ext(base))

		if funk.InStrings(ds.Modules, module) {
			publishedNames = append(publishedNames, driver.PublishedName)

			logger.Debugf(
				"injectDriversByDism: existing driver detected: module=%s published=%s",
				module,
				driver.PublishedName,
			)
		}
	}

	if len(publishedNames) != 0 {
		logger.Infof(
			"injectDriversByDism: removing %d existing driver(s): %v",
			len(publishedNames),
			publishedNames,
		)

		drvArgs := make([]string, 0)
		for _, publishName := range publishedNames {
			drvArgs = append(drvArgs, fmt.Sprintf("/Driver:%s", publishName))
		}
		rmCmdline := fmt.Sprintf(`dism /Image:%s:\ /Remove-Driver %s`,
			fixer.offsys.sysVolumeLtr,
			strings.Join(drvArgs, " "))

		if _, _, e := command.Execute(rmCmdline, command.WithDebug()); e != nil {
			return errors.Wrapf(e,
				"remove drivers (%s)",
				strings.Join(publishedNames, ", "))
		}

	} else {
		logger.Debugf("injectDriversByDism: no existing drivers need removal")
	}

	// ----------------------------------------------------------------------
	// Inject drivers.
	// ----------------------------------------------------------------------

	logger.Infof(
		"injectDriversByDism: injecting drivers from %s",
		ds.Dir,
	)

	injectCmd := fmt.Sprintf(
		`dism /Image:%s:\ /Add-Driver /Driver:%s /Recurse /ForceUnsigned`,
		fixer.offsys.sysVolumeLtr,
		ds.Dir,
	)

	_, output, err = command.Execute(injectCmd, command.WithDebug())
	if err != nil {
		logger.Errorf(
			"injectDriversByDism: driver injection failed\n%s",
			output,
		)
		return errors.Wrapf(
			err,
			"inject drivers (%s)",
			strings.Join(ds.Modules, ","),
		)
	}

	logger.Infof(
		"injectDriversByDism: successfully injected %d driver module(s): %s",
		len(ds.Modules),
		strings.Join(ds.Modules, ","),
	)

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

func (fixer *windowsSystemFixer) fixUefi() error {
	logger.Debugf("fixUefi: ++")
	defer logger.Debugf("fixUefi: --")

	if fixer.offsys.bootMode != define.BootModeUEFI {
		return nil
	}

	espRoot := fixer.offsys.efiVolumeLtr + ":\\"

	bootmgfw := filepath.Join(
		espRoot,
		"EFI", "Microsoft", "Boot", "bootmgfw.efi",
	)
	if !extend.IsExisted(bootmgfw) {
		logger.Debugf("Windows Boot Manager not found: %s", bootmgfw)
		return nil
	}

	bootDir := filepath.Join(espRoot, "EFI", "Boot")
	fallback := filepath.Join(
		bootDir,
		fmt.Sprintf("BOOT%s.EFI", strings.ToUpper(getUefiArch())),
	)

	// 已经存在且内容一致
	if extend.IsExisted(fallback) {
		equal, err := extend.FileEqual(bootmgfw, fallback)
		if err == nil && equal {
			logger.Debugf("UEFI fallback bootloader already exists.")
			return nil
		}
	}

	logger.Infof("Fixing UEFI fallback bootloader.")

	// virt-v2v 的行为：删除整个 EFI\Boot
	if err := os.RemoveAll(bootDir); err != nil {
		return errors.Wrap(err, "remove EFI\\Boot")
	}

	if err := os.MkdirAll(bootDir, 0755); err != nil {
		return errors.Wrap(err, "create EFI\\Boot")
	}

	if _, err := extend.CopyFile(bootmgfw, fallback); err != nil {
		return errors.Wrap(err, "copy bootmgfw.efi")
	}

	logger.Infof("Created fallback bootloader: %s", fallback)

	return nil
}

func (fixer *windowsSystemFixer) fixBCD() error {
	logger.Debugf("fixBCD: ++")
	defer logger.Debugf("fixBCD: --")

	if fixer.offsys.bootMode != define.BootModeUEFI {
		return nil
	}

	bcdPath := filepath.Join(
		fixer.offsys.efiVolumeLtr+":\\",
		"EFI", "Microsoft", "Boot", "BCD",
	)

	if !extend.IsExisted(bcdPath) {
		logger.Debugf("BCD not found: %s", bcdPath)
		return nil
	}

	logger.Infof("Fixing Windows BCD.")

	const hiveName = `OFFLINEBCDH0NK1`
	regRoot := `HKLM\` + hiveName

	if err := loadReg(regRoot, bcdPath); err != nil {
		return errors.Wrapf(err, "load BCD hive: %s", bcdPath)
	}
	defer func() {
		if err := unloadReg(regRoot); err != nil {
			logger.Warnf("Unload BCD hive failed: %v", err)
		}
	}()

	const bootMgrGuid = `{9dea862c-5cdd-4e70-acc1-f32b344d4795}`

	defaultKey := hiveName +
		`\Objects\` +
		bootMgrGuid +
		`\Elements\23000003`

	k, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		defaultKey,
		registry.QUERY_VALUE,
	)
	if err != nil {
		logger.Debugf("BCD default boot entry not found.")
		return nil
	}
	defer k.Close()

	currentGuid, _, err := k.GetStringValue("Element")
	if err != nil {
		logger.Debugf("BCD default boot GUID not found.")
		return nil
	}

	currentGuid = strings.TrimSpace(currentGuid)

	graphicsKey := hiveName +
		`\Objects\` +
		currentGuid +
		`\Elements\16000046`

	if err = deleteRegistryTree(registry.LOCAL_MACHINE, graphicsKey); err != nil {
		return errors.Wrap(err, "delete graphicsmodedisabled")
	}

	logger.Infof("Removed BCD graphicsmodedisabled option.")

	return nil
}
