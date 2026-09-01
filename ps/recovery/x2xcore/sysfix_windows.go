package x2xcore

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kisun-bit/drpkg/command"
	"github.com/kisun-bit/drpkg/define"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/ps/info"
	"github.com/kisun-bit/drpkg/ps/recovery/x2xlib"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
	"golang.org/x/sys/windows/registry"
)

type driverDatabaseType int

const (
	drvDbUnknown driverDatabaseType = iota
	drvDbLegacy
	drvDbDriverStore
)

type windowsSystemFixer struct {
	ctx     context.Context
	opts    *FixerCreateOptions // 恢复参数
	logs    chan LogEntry       // 日志缓存通道
	x2xLib  *x2xlib.X2XLib      // 驱动库
	reqPort io.Writer           // 修复虚拟机与宿主机的通信信道
	offsys  offlineSystem       // 离线系统的私有信息
}

type infMap struct {
	serviceName string   // inf中安装的服务名
	classGuid   string   // inf中声明的设备类GUID（ClassGUID）
	infName     string   // inf文件名（Windows\Inf目录下）
	pciList     []string // inf中声明的pci硬件/兼容ID集合（小写归一化）
	sysFiles    []string // inf声明的.sys驱动文件列表
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
	legacyBlockDriver  string   // 传统方式注入的 virtio 块驱动服务名（viostor/vioscsi），空表示未注入
	legacyNetDriver    bool     // 是否以传统方式注入了 netkvm 网络驱动
	infMaps            []infMap // 驱动名至pci硬件集的映射表，key是
}

func NewSysFixer(ctx context.Context, opts *FixerCreateOptions, serialReqPort io.Writer) (fixer SysFixer, err error) {
	logger.Debugf("NewSysFixer: opts:\n%s", extend.Pretty(opts))
	if err = CheckAndFillFixerCreateOptions(opts); err != nil {
		return nil, err
	}
	logger.Debugf("NewSysFixer: opts(repaired):\n%s", extend.Pretty(opts))
	lf := &windowsSystemFixer{ctx: ctx, opts: opts, logs: make(chan LogEntry, 1000)}

	if opts.InRepairVM {
		if serialReqPort == nil {
			return nil, errors.New("serialReqPort is required")
		}
		lf.reqPort = serialReqPort
	}

	lf.x2xLib, err = x2xlib.NewX2XLib(opts.RecoveryParam.X2xLibrary, true)
	if err != nil {
		return nil, err
	}
	return lf, nil
}

func (fixer *windowsSystemFixer) Prepare() error {
	logger.Debugf("Prepare: ++")
	defer logger.Debugf("Prepare: --")

	fixer.infof(LogTplForReadyWith0Args)

	if err := fixer.importForeignDisk(); err != nil {
		logger.Warnf("Prepare: importForeignDisk: %v", err)
	}

	if err := fixer.detectSysVolume(); err != nil {
		return errors.Wrap(err, "detect system volume")
	}

	if err := fixer.chkVolume(); err != nil {
		return errors.Wrap(err, "check volume")
	}

	fixer.infof(LogTplForLoadRegistryWith0Args)
	if err := fixer.loadSystemRegistry(); err != nil {
		return errors.Wrap(err, "mount registry")
	}

	if err := fixer.detectCurrentControlSet(); err != nil {
		return errors.Wrap(err, "detect current control set")
	}
	fixer.infof(LogTplForPrintControlSetWith1Args, fixer.offsys.currentControlSet)

	if err := fixer.detectDriverDatabaseType(); err != nil {
		return errors.Wrap(err, "detect driver database")
	}

	if err := fixer.detectWindowsVersion(); err != nil {
		return errors.Wrap(err, "detect windows version")
	}

	if err := fixer.detectHAL(); err != nil {
		//return errors.Wrap(err, "detect hal")
		logger.Warnf("Prepare: detectHAL: %v", err)
	}

	if err := fixer.detectBootMode(); err != nil {
		return errors.Wrap(err, "detect boot mode")
	}

	return nil
}

func (fixer *windowsSystemFixer) Repair() (err error) {
	logger.Debugf("Repair: ++")
	defer logger.Debugf("Repair: --")

	defer fixer.sync()

	// 1. 基础系统修复
	if err = fixer.repairBaseSystem(); err != nil {
		return err
	}

	// 2. 低版本 Windows（< Win7/2008R2）不支持 DISM 与 firstboot
	//    配置服务，跳过服务/网络注入，但仍执行虚拟化平台驱动适配
	//    （通过 CriticalDeviceDatabase 等传统方式注入）。
	dismSupported, e := fixer.isModernWindows()
	if e != nil {
		return e
	}

	if dismSupported {
		// 3. 注入配置与网络服务（仅支持 Win7/2008R2 及以上）
		if err = fixer.injectConfigService(); err != nil {
			return errors.Wrap(err, "inject config service")
		}
		if err = fixer.injectNetworkConfig(); err != nil {
			return errors.Wrap(err, "inject network config")
		}
	} else {
		fixer.warnf(LogTplForSkipFirstBootServiceWith1Args, fixer.offsys.windowsVersion)
	}

	// 4. 虚拟化平台驱动适配
	if err = fixer.adaptVirtPlatform(); err != nil {
		return err
	}

	return nil
}

// repairBaseSystem 执行基础系统修复步骤
func (fixer *windowsSystemFixer) repairBaseSystem() error {
	steps := []struct {
		name string
		fn   func() error
	}{
		{"disable arp check", fixer.disableArpCheck},
		{"disable auto reboot", fixer.disableAutoReboot},
		{"enable ide", fixer.enableIDE},
		{"enable sata", fixer.enableSATA},
		{"fix uefi", fixer.fixUefi},
		{"fix bcd", fixer.fixBCD},
	}

	for _, step := range steps {
		if err := step.fn(); err != nil {
			return errors.Wrap(err, step.name)
		}
	}
	return nil
}

// adaptVirtPlatform 根据源/目标虚拟化平台执行驱动卸载与安装
func (fixer *windowsSystemFixer) adaptVirtPlatform() error {
	param := fixer.opts.RecoveryParam

	// 同源同目标虚拟化平台且配置要求跳过时，无需适配
	if param.SkipDriverRepairIfPlatformUnchanged &&
		param.Source.Base == define.HPVirt &&
		param.Target.Base == define.HPVirt &&
		param.Source.Virt == param.Target.Virt {
		// TODO: 虚拟化硬件平台相同时，忽略修复
		return nil
	}

	unconfigFun := fixer.getUnconfigFunc(param.Source.Virt)
	if err := unconfigFun(); err != nil {
		return errors.Wrapf(err, "unconfig %s", param.Source.Virt)
	}

	configFun := fixer.getConfigFunc(param.Target.Virt)
	if err := configFun(); err != nil {
		return errors.Wrapf(err, "config %s", param.Target.Virt)
	}

	return nil
}

func (fixer *windowsSystemFixer) getUnconfigFunc(virt define.HPVirtType) func() error {
	switch virt {
	case define.HPVTXen:
		return fixer.unconfigXen
	case define.HPVTVmware:
		return fixer.unconfigVmware
	case define.HPVTKvm:
		return fixer.unconfigKvm
	case define.HPVTHyperV:
		return fixer.unconfigHyperV
	case define.HPVTParallels:
		return fixer.unconfigParallel
	default:
		return fixer.unconfigBareMetal
	}
}

func (fixer *windowsSystemFixer) getConfigFunc(virt define.HPVirtType) func() error {
	switch virt {
	case define.HPVTXen:
		return fixer.configXen
	case define.HPVTVmware:
		return fixer.configVmware
	case define.HPVTKvm:
		return fixer.configKvm
	case define.HPVTHyperV:
		return fixer.configHyperV
	case define.HPVTParallels:
		return fixer.configParallel
	default:
		return fixer.configBareMetal
	}
}

func (fixer *windowsSystemFixer) CustomProcess(f func() error) error {
	logger.Debugf("CustomProcess: ++")
	defer logger.Debugf("CustomProcess: --")

	return f()
}

func (fixer *windowsSystemFixer) Cleanup() error {
	logger.Debugf("Cleanup: ++")
	defer logger.Debugf("Cleanup: --")

	fixer.infof(LogTplForUnloadRegistryWith0Args)
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

func (fixer *windowsSystemFixer) GetPreferHostConfig(virtual define.HPVirtType) (cfg PreferConfig, err error) {
	switch virtual {
	case define.HPVTKvm:
		cfg.Chipset = define.ChipsetI440fx
		cfg.Video = define.VideoVGA
		cfg.DiskBus = define.DiskBusIde
		cfg.NetworkType = define.NetworkTypeRTL8192

		if modern, _ := fixer.isModernWindows(); modern {
			cfg.Chipset = define.ChipsetQ35
			cfg.DiskBus = define.DiskBusVirtio
			cfg.NetworkType = define.NetworkTypeVIRTIO
		} else {
			// 旧系统（< Win7/2008R2）若已成功注入（或已具备可用的）
			// legacy virtio 驱动，宿主机即可使用 virtio 磁盘/网卡；
			// 保持 i440fx 与 legacy PCI 设备 ID
			// （见 viostorCompatIds/vioscsiCompatIds）。
			blockDrv, netDrv := fixer.probeLegacyKvmDrivers()
			if blockDrv != "" {
				cfg.DiskBus = define.DiskBusVirtio
			}
			if netDrv {
				cfg.NetworkType = define.NetworkTypeVIRTIO
			}
		}

		if fixer.opts.RecoveryParam.Source.Arch == "arm64" {
			cfg.Chipset = define.ChipsetVirt
		}

		return cfg, nil
	default:
		return cfg, errors.New("GetPreferHostConfig: unsupported virtual type")
	}
}

func (fixer *windowsSystemFixer) importForeignDisk() error {
	logger.Debugf("importForeignDisk: ++")
	defer logger.Debugf("importForeignDisk: --")

	fixer.infof(LogTplForOfflineSystemReadyWith0Args)

	return ImportForeignDisk()
}

func (fixer *windowsSystemFixer) detectSysVolume() error {
	logger.Debugf("detectSysVolume: ++")
	defer logger.Debugf("detectSysVolume: --")

	fixer.infof(LogTplForEnumFsWith0Args)

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
		ltr := strings.TrimSuffix(v.DriveLetter, ":")
		fixer.offsys.volumeLtrList = append(fixer.offsys.volumeLtrList, ltr)
	}
	logger.Debugf("detectSysVolume: volumes:\n%s", extend.Pretty(fixer.offsys.volumeLtrList))

	fixer.infof(LogTplForSpecifySystemBootDeviceWith0Args)

	for _, v := range fixer.offsys.volumeLtrList {
		if info.IsMemoryOS() && strings.ToLower(v) == "x" {
			continue
		}
		vp := v + ":\\"

		if IsLockedByBitlocker(v) {
			fixer.infof(LogTplForUnlockBitlockerWith1Args, v)
			if eu := UnlockBitlockerWithRecoveryKey(v, fixer.opts.RecoveryParam.BitlockerGlobalRecoveryKey); eu != nil {
				fixer.errorf(LogTplForUnlockBitlockerFailedWith2Args, v, eu)
				continue
			}
		}

		if !extend.IsRootDir(vp) {
			continue
		}
		fixer.offsys.sysVolumeLtr = v
		break
	}

	if fixer.offsys.sysVolumeLtr == "" {
		return errors.Errorf("system volume letter is empty: offsys detection failed")
	}

	fixer.infof(LogTplForPrintSystemBootDeviceWith2Args,
		fixer.offsys.sysVolumeLtr+":",
		fixer.offsys.sysVolumeLtr+":\\")

	logger.Debugf("detectSysVolume: system volume: %v", fixer.offsys.sysVolumeLtr)

	return nil
}

func (fixer *windowsSystemFixer) chkVolume() error {
	if !fixer.opts.RecoveryParam.FsckFs {
		return nil
	}
	fixer.infof(LogTplForFsckFsWith0Args)
	for _, v := range fixer.offsys.volumeLtrList {
		_, _, _ = command.Execute(fmt.Sprintf("chkdsk.exe /f %s:", v), command.WithDebug())
	}
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
	registryRootKey := "OFFLINESYSTEMH0NK1"

	_ = unloadReg(registryRootKey)
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
			drvDbDriverStore,
		},
	}

	for _, item := range paths {
		key, err := registry.OpenKey(registry.LOCAL_MACHINE, item.path, registry.READ)
		switch {
		case err == nil:
			_ = key.Close()
			fixer.offsys.driverDatabaseType = item.typ
			logger.Debugf("detectDriverDatabaseType: %v", item.typ)

			if fixer.offsys.driverDatabaseType == drvDbLegacy {
				fixer.infof(LogTplForPrintDriverDatabaseLegacyWith0Args)
			} else if fixer.offsys.driverDatabaseType == drvDbDriverStore {
				fixer.infof(LogTplForPrintDriverDatabasePnpWith0Args)
			}

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
	fixer.infof(LogTplForPrintSystemBootTypeWith1Args, fixer.offsys.bootMode)
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
	offlineSoftwareKey := offlineSoftwareKeyName

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

	fixer.infof(LogTplForPrintDistroWith1Args, fixer.offsys.windowsVersion)

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

	if err = key.SetDWordValue("ArpRetryCount", 0); err != nil {
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

	fixer.infof(LogTplForDisableAutoRebootWith0Args)

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
		`%s /Image:%s:\ /Get-Drivers`,
		fixer.getDismProgram(),
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
		`%s /Image:%s:\ /Add-Driver /Driver:%s /Recurse`,
		fixer.getDismProgram(),
		fixer.offsys.sysVolumeLtr,
		ds.Dir,
	)
	// injectCmd += " /ForceUnsigned"

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

// viostorCompatIds / vioscsiCompatIds 是 virtio 块设备的 PCI 兼容硬件 ID
// （参见 virt-v2v windows_virtio.ml）。legacy 系统与 modern 设备的
// Device ID 不同，需分别注册：
//
//   - viostor:  legacy  1001 / 1100
//   - viostor:  modern  1041 (transitional) / 1042
//   - vioscsi:  legacy  1004
//   - vioscsi:  modern  1048
var (
	viostorCompatIds = []string{
		`PCI\VEN_1AF4&DEV_1001&SUBSYS_00021AF4&REV_00`,
		//`PCI\VEN_1AF4&DEV_1100&SUBSYS_00021AF4&REV_01`,
		//`PCI\VEN_1AF4&DEV_1041&SUBSYS_11001AF4&REV_01`,
		`PCI\VEN_1AF4&DEV_1042&SUBSYS_11001AF4&REV_01`,
		//`PCI\VEN_1AF4&DEV_1001&SUBSYS_00021AF4`,
		//`PCI\VEN_1AF4&DEV_1100&SUBSYS_00021AF4`,
		//`PCI\VEN_1AF4&DEV_1041&SUBSYS_11001AF4`,
		//`PCI\VEN_1AF4&DEV_1042&SUBSYS_11001AF4`,
		//`PCI\VEN_1AF4&DEV_1001`,
		//`PCI\VEN_1AF4&DEV_1100`,
		//`PCI\VEN_1AF4&DEV_1041`,
		//`PCI\VEN_1AF4&DEV_1042`,
	}

	vioscsiCompatIds = []string{
		`PCI\VEN_1AF4&DEV_1004&SUBSYS_00081AF4&REV_00`,
		`PCI\VEN_1AF4&DEV_1048&SUBSYS_11001AF4&REV_01`,
		//`PCI\VEN_1AF4&DEV_1004`,
		//`PCI\VEN_1AF4&DEV_1048`,
	}
)

const scsiMiniportGroup = "SCSI miniport"

const (
	// classGuidSCSIAdapter SCSI 适配器设备类 GUID（存储类驱动）
	classGuidSCSIAdapter = "{4D36E97B-E325-11CE-BFC1-08002BE10318}"
	// classGuidNet 网络设备类 GUID
	classGuidNet = "{4D36E972-E325-11CE-BFC1-08002BE10318}"
	// classGuidHDC IDE/ATA 控制器设备类 GUID（intelide 等）
	classGuidHDC = "{4D36E96A-E325-11CE-BFC1-08002BE10318}"
)

// injectDriversLegacy 为旧版 Windows（win2k/winxp/win2k3/winvista/win2k8，
// 即 < Win7/2008R2）注入驱动库中的虚拟化驱动。
//
// 旧系统不支持 DISM /Add-Driver，也不存在 DriverStore（DriverDatabase
// 从 Win8 起使用，Vista 的 CDB 仍有效）。实现参考 virt-v2v 对
// XP/2003/Vista 的处理：
//
//  1. 将驱动模块（.sys 及同目录的 .inf/.cat/.pnf 配套文件）复制到离线系统的
//     System32\Drivers 与 Inf 目录；
//  2. 在 SYSTEM hive 的 CriticalDeviceDatabase 中登记启动关键设备的
//     PCI 兼容 ID 与对应驱动服务；
//  3. 创建/更新驱动服务项：Type=1（内核驱动）、Start=0（boot start）、
//     ErrorControl=1、ImagePath=system32\drivers\<drv>.sys，块驱动另
//     设置 Group="SCSI miniport" 以保证在磁盘子系统初始化前加载。
//
// 块驱动（viostor/vioscsi）必须做 boot start 注册才能引导；
// 其余驱动（如 netkvm）随系统启动后由 PnP 管理器安装。
//
// 返回值（blockDrv, netDrv）表示实际完成注册的块驱动服务名与网络驱动，
// 供调用方决定宿主机磁盘总线与网卡类型；块驱动为空表示无可用驱动，
// 恢复后应使用 IDE 等模拟磁盘启动。
func (fixer *windowsSystemFixer) injectDriversLegacy(ds *x2xlib.DriverResource) (blockDrv, netDrv string, err error) {
	logger.Debugf("injectDriversLegacy: begin, dir=%s modules=%v", ds.Dir, ds.Modules)
	defer logger.Debugf("injectDriversLegacy: end")

	fixer.infof(LogTplForInjectLegacyDriversWith0Args)

	const (
		moduleViostor = "viostor"
		moduleVioscsi = "vioscsi"
		moduleNetkvm  = "netkvm"
		scsiClassGuid = "{4D36E97B-E325-11CE-BFC1-08002BE10318}" // SCSI adapter 类 GUID
	)

	// ------------------------------------------------------------------
	// 1. 拷贝驱动文件
	// ------------------------------------------------------------------

	sysRoot := fixer.offsys.sysVolumeLtr + `:\`
	driversDir := filepath.Join(sysRoot, "Windows", "System32", "drivers")
	infDir := filepath.Join(sysRoot, "Windows", "inf")

	files, e := os.ReadDir(ds.Dir)
	if e != nil {
		return "", "", errors.Wrapf(e, "read driver dir %s", ds.Dir)
	}

	// legacySysFiles 记录每个模块实际复制成功的 .sys 文件名，
	// 供后续注册表写入使用。
	legacySysFiles := make(map[string]string)

	for _, f := range files {
		if f.IsDir() {
			continue
		}

		name := f.Name()
		ext := strings.ToLower(filepath.Ext(name))
		module := strings.TrimSuffix(strings.ToLower(name), ext)

		// 仅拷贝驱动库中与本驱动集模块相关的文件
		matched := false
		for _, m := range ds.Modules {
			if strings.EqualFold(m, module) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}

		src := filepath.Join(ds.Dir, name)

		var dst string
		switch ext {
		case ".sys":
			dst = filepath.Join(driversDir, name)
			legacySysFiles[module] = name
		case ".inf", ".cat", ".pnf":
			dst = filepath.Join(infDir, name)
		default:
			// .pdb 等调试文件无需注入
			logger.Debugf("injectDriversLegacy: skip %s", name)
			continue
		}

		if e = extend.CopyFile(src, dst, 0o666); e != nil {
			return "", "", errors.Wrapf(e, "copy %s -> %s", src, dst)
		}
		equal, e := extend.FileEqual(src, dst)
		if e != nil {
			return "", "", errors.Wrapf(e, "same md5sum %s -> %s", src, dst)
		}
		if !equal {
			return "", "", errors.Errorf("md5 mismatch, src:%s, dst:%s", src, dst)
		}
		logger.Debugf("injectDriversLegacy: copied %s", dst)
	}

	// ------------------------------------------------------------------
	// 2. 注册表注入
	// ------------------------------------------------------------------

	isBlockModule := map[string]bool{
		moduleViostor: true,
		moduleVioscsi: true,
	}

	for _, module := range ds.Modules {
		sysFile, ok := legacySysFiles[module]
		if !ok {
			// 模块没有对应的 .sys（例如纯 INF 驱动），跳过服务注册
			logger.Debugf("injectDriversLegacy: module %s has no sys file, skip", module)
			continue
		}

		// 块驱动必须为 boot start（Start=0）；网络等非引导驱动
		// 注册为按需启动（Start=3），由 PnP 管理器在首次启动时安装。
		if e := fixer.installLegacyService(module, sysFile, isBlockModule[module]); e != nil {
			return "", "", e
		}
	}

	// 块驱动：在 CDB 中登记 PCI 兼容 ID，保证引导阶段可加载
	for _, module := range []string{moduleViostor, moduleVioscsi} {
		if _, ok := legacySysFiles[module]; !ok {
			continue
		}

		compatIds := viostorCompatIds
		if module == moduleVioscsi {
			compatIds = vioscsiCompatIds
		}

		if e := fixer.registerCriticalDeviceDatabase(module, scsiClassGuid, compatIds); e != nil {
			return "", "", e
		}
		blockDrv = module
	}

	// 网络驱动
	if _, ok := legacySysFiles[moduleNetkvm]; ok {
		netDrv = moduleNetkvm
	}

	fixer.offsys.legacyBlockDriver = blockDrv
	fixer.offsys.legacyNetDriver = netDrv != ""

	logger.Infof(
		"injectDriversLegacy: done, block=%q net=%q",
		blockDrv,
		netDrv,
	)

	return blockDrv, netDrv, nil
}

// installLegacyService 在离线 SYSTEM hive 中创建/更新内核驱动服务项。
//
// bootCritical 为 true（块存储驱动）时：
//
//	Type         = SERVICE_KERNEL_DRIVER (1)
//	Start        = SERVICE_BOOT_START   (0)
//	ErrorControl = SERVICE_ERROR_NORMAL (1)
//	Group        = SCSI miniport
//
// 同时写入对应 INF 中的 Parameters 配置：
//
//	Parameters\BusType
//	Parameters\PnpInterface\5
//
// bootCritical 为 false 时：
//
//	Start = SERVICE_DEMAND_START (3)
//	不设置启动加载组及 boot-critical 参数。
func (fixer *windowsSystemFixer) installLegacyService(
	serviceName, sysFileName string,
	bootCritical bool,
) error {
	servicesRegPath := fmt.Sprintf(
		"%s\\ControlSet%03d\\Services",
		fixer.offsys.registryRootKey,
		fixer.offsys.currentControlSet,
	)

	svcKey, _, err := registry.CreateKey(
		registry.LOCAL_MACHINE,
		servicesRegPath+"\\"+serviceName,
		registry.ALL_ACCESS,
	)
	if err != nil {
		return errors.Wrapf(err, "create service key %s", serviceName)
	}
	defer svcKey.Close()

	// Type: SERVICE_KERNEL_DRIVER = 1
	if err := svcKey.SetDWordValue("Type", 1); err != nil {
		return errors.Wrap(err, "set Type")
	}

	// Start:
	//   SERVICE_BOOT_START   = 0
	//   SERVICE_DEMAND_START = 3
	start := uint32(3)
	if bootCritical {
		start = 0
	}
	if err := svcKey.SetDWordValue("Start", start); err != nil {
		return errors.Wrap(err, "set Start")
	}

	// ErrorControl: SERVICE_ERROR_NORMAL = 1
	if err := svcKey.SetDWordValue("ErrorControl", 1); err != nil {
		return errors.Wrap(err, "set ErrorControl")
	}

	// ServiceBinary = %12%\viostor.sys
	//
	// %12% 对应 %SystemRoot%\System32\drivers。
	// 使用 REG_EXPAND_SZ，与 INF 中的 ServiceBinary 保持一致。
	imagePath := fmt.Sprintf(`system32\drivers\%s`, sysFileName)
	if err := svcKey.SetExpandStringValue("ImagePath", imagePath); err != nil {
		return errors.Wrap(err, "set ImagePath")
	}

	if bootCritical {
		// LoadOrderGroup = SCSI miniport
		if err := svcKey.SetStringValue("Group", "SCSI miniport"); err != nil {
			return errors.Wrap(err, "set Group")
		}

		busType := uint32(1)
		if serviceName == "vioscsi" {
			busType = 0xA
		}

		// Services\<service>\Parameters
		paramKey, _, err := registry.CreateKey(svcKey, "Parameters", registry.ALL_ACCESS)
		if err != nil {
			return errors.Wrap(err, "create Parameters key")
		}
		defer paramKey.Close()

		if err := paramKey.SetDWordValue("BusType", busType); err != nil {
			return errors.Wrap(err, "set Parameters\\BusType")
		}

		// Services\<service>\Parameters\PnpInterface
		pnpKey, _, err := registry.CreateKey(paramKey, "PnpInterface", registry.ALL_ACCESS)
		if err != nil {
			return errors.Wrap(err, "create PnpInterface key")
		}
		defer pnpKey.Close()

		if err := pnpKey.SetDWordValue("5", 1); err != nil {
			return errors.Wrap(err, "set Parameters\\PnpInterface\\5")
		}
	}

	logger.Debugf(
		"installLegacyService: service %s registered "+
			"(ImagePath=%s, Start=%d, bootCritical=%v)",
		serviceName,
		imagePath,
		start,
		bootCritical,
	)

	return nil
}

// ensureLegacyBootDriver 使指定服务名的引导关键驱动在离线系统中可用，
// 并为设备登记 CriticalDeviceDatabase 记录：
//
//  1. 驱动服务已存在时，仅将其设置为 Start=0（boot start）；
//  2. 服务不存在时，校验 sysFileName 位于 system32\drivers 后
//     创建服务键：Type=1、Start=0、ErrorControl=1、
//     ImagePath=system32\drivers\<sysFileName>，loadOrderGroup
//     非空时另设置加载组；
//  3. 在 CriticalDeviceDatabase 中为设备的每个兼容/硬件 ID
//     创建 Service 与 ClassGUID 记录，保证引导阶段可识别设备
//     并加载驱动。
//
// 适用于系统自带的引导驱动（如 intelide.sys）离线修复场景。
func (fixer *windowsSystemFixer) ensureLegacyBootDriver(
	serviceName, sysFileName, loadOrderGroup, classGuid string,
	deviceIds []string,
) error {
	// 1)/2) 服务注册
	if fixer.existedService(serviceName) {
		if err := fixer.enableService(serviceName); err != nil {
			return errors.Wrapf(err, "enable service %s", serviceName)
		}
	} else {
		sysPath := filepath.Join(
			fixer.offsys.sysVolumeLtr+":\\",
			"Windows", "System32", "drivers", sysFileName)
		if !extend.IsExisted(sysPath) {
			return errors.Errorf("driver file not found: %s", sysPath)
		}

		servicesRegPath := fmt.Sprintf(
			"%s\\ControlSet%03d\\Services",
			fixer.offsys.registryRootKey,
			fixer.offsys.currentControlSet,
		)

		svcKey, _, err := registry.CreateKey(
			registry.LOCAL_MACHINE,
			servicesRegPath+"\\"+serviceName,
			registry.ALL_ACCESS,
		)
		if err != nil {
			return errors.Wrapf(err, "create service key %s", serviceName)
		}

		// Type: SERVICE_KERNEL_DRIVER = 1
		if err = svcKey.SetDWordValue("Type", 1); err != nil {
			svcKey.Close()
			return errors.Wrap(err, "set Type")
		}
		// Start: SERVICE_BOOT_START = 0
		if err = svcKey.SetDWordValue("Start", 0); err != nil {
			svcKey.Close()
			return errors.Wrap(err, "set Start")
		}
		// ErrorControl: SERVICE_ERROR_NORMAL = 1
		if err = svcKey.SetDWordValue("ErrorControl", 1); err != nil {
			svcKey.Close()
			return errors.Wrap(err, "set ErrorControl")
		}

		imagePath := fmt.Sprintf(`system32\drivers\%s`, sysFileName)
		if err = svcKey.SetExpandStringValue("ImagePath", imagePath); err != nil {
			svcKey.Close()
			return errors.Wrap(err, "set ImagePath")
		}

		if loadOrderGroup != "" {
			if err = svcKey.SetStringValue("Group", loadOrderGroup); err != nil {
				svcKey.Close()
				return errors.Wrap(err, "set Group")
			}
		}
		svcKey.Close()

		logger.Debugf(
			"ensureLegacyBootDriver: service %s created (ImagePath=%s, Group=%s)",
			serviceName, imagePath, loadOrderGroup)
	}

	// 3) 登记 CDB 记录
	return fixer.registerCriticalDeviceDatabase(serviceName, classGuid, deviceIds)
}

// cdbKeyName 将 Windows PCI 设备兼容 ID 转换为
// CriticalDeviceDatabase 的注册表键名。
//
// Windows PnP 的设备实例路径中字段之间使用 "&" 分隔
// （如 PCI\VEN_8086&DEV_7010），仅枚举器前缀与首个字段之间
// 使用 "\"，在 CDB 键名中统一替换为 "#"：
//
//	PCI\VEN_8086&DEV_7010 -> pci#ven_8086&dev_7010
//
// 兼容 ID 若以 "\" 分隔各字段（如 PCI\VEN_8086\DEV_7010），
// 第一个 "\" 替换为 "#"，其余 "\" 替换为 "&"。
func cdbKeyName(compatID string) string {
	id := strings.ToLower(strings.TrimSpace(compatID))
	if id == "" {
		return ""
	}

	i := strings.Index(id, `\`)
	if i < 0 {
		return id
	}

	return id[:i] + "#" + strings.ReplaceAll(id[i+1:], `\`, "&")
}

// registerCriticalDeviceDatabase 在 CriticalDeviceDatabase 中为每个
// PCI 兼容 ID 创建条目（HKLM\SYSTEM\<CCS>\Control\CriticalDeviceDatabase\PCI#...），
// 值为 Service（驱动服务名）与 ClassGUID。这是旧版 Windows 引导阶段
// 识别启动关键设备并加载对应驱动的机制。
func (fixer *windowsSystemFixer) registerCriticalDeviceDatabase(serviceName, classGuid string, compatIds []string) error {
	logger.Debugf("registerCriticalDeviceDatabase: service=%s, class=%s, compatIds=\n%s",
		serviceName, classGuid, strings.Join(compatIds, "\n"))

	cdbRoot := fmt.Sprintf(
		"%s\\ControlSet00%d\\Control\\CriticalDeviceDatabase",
		fixer.offsys.registryRootKey,
		fixer.offsys.currentControlSet,
	)

	for _, compatID := range compatIds {
		keyName := cdbKeyName(compatID)
		if keyName == "" {
			continue
		}
		keyPath := cdbRoot + `\` + keyName

		key, _, err := registry.CreateKey(
			registry.LOCAL_MACHINE,
			keyPath,
			registry.ALL_ACCESS,
		)
		if err != nil {
			return errors.Wrapf(err, "create CDB key %s", keyName)
		}

		if err = key.SetStringValue("Service", serviceName); err != nil {
			key.Close()
			return errors.Wrap(err, "set Service")
		}
		if err = key.SetStringValue("ClassGUID", classGuid); err != nil {
			key.Close()
			return errors.Wrap(err, "set ClassGUID")
		}
		key.Close()

		logger.Debugf(
			"registerCriticalDeviceDatabase: %s -> %s",
			keyName,
			serviceName,
		)
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
		"iaStorAC",
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

	fixer.infof(LogTplForOptimizeUEFIWith0Args)

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
			logger.Debugf("UEFI fallback bootloader already exists")
			return nil
		}
	}

	logger.Infof("Fixing UEFI fallback bootloader")

	// virt-v2v 的行为：删除整个 EFI\Boot
	if err := os.RemoveAll(bootDir); err != nil {
		return errors.Wrap(err, "remove EFI\\Boot")
	}

	if err := os.MkdirAll(bootDir, 0755); err != nil {
		return errors.Wrap(err, "create EFI\\Boot")
	}

	if err := extend.CopyFile(bootmgfw, fallback, 0755); err != nil {
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

	fixer.infof(LogTplForOptimizeBCDWith0Args)

	bcdPath := filepath.Join(
		fixer.offsys.efiVolumeLtr+":\\",
		"EFI", "Microsoft", "Boot", "BCD",
	)

	if !extend.IsExisted(bcdPath) {
		logger.Debugf("BCD not found: %s", bcdPath)
		return nil
	}

	logger.Infof("Fixing Windows BCD")

	const hiveName = `OFFLINEBCDH0NK1`
	regRoot := hiveName

	// 防御性清理:如果上次运行异常退出导致 hive 残留挂载,
	// 这里先尝试卸载一次,避免 loadReg 因为 key 已存在而失败。
	if err := unloadReg(regRoot); err != nil {
		logger.Debugf("pre-clean unload (ignorable): %v", err)
	}

	if err := loadReg(regRoot, bcdPath); err != nil {
		return errors.Wrapf(err, "load BCD hive: %s", bcdPath)
	}
	defer func() {
		if err := unloadReg(regRoot); err != nil {
			logger.Warnf("Unload BCD hive failed: %v", err)
		}
	}()

	currentGuid, err := getDefaultBootEntryGuid(hiveName)
	if err != nil {
		return errors.Wrap(err, "get default boot entry guid")
	}
	if currentGuid == "" {
		// 未找到默认启动项属于正常情况(比如非标准 BCD),不视为错误
		logger.Debugf("BCD default boot GUID not found")
		return nil
	}

	if err := removeGraphicsModeDisabled(hiveName, currentGuid); err != nil {
		return errors.Wrap(err, "delete graphicsmodedisabled")
	}

	logger.Infof("Removed BCD graphicsmodedisabled option")
	return nil
}

// getDefaultBootEntryGuid 读取 BCD 中 bootmgr 的默认启动项 GUID。
// 返回空字符串且 err == nil 表示"找不到但不算错误";
// 返回非 nil err 表示读取过程本身出了问题(权限、句柄等)。
func getDefaultBootEntryGuid(hiveName string) (string, error) {
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
		if errors.Is(err, registry.ErrNotExist) {
			logger.Debugf("BCD default boot entry not found")
			return "", nil
		}
		return "", err // 真正的错误,不要吞掉
	}
	defer k.Close()

	guid, _, err := k.GetStringValue("Element")
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return "", nil
		}
		return "", err
	}

	return strings.TrimSpace(guid), nil
}

// removeGraphicsModeDisabled 删除指定启动项下的 graphicsmodedisabled 元素。
func removeGraphicsModeDisabled(hiveName, entryGuid string) error {
	graphicsKey := hiveName +
		`\Objects\` +
		entryGuid +
		`\Elements\16000046`

	return deleteRegistryTree(registry.LOCAL_MACHINE, graphicsKey)
}

// ntVersion 返回离线系统的 Windows NT 版本号
// （如 NT61 表示 Win7/2008R2）。未知版本返回错误。
func (fixer *windowsSystemFixer) ntVersion() (define.NTVersion, error) {
	ntVer, ok := define.OsNTVersion[fixer.offsys.windowsVersion]
	if !ok {
		return define.NTUnknown, errors.New("not supported windows version")
	}
	return ntVer, nil
}

// isModernWindows 判断离线系统是否为现代版 Windows
// （>= Win7/2008R2），支持 DISM 离线注入、DriverStore 与
// firstboot 配置服务。
func (fixer *windowsSystemFixer) isModernWindows() (bool, error) {
	ntVer, e := fixer.ntVersion()
	if e != nil {
		return false, e
	}
	return ntVer >= define.NT61, nil
}

// isLegacyWindows 判断离线系统是否为旧版 Windows
// （< Win7/2008R2），需走 CriticalDeviceDatabase 等传统注入方式。
func (fixer *windowsSystemFixer) isLegacyWindows() (bool, error) {
	modern, e := fixer.isModernWindows()
	if e != nil {
		return false, e
	}
	return !modern, nil
}

// probeLegacyKvmDrivers 探测旧系统当前可用的 KVM virtio 驱动，
// 返回可用的块驱动服务名（viostor/vioscsi，空表示不可用）
// 与网络驱动是否可用。
//
// 优先使用本次修复注入的结果；当修复流程被跳过时（如源/目标
// 虚拟化平台相同），退化为检查离线系统中已注册的驱动服务。
func (fixer *windowsSystemFixer) probeLegacyKvmDrivers() (blockDrv string, netDrv bool) {
	blockDrv = fixer.offsys.legacyBlockDriver
	if blockDrv == "" {
		for _, m := range []string{"viostor", "vioscsi"} {
			if fixer.existedService(m) {
				blockDrv = m
				break
			}
		}
	}

	netDrv = fixer.offsys.legacyNetDriver
	if !netDrv {
		netDrv = fixer.existedService("netkvm")
	}

	return blockDrv, netDrv
}

func (fixer *windowsSystemFixer) fixNtfsHeads() error {
	logger.Debugf("fixNtfsHeads: ++")
	defer logger.Debugf("fixNtfsHeads: --")

	// TODO 未实现

	return nil
}

// injectConfigService 向离线系统注入配置程序。
//
// drfirstboot.exe 仅面向 Win7/2008R2（NT61）及以上系统；
// 更老的系统（winxp/win2k3/winvista/win2k8）不支持该服务，
// 仅写入网络配置文件无意义，因此直接跳过。
func (fixer *windowsSystemFixer) injectConfigService() error {
	logger.Debugf("injectConfigService: ++")
	defer logger.Debugf("injectConfigService: --")

	yes, err := fixer.isModernWindows()
	if err != nil {
		return errors.Wrap(err, "check nt version")
	}
	if !yes {
		fixer.warnf(LogTplForSkipFirstBootServiceWith1Args, fixer.offsys.windowsVersion)
		return nil
	}

	const serviceName = "drfirstboot"
	fixer.infof(LogTplForInjectFirstBootServiceWith1Args, serviceName)

	serviceTgtExePath := fmt.Sprintf("%s:\\Windows\\%s.exe", fixer.offsys.sysVolumeLtr, serviceName)

	serviceSrcExePath := filepath.Join(
		fixer.opts.RecoveryParam.X2xLibrary,
		"extra",
		"firstboot",
		serviceName,
		"windows",
		fixer.opts.RecoveryParam.Source.Arch,
		serviceName+".exe",
	)

	// 源文件必须存在
	if _, err = os.Stat(serviceSrcExePath); err != nil {
		return errors.Wrapf(err, "source exe not found: %s", serviceSrcExePath)
	}

	servicesRegPath := fmt.Sprintf(
		"%s\\ControlSet00%d\\Services",
		fixer.offsys.registryRootKey,
		fixer.offsys.currentControlSet,
	)
	logger.Debugf("injectConfigService: servicesRegPath: %s", servicesRegPath)

	servicesKey, err := registry.OpenKey(registry.LOCAL_MACHINE, servicesRegPath, registry.ALL_ACCESS)
	if err != nil {
		return errors.Wrap(err, "open services registry key")
	}
	defer servicesKey.Close()

	subKeys, err := servicesKey.ReadSubKeyNames(-1)
	if err != nil {
		return errors.Wrap(err, "read services subkeys")
	}

	serviceInstalled := false
	for _, s := range subKeys {
		if strings.EqualFold(s, serviceName) {
			serviceInstalled = true
			break
		}
	}

	// 重置标记文件
	firstBootProcFlag := fmt.Sprintf("%s:\\Windows\\%s*", fixer.offsys.sysVolumeLtr, FirstBootProcFilePrefix)
	files, e := filepath.Glob(firstBootProcFlag)
	if e != nil {
		logger.Warnf("injectConfigService: failed to glob %s", firstBootProcFlag)
	} else {
		for _, file := range files {
			_ = os.RemoveAll(file)
			logger.Debugf("injectConfigService: removed %s", file)
		}
	}

	// 1. 拷贝可执行文件到目标系统（无论是否已安装服务，都刷新一遍文件，保证版本一致）
	if err = extend.CopyFile(serviceSrcExePath, serviceTgtExePath, 0o666); err != nil {
		return errors.Wrapf(err, "copy service exe to %s", serviceTgtExePath)
	}

	if serviceInstalled {
		logger.Debugf("injectConfigService: service %s already installed, skip registry creation", serviceName)
		return nil
	}

	// 2. 创建服务注册表项
	svcKey, _, err := registry.CreateKey(registry.LOCAL_MACHINE, servicesRegPath+"\\"+serviceName, registry.ALL_ACCESS)
	if err != nil {
		return errors.Wrapf(err, "create service key %s", serviceName)
	}
	defer svcKey.Close()

	// ImagePath 使用离线系统盘符对应的运行时路径（如 C:\Windows\drfirstboot.exe）
	imagePath := fmt.Sprintf("C:\\Windows\\%s.exe", serviceName)
	if err = svcKey.SetExpandStringValue("ImagePath", imagePath); err != nil {
		return errors.Wrap(err, "set ImagePath")
	}
	logger.Debugf("injectConfigService: ImagePath: %s", imagePath)

	if err = svcKey.SetStringValue("DisplayName", serviceName); err != nil {
		return errors.Wrap(err, "set DisplayName")
	}
	// Type: 0x10 = SERVICE_WIN32_OWN_PROCESS
	if err = svcKey.SetDWordValue("Type", 0x10); err != nil {
		return errors.Wrap(err, "set Type")
	}
	// Start: 2 = SERVICE_AUTO_START
	if err = svcKey.SetDWordValue("Start", 2); err != nil {
		return errors.Wrap(err, "set Start")
	}
	// ErrorControl: 1 = SERVICE_ERROR_NORMAL
	if err = svcKey.SetDWordValue("ErrorControl", 1); err != nil {
		return errors.Wrap(err, "set ErrorControl")
	}
	if err = svcKey.SetStringValue("ObjectName", "LocalSystem"); err != nil {
		return errors.Wrap(err, "set ObjectName")
	}

	logger.Debugf("injectConfigService: service %s created", serviceName)
	return nil
}

func (fixer *windowsSystemFixer) injectNetworkConfig() error {
	logger.Debugf("injectNetworkConfig: ++")
	defer logger.Debugf("injectNetworkConfig: --")

	if !fixer.opts.RecoveryParam.Network.Enable {
		logger.Debugf("injectNetworkConfig: network config disabled")
		return nil
	}

	// 网络配置由 drfirstboot 服务在首次启动时应用，
	// 该服务不支持低于 NT61 的旧系统，因此旧系统无需写入配置文件。
	yes, err := fixer.isModernWindows()
	if err != nil {
		return errors.Wrap(err, "check nt version")
	}
	if !yes {
		logger.Debugf("injectNetworkConfig: skip for old system %s", fixer.offsys.windowsVersion)
		return nil
	}

	fixer.infof(LogTplForInjectNetworkConfigWith0Args)

	netCfgPath := fmt.Sprintf("%s:\\Windows\\%s", fixer.offsys.sysVolumeLtr, FirstBootProcNetworkConfigFileName)
	_ = os.RemoveAll(netCfgPath)
	data, _ := json.Marshal(fixer.opts.RecoveryParam.Network)

	return os.WriteFile(netCfgPath, data, 0644)
}

func (fixer *windowsSystemFixer) getDismProgram() string {
	return "dism.exe"
}

func (fixer *windowsSystemFixer) logf(level LogLevel, tpl LangTpl, v ...interface{}) {
	le := LogEntry{
		Level: level,
		MsgEn: fmt.Sprintf(tpl.En, v...),
		MsgZh: fmt.Sprintf(tpl.Zh, v...),
	}

	if fixer.opts.InRepairVM {
		_ = WriteSerialMessageTypeRepairLog(fixer.reqPort, le)
		return
	}

	fixer.logs <- le

	le.Println()
}

func (fixer *windowsSystemFixer) infof(tpl LangTpl, v ...interface{}) {
	fixer.logf(LogInfo, tpl, v...)
}

func (fixer *windowsSystemFixer) warnf(tpl LangTpl, v ...interface{}) {
	fixer.logf(LogWarn, tpl, v...)
}

func (fixer *windowsSystemFixer) errorf(tpl LangTpl, v ...interface{}) {
	fixer.logf(LogError, tpl, v...)
}

func (fixer *windowsSystemFixer) sync() {
	_, _, _ = command.Execute("sync.exe /accepteula", command.WithDebug())
}
