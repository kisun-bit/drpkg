package x2xcore

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/ps/bus/pci/universal"
	"github.com/kisun-bit/drpkg/ps/recovery/x2xlib"
	"github.com/pkg/errors"
	"golang.org/x/sys/windows/registry"
)

func (fixer *windowsSystemFixer) unconfigBareMetal() error {
	logger.Debugf("unconfigBareMetal: ++")
	defer logger.Debugf("unconfigBareMetal: --")

	logger.Debugf("unconfigBareMetal: do nothing")

	return nil
}

func (fixer *windowsSystemFixer) configBareMetal() error {
	logger.Debugf("configBareMetal: ++")
	defer logger.Debugf("configBareMetal: --")

	// 遍历离线系统 Windows\Inf 目录下的所有 INF，
	// 重建 服务名/设备类 → PCI 硬件 ID 集合 的索引（fixer.offsys.infMaps）。
	if err := fixer.buildInfMaps(); err != nil {
		return errors.Wrap(err, "build inf maps")
	}

	for _, p := range fixer.opts.RecoveryParam.Target.PciList {

		fixer.infof(LogTplForMatchDriverWith1Args, p)

		up, e := universal.UniPciFromString(p)
		if e != nil {
			return e
		}

		logger.Debugf("configBareMetal: \npci: %s\nhardwareIds:\n%s",
			p,
			extend.Pretty(up.MsHardwareId()))

		//
		// 根据目标机器的 PCI 设备，检查离线 Windows 对该硬件的支持情况：
		//
		// 1. 驱动已安装并可启动。
		// 2. 驱动已安装，但未配置为启动驱动，需要调整 Start。
		// 3. 驱动文件已存在于 DriverStore，但尚未安装（缺少 Service），
		//    需要重新安装该驱动。
		// 4. 系统中不存在兼容驱动，需要注入新驱动。
		//

		var err error

		switch fixer.offsys.driverDatabaseType {
		case drvDbDriverStore:
			err = fixer.checkPciInDriverStore(up)
		case drvDbLegacy:
			err = fixer.checkPciInDriverStoreLegacy(up)
		default:
			err = errors.New("Unsupported driver database")
		}

		if err == nil {
			fixer.infof(LogTplForMatchDriverSuccessWith1Args, p)
			continue
		}

		if up.BaseClassId() == 0x01 {
			fixer.errorf(LogTplForIncompatibleBootPCIWith2Args, p, up.Human())
			return errors.Wrapf(err, "incompatible pci(%s): %s", up, up.MsHardwareId()[0])
		}
		fixer.warnf(LogTplForIncompatibleNonBootPCIWith2Args, p, up.Human())
	}

	return nil
}

func (fixer *windowsSystemFixer) checkPciInDriverStore(up *universal.UniPci) error {
	logger.Debugf("checkPciInDriverStore: checking %s", up)
	defer logger.Debugf("checkPciInDriverStore: done")

	//
	// Find matching INF from DriverDatabase\DeviceIds
	//
	deviceIDsPath := filepath.Join(
		fixer.offsys.registryRootKey,
		"DriverDatabase",
		"DeviceIds",
	)

	var infName string

	msCompatibleIds := up.MsCompatibleId()
	logger.Debugf("checkPciInDriverStore: \nupci: %s\nmsCompatibleIds: %s\n",
		up,
		extend.Pretty(msCompatibleIds))

	for _, compatID := range up.MsCompatibleId() {

		keyPath := filepath.Join(deviceIDsPath, compatID)

		key, err := registry.OpenKey(
			registry.LOCAL_MACHINE,
			keyPath,
			registry.QUERY_VALUE|registry.READ,
		)
		if err != nil {
			logger.Debugf("checkPciInDriverStore: DeviceId %s not found", compatID)
			continue
		}

		logger.Debugf("checkPciInDriverStore: DeviceId %s found, key is %s", compatID, keyPath)

		valueNames, err := key.ReadValueNames(-1)
		key.Close()
		if err != nil {
			logger.Warnf("checkPciInDriverStore: failed to enumerate %s: %v", keyPath, err)
			continue
		}

		for _, value := range valueNames {
			if strings.HasSuffix(strings.ToLower(value), ".inf") {
				infName = value
				break
			}
		}

		if infName != "" {
			logger.Debugf("checkPciInDriverStore: matched INF %s", infName)
			break
		}
	}

	if infName == "" {
		logger.Debug("checkPciInDriverStore: no matching driver found")

		//
		// Query driver db
		//
		ds, e := fixer.x2xLib.SelectWindowsBestNormalDriver(
			fixer.opts.RecoveryParam.Source.Arch,
			fixer.offsys.windowsVersion,
			up.String(),
			false)
		if e != nil {
			return e
		}

		if e = fixer.injectDriversByDism(ds); e != nil {
			return e
		}

		return nil
	}

	logger.Debugf("checkPciInDriverStore: matching %s => %s", up, infName)

	//
	// Find active package
	//
	infKeyPath := filepath.Join(
		fixer.offsys.registryRootKey,
		"DriverDatabase",
		"DriverInfFiles",
		infName,
	)

	infKey, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		infKeyPath,
		registry.QUERY_VALUE|registry.READ,
	)
	if err != nil {
		logger.Warnf("checkPciInDriverStore: failed to open %s: %v", infKeyPath, err)
		return fmt.Errorf("open DriverInfFiles: %v", err)
	}
	defer infKey.Close()

	pkgIDs, _, err := infKey.GetStringsValue("Active")
	if err != nil {
		logger.Warnf("checkPciInDriverStore: Active value missing: %v", err)
		return fmt.Errorf("read Active packages: %v", err)
	}
	logger.Debugf("checkPciInDriverStore: found %d packages, details:\n%s", len(pkgIDs), extend.Pretty(pkgIDs))

	//
	// Non-storage devices only need to verify existence.
	//
	if up.BaseClassId() != 0x01 {
		logger.Debug("checkPciInDriverStore: non-storage device")
		return nil
	}

	var processed bool

	for _, pkgID := range pkgIDs {

		infDir := filepath.Join(
			fixer.offsys.sysVolumeLtr+":\\",
			"Windows",
			"System32",
			"DriverStore",
			"FileRepository",
			pkgID,
		)

		if extend.IsEmptyDir(infDir) {
			logger.Warnf("checkPciInDriverStore: package %s missing", pkgID)
			continue
		}

		entries, err := os.ReadDir(infDir)
		if err != nil {
			logger.Warnf("checkPciInDriverStore: failed to read %s: %v", infDir, err)
			continue
		}

		for _, entry := range entries {

			if entry.IsDir() {
				continue
			}

			if !strings.HasSuffix(strings.ToLower(entry.Name()), ".inf") {
				continue
			}

			infPath := filepath.Join(infDir, entry.Name())

			infObj, err := ParseINF(infPath)
			if err != nil {
				logger.Warnf("checkPciInDriverStore: failed to parse %s: %v", infPath, err)
				continue
			}

			for _, svc := range infObj.ServiceNames() {

				if !fixer.existedService(svc) {
					logger.Debugf("checkPciInDriverStore: service %s not found", svc)
					continue
				}

				if err := fixer.enableService(svc); err != nil {
					return fmt.Errorf("enable service %s: %v", svc, err)
				}

				logger.Infof("checkPciInDriverStore: enabled service %s", svc)
			}

			processed = true
		}
	}

	if !processed {
		return ErrDeviceNotSupported
	}

	logger.Debug("checkPciInDriverStore: driver is available")

	return nil
}

// intelIDEPCIIDs 是 intelide.sys 支持的 Intel IDE 控制器 PCI ID 集合。
// 键使用 CDB 键名形式（cdbKeyName 归一化），与设备兼容 ID 归一化
// 后的形式一致。
var intelIDEPCIIDs = map[string]struct{}{
	`pci#ven_8086&dev_1230`: {}, // PIIX
	`pci#ven_8086&dev_7010`: {}, // PIIX3 IDE
	`pci#ven_8086&dev_7111`: {}, // PIIX4 IDE
	`pci#ven_8086&dev_2411`: {}, // ICH IDE
	`pci#ven_8086&dev_2421`: {}, // ICH0 IDE
	`pci#ven_8086&dev_244a`: {}, // ICH2 IDE
	`pci#ven_8086&dev_248a`: {}, // ICH3 IDE
	`pci#ven_8086&dev_24ca`: {}, // ICH4 IDE
	`pci#ven_8086&dev_24db`: {}, // ICH5 IDE
	`pci#ven_8086&dev_2651`: {}, // ICH6 IDE
	`pci#ven_8086&dev_27df`: {}, // ICH7 IDE
}

func (fixer *windowsSystemFixer) checkPciInDriverStoreLegacy(up *universal.UniPci) error {
	logger.Debugf("checkPciInDriverStoreLegacy: ++")
	defer logger.Debugf("checkPciInDriverStoreLegacy: --")

	criticalDeviceDatabasePath := filepath.Join(
		fixer.offsys.registryRootKey,
		fmt.Sprintf("ControlSet00%d", fixer.offsys.currentControlSet),
		"Control",
		"CriticalDeviceDatabase",
	)

	svcName := ""
	for _, compatID := range up.MsCompatibleId() {
		keyName := cdbKeyName(compatID)
		if keyName == "" {
			continue
		}
		keyPath := filepath.Join(criticalDeviceDatabasePath, keyName)

		key, err := registry.OpenKey(
			registry.LOCAL_MACHINE,
			keyPath,
			registry.QUERY_VALUE|registry.READ,
		)
		if err != nil {
			logger.Debugf("checkPciInDriverStoreLegacy: DeviceId %s not found", keyName)
			continue
		}

		svc, _, err := key.GetStringValue("Service")
		key.Close()

		if err != nil {
			continue
		}

		svcName = svc
	}

	logger.Debugf("checkPciInDriverStoreLegacy: svcName is `%s`", svcName)

	if svcName == "" {
		// CDB无记录，就匹配当前硬件是否是被intelide兼容的硬件
		// 经典形式：PCI\VEN_*&DEV_*
		venAndDevString := cdbKeyName(up.MsCompatibleId()[1])
		if _, ok := intelIDEPCIIDs[venAndDevString]; ok {
			// 属于 intelide 支持的 PCI ID：intelide.sys 是系统自带
			// 驱动，只需登记 CDB 记录并启用服务即可引导。
			// CDB 记录：ClassGUID = IDE/ATA 控制器类，
			// Service = intelide，加载组为 SCSI miniport
			// （与真实系统中 intelide 的 Group 一致）。
			logger.Debugf("checkPciInDriverStoreLegacy: supported by intelIde.sys")

			deviceIds := append(up.MsHardwareId(), up.MsCompatibleId()...)
			if e := fixer.ensureLegacyBootDriver(
				"intelide", "intelide.sys",
				scsiMiniportGroup, classGuidHDC, deviceIds); e != nil {
				return e
			}

			return nil
		}

		// CDB 中无记录：基于 infMaps 索引匹配系统内已有驱动。
		// 匹配成功后补齐驱动文件、创建服务并登记 CDB 记录。
		m, _ := fixer.matchInfMap(up)
		if m != nil {
			logger.Debugf("checkPciInDriverStoreLegacy: matched inf map, service=%s inf=%s",
				m.serviceName, m.infName)

			if e := fixer.installLegacyDriverFromInfMap(m, up); e != nil {
				return e
			}

			return nil
		}

		logger.Debugf("checkPciInDriverStoreLegacy: no inf map matched")
	}

	if svcName != "" && fixer.existedService(svcName) {
		return fixer.enableService(svcName)
	}

	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("SelectWindowsBestNormalDriver panic: %v\n%s", r, debug.Stack())
			panic(r) // 如果不想吞掉 panic，可以继续抛
		}
	}()

	ds, e := fixer.x2xLib.SelectWindowsBestNormalDriver(
		fixer.opts.RecoveryParam.Source.Arch,
		fixer.offsys.windowsVersion,
		up.String(),
		false)
	if e != nil {
		logger.Warnf("checkPciInDriverStoreLegacy: SelectWindowsBestNormalDriver: %v", e)
		return e
	}

	logger.Debugf("checkPciInDriverStoreLegacy: ds is %s", extend.Pretty(ds))

	// 旧版系统（< Win7/2008R2）不支持 DISM /Add-Driver，
	// 通过文件复制 + 创建服务 + 登记 CDB 的方式注入。
	if e = fixer.injectNormalDriverLegacy(ds, up); e != nil {
		return e
	}

	logger.Debugf("checkPciInDriverStoreLegacy: success")

	return nil
}

// injectNormalDriverLegacy 为旧版系统（< Win7/2008R2）注入驱动库中的
// 普通硬件驱动（非虚拟化驱动）。旧系统无 DISM /Add-Driver 能力，
// 通过文件复制 + 注册表服务项 + CriticalDeviceDatabase 完成：
//
//  1. 将驱动库中的 .sys 复制到 system32\drivers，
//     .inf/.cat/.pnf 复制到 Windows\Inf；
//  2. 解析 .inf 获取服务名与 ClassGUID，创建驱动服务
//     （存储类为 boot start）；
//  3. 在 CDB 中为该设备登记 Service 与 ClassGUID，
//     保证引导阶段可识别并加载驱动。
func (fixer *windowsSystemFixer) injectNormalDriverLegacy(
	ds *x2xlib.DriverResource,
	up *universal.UniPci,
) error {
	sysRoot := fixer.offsys.sysVolumeLtr + `:\`
	driversDir := filepath.Join(sysRoot, "Windows", "System32", "drivers")
	infDir := filepath.Join(sysRoot, "Windows", "Inf")

	files, e := os.ReadDir(ds.Dir)
	if e != nil {
		return errors.Wrapf(e, "read driver dir %s", ds.Dir)
	}

	// 1) 拷贝驱动文件
	copiedInfs := make([]string, 0)
	for _, f := range files {
		if f.IsDir() {
			continue
		}

		name := f.Name()
		ext := strings.ToLower(filepath.Ext(name))

		var dst string
		switch ext {
		case ".sys":
			dst = filepath.Join(driversDir, name)
		case ".inf", ".cat", ".pnf":
			dst = filepath.Join(infDir, name)
			if ext == ".inf" {
				copiedInfs = append(copiedInfs, dst)
			}
		default:
			continue
		}

		src := filepath.Join(ds.Dir, name)
		if e = extend.CopyFile(src, dst, 0o666); e != nil {
			return errors.Wrapf(e, "copy %s -> %s", src, dst)
		}
		logger.Debugf("injectNormalDriverLegacy: copied %s", dst)
	}

	// 2)/3) 解析每个 .inf，注册服务并登记 CDB
	bootCritical := up.BaseClassId() == 0x01

	for _, infPath := range copiedInfs {
		infObj, e := ParseINF(infPath)
		if e != nil {
			logger.Warnf("injectNormalDriverLegacy: parse %s: %v", infPath, e)
			continue
		}

		classGuid := infObj.ClassGUID()
		sysFiles := infObj.SysFiles()

		for _, svc := range infObj.ServiceNames() {
			sysFileName := ""
			if len(sysFiles) > 0 {
				sysFileName = sysFiles[0]
			}

			if sysFileName != "" {
				if e = fixer.installLegacyService(svc, sysFileName, bootCritical); e != nil {
					return e
				}
			}

			// 仅引导关键（存储类）驱动登记 CDB
			if bootCritical && classGuid != "" {
				if e = fixer.registerCriticalDeviceDatabase(
					svc, classGuid, up.MsCompatibleId()); e != nil {
					return e
				}
			}
		}
	}

	return nil
}

// buildInfMaps 遍历离线系统 Windows\Inf 目录下的所有 INF 文件，
// 解析出驱动服务名、设备类 GUID、PCI 硬件/兼容 ID 集合与 .sys 文件
// 列表，重建 fixer.offsys.infMaps 索引。
//
// 仅收录存储类（SCSIAdapter）与网络类（Net）驱动，其余类别对异构
// 恢复引导无影响，不纳入索引。
//
// 该索引供旧版系统（< Win7/2008R2，使用 CriticalDeviceDatabase）
// 在 CDB 中无记录时，按目标机器 PCI 设备匹配系统中已有的驱动。
func (fixer *windowsSystemFixer) buildInfMaps() error {
	infDir := filepath.Join(fixer.offsys.sysVolumeLtr+":\\", "Windows", "Inf")

	entries, err := os.ReadDir(infDir)
	if err != nil {
		return errors.Wrapf(err, "read inf dir %s", infDir)
	}

	fixer.offsys.infMaps = fixer.offsys.infMaps[:0]

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.EqualFold(filepath.Ext(name), ".inf") {
			continue
		}

		infPath := filepath.Join(infDir, name)

		infObj, err := ParseINF(infPath)
		if err != nil {
			logger.Warnf("buildInfMaps: failed to parse %s: %v", infPath, err)
			continue
		}

		// 仅收录存储类与网络类驱动
		classGuid := infObj.ClassGUID()
		if !strings.EqualFold(classGuid, classGuidSCSIAdapter) &&
			!strings.EqualFold(classGuid, classGuidNet) {
			continue
		}

		hwIds := infObj.HardwareIds()
		if len(hwIds) == 0 {
			continue
		}

		sysFiles := infObj.SysFiles()

		for _, svc := range infObj.ServiceNames() {
			fixer.offsys.infMaps = append(fixer.offsys.infMaps, infMap{
				serviceName: svc,
				classGuid:   classGuid,
				infName:     name,
				pciList:     hwIds,
				sysFiles:    sysFiles,
			})
		}
	}

	logger.Debugf("buildInfMaps: %d inf maps built from %s", len(fixer.offsys.infMaps), infDir)

	return nil
}

// matchInfMap 在 fixer.offsys.infMaps 索引中查找与目标设备兼容 ID
// 匹配的驱动条目。
//
// 设备的兼容 ID（形如 PCI\VEN_1AF4\DEV_1001，字段间以 "\" 分隔）与
// INF 声明的硬件/兼容 ID（形如 pci\ven_1af4&dev_1001，字段间以 "&"
// 分隔）格式不同，比对前统一用 cdbKeyName 归一化为
// "pci#ven_1af4&dev_1001" 形式，任一相同即命中。
//
// 返回匹配到的索引条目与命中的归一化 ID；未匹配时返回 (nil, "")。
func (fixer *windowsSystemFixer) matchInfMap(up *universal.UniPci) (*infMap, string) {
	// 设备硬件 ID 与兼容 ID 归一化为 CDB 键名形式后建立集合：
	// INF 中既可能声明完整硬件 ID（带 SUBSYS），也可能只声明
	// 兼容 ID（VEN+DEV 等），两者都要参与匹配。
	compatSet := make(map[string]bool)
	for _, id := range append(up.MsHardwareId(), up.MsCompatibleId()...) {
		k := cdbKeyName(id)
		if k != "" {
			compatSet[k] = true
		}
	}

	for i := range fixer.offsys.infMaps {
		m := &fixer.offsys.infMaps[i]

		for _, pciID := range m.pciList {
			k := cdbKeyName(pciID)
			if k != "" && compatSet[k] {
				return m, k
			}
		}
	}

	return nil, ""
}

// installLegacyDriverFromInfMap 处理旧版系统 CDB 中无记录、但系统内
// 已有匹配驱动（INF 存在于 Windows\Inf）的场景：
//
//  1. 按 infMap 中的 sysFiles 检查 system32\drivers 下驱动文件是否在场；
//  2. 缺失时，到 DriverStore\FileRepository 下查找该驱动的安装包目录，
//     将包内对应的 .sys 文件复制到 system32\drivers；找不到包则报错；
//  3. 创建驱动服务（存储类为 boot start，网络类为 demand start）；
//  4. 在 CriticalDeviceDatabase 中为该设备登记 Service 与 ClassGUID。
func (fixer *windowsSystemFixer) installLegacyDriverFromInfMap(
	m *infMap,
	up *universal.UniPci,
) error {
	sysRoot := fixer.offsys.sysVolumeLtr + `:\`
	driversDir := filepath.Join(sysRoot, "Windows", "System32", "drivers")

	// 1) 逐个确认 INF 声明的 .sys 是否已在 system32\drivers
	var missingSys []string
	for _, sysFile := range m.sysFiles {
		dst := filepath.Join(driversDir, sysFile)
		if !extend.IsExisted(dst) {
			missingSys = append(missingSys, sysFile)
		}
	}

	// 2) 缺失时从 DriverStore\FileRepository 的安装包中补齐
	if len(missingSys) > 0 {
		pkgDir, e := fixer.findDriverPackageDir(m)
		if e != nil {
			return e
		}

		for _, sysFile := range missingSys {
			src := filepath.Join(pkgDir, sysFile)
			if !extend.IsExisted(src) {
				return errors.Errorf(
					"driver package %s does not contain %s", pkgDir, sysFile)
			}

			dst := filepath.Join(driversDir, sysFile)
			if e = extend.CopyFile(src, dst, 0o666); e != nil {
				return errors.Wrapf(e, "copy %s -> %s", src, dst)
			}
			logger.Debugf("installLegacyDriverFromInfMap: copied %s -> %s", src, dst)
		}
	}

	// 3) 创建驱动服务：存储类驱动需引导加载（boot start）
	bootCritical := strings.EqualFold(m.classGuid, classGuidSCSIAdapter)

	// 服务注册需要 .sys 文件名；取 INF 声明的第一个 .sys
	if len(m.sysFiles) == 0 {
		return errors.Errorf("inf %s declares no sys file", m.infName)
	}
	sysFileName := m.sysFiles[0]

	if e := fixer.installLegacyService(m.serviceName, sysFileName, bootCritical); e != nil {
		return e
	}

	// 4) 在 CDB 中登记该设备，保证引导阶段可识别并加载驱动。
	// 完整硬件 ID 与兼容 ID 一并登记，提高引导阶段命中率。
	compatIds := append(up.MsHardwareId(), up.MsCompatibleId()...)
	if e := fixer.registerCriticalDeviceDatabase(m.serviceName, m.classGuid, compatIds); e != nil {
		return e
	}

	logger.Infof(
		"installLegacyDriverFromInfMap: service %s installed for %s (inf=%s)",
		m.serviceName,
		up,
		m.infName,
	)

	return nil
}

// findDriverPackageDir 在离线系统的 DriverStore\FileRepository 下
// 查找指定驱动的安装包目录。
//
// 包目录以驱动 INF 原始名开头（如 viostor.inf_amd64_xxx），优先按
// infMap 中记录的服务名匹配；若按服务名找不到，则扫描所有包目录，
// 返回第一个包含该驱动全部 .sys 文件的目录。
func (fixer *windowsSystemFixer) findDriverPackageDir(m *infMap) (string, error) {
	fileRepoDir := filepath.Join(
		fixer.offsys.sysVolumeLtr+":\\",
		"Windows", "System32", "DriverStore", "FileRepository")

	entries, err := os.ReadDir(fileRepoDir)
	if err != nil {
		return "", errors.Wrapf(err, "read FileRepository %s", fileRepoDir)
	}

	// 优先：按 "<服务名>.inf" 前缀匹配包目录名
	prefix := strings.ToLower(m.serviceName) + ".inf"
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		name := strings.ToLower(entry.Name())
		if strings.HasPrefix(name, prefix) {
			pkgDir := filepath.Join(fileRepoDir, entry.Name())
			logger.Debugf("findDriverPackageDir: matched by service name: %s", pkgDir)
			return pkgDir, nil
		}
	}

	// 兜底：扫描所有包目录，返回包含全部所需 .sys 的目录
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pkgDir := filepath.Join(fileRepoDir, entry.Name())

		allFound := true
		for _, sysFile := range m.sysFiles {
			if !extend.IsExisted(filepath.Join(pkgDir, sysFile)) {
				allFound = false
				break
			}
		}

		if allFound && len(m.sysFiles) > 0 {
			logger.Debugf("findDriverPackageDir: matched by sys files: %s", pkgDir)
			return pkgDir, nil
		}
	}

	return "", errors.Errorf(
		"no driver package found in FileRepository for service %s (inf %s)",
		m.serviceName, m.infName)
}
