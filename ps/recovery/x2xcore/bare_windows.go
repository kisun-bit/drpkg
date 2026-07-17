package x2xcore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/ps/bus/pci/universal"
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

	for _, p := range fixer.opts.RecoveryParam.Target.PciList {
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

		if err != nil {
			if up.BaseClassId() == 0x01 {
				return errors.Wrapf(err, "incompatible pci(%s): %s", up, up.MsHardwareId()[0])
			}
			// TODO 日志警告
		}
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
		return fmt.Errorf("open DriverInfFiles: %w", err)
	}
	defer infKey.Close()

	pkgIDs, _, err := infKey.GetStringsValue("Active")
	if err != nil {
		logger.Warnf("checkPciInDriverStore: Active value missing: %v", err)
		return fmt.Errorf("read Active packages: %w", err)
	}

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
					return fmt.Errorf("enable service %s: %w", svc, err)
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

func (fixer *windowsSystemFixer) checkPciInDriverStoreLegacy(up *universal.UniPci) error {
	logger.Debugf("checkPciInDriverStore: ++")
	defer logger.Debugf("checkPciInDriverStore: --")

	criticalDeviceDatabasePath := filepath.Join(
		fixer.offsys.registryRootKey,
		fmt.Sprintf("ControlSet00%d", fixer.offsys.currentControlSet),
		"Control",
		"CriticalDeviceDatabase",
	)

	svcName := ""
	for _, compatID := range up.MsCompatibleId() {
		compatID = strings.ReplaceAll(compatID, "\\", "#")
		keyPath := filepath.Join(criticalDeviceDatabasePath, compatID)

		key, err := registry.OpenKey(
			registry.LOCAL_MACHINE,
			keyPath,
			registry.QUERY_VALUE|registry.READ,
		)
		if err != nil {
			logger.Debugf("checkPciInDriverStore: DeviceId %s not found", compatID)
			continue
		}

		svc, _, err := key.GetStringValue("Service")
		key.Close()

		if err != nil {
			continue
		}

		svcName = svc
	}

	if svcName != "" && fixer.existedService(svcName) {
		return fixer.enableService(svcName)
	}

	// TODO 查询驱动库所有驱动文件，看非启动相关的Pnp能否加载对应硬件的驱动

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
