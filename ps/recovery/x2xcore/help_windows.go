package x2xcore

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kisun-bit/drpkg/command"
	"github.com/kisun-bit/drpkg/define"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/ps/info"
	"github.com/kisun-bit/drpkg/ps/recovery/x2xlib"
	"github.com/pkg/errors"
	"github.com/yusufpapurcu/wmi"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func Mount(ctx context.Context, device string, mountpoint string, readonly bool) (supported bool, err error) {
	return true, nil
}

func Umount(deviceOrMountpoint string, recursive bool) error {
	return nil
}

func VmbusExisted() (bool, error) {
	devInfo, err := windows.SetupDiGetClassDevsEx(
		nil,
		"",
		0,
		windows.DIGCF_ALLCLASSES|windows.DIGCF_PRESENT,
		0,
		"",
	)
	if err != nil {
		return false, err
	}
	defer devInfo.Close()

	for i := 0; ; i++ {
		devInfoData, eEnum := devInfo.EnumDeviceInfo(i)
		if eEnum != nil {
			if errors.Is(eEnum, windows.ERROR_NO_MORE_ITEMS) {
				break
			}
			continue
		}
		hwIdVal, eHwIdVal := devInfo.DeviceRegistryProperty(devInfoData, windows.SPDRP_HARDWAREID)
		if eHwIdVal != nil {
			continue
		}
		for _, hwid := range hwIdVal.([]string) {
			if strings.HasPrefix(hwid, "VMBUS") {
				return true, nil
			}
		}
	}

	return false, nil
}

type Win32Volume struct {
	DeviceID    string
	DriveLetter string
	Label       string
	DriveType   uint32
}

func ListLocalVolumes() ([]Win32Volume, error) {
	var volumes []Win32Volume

	err := wmi.Query(
		`SELECT DeviceID, DriveLetter, Label, DriveType
		  FROM Win32_Volume
		  WHERE DriveType = 3`,
		&volumes,
	)
	if err != nil {
		return nil, err
	}

	return volumes, nil
}

func AssignDriveLetter(deviceId string, driveLetter string) error {
	if deviceId == "" {
		return errors.New("AssignDriveLetter: deviceId is empty")
	}
	if driveLetter == "" {
		ok, ltr := getFreeLtr()
		if !ok {
			return errors.New("getFreeLtr failed")
		}
		driveLetter = ltr + ":\\"
	} else {
		driveLetter += ":\\"
	}
	drvLtrU16, _ := windows.UTF16PtrFromString(driveLetter)
	volPathU16, _ := windows.UTF16PtrFromString(deviceId)

	//_, _, e := command.Execute(fmt.Sprintf("mountvol.exe %s %s", driveLetter, deviceId), command.WithDebug())
	return windows.SetVolumeMountPoint(drvLtrU16, volPathU16)
}

func getFreeLtr() (existed bool, ltr string) {
	for c := 'D'; c <= 'Y'; c++ {
		ltr = fmt.Sprintf("%c", c)
		drive := fmt.Sprintf("%s:\\", ltr)
		if _, err := os.Stat(drive); os.IsNotExist(err) {
			return true, ltr
		}
	}
	return false, ""
}

func ImportForeignDisk() error {
	logger.Debugf("ImportForeignDisk: ++")
	defer logger.Debugf("ImportForeignDisk: --")

	disks, err := info.QueryDisks()
	if err != nil {
		return err
	}

	type dynamicDisk struct {
		info.Disk
		ID int
	}

	var dynDisks []dynamicDisk

	for _, d := range disks {
		if !d.IsMsDynamic {
			continue
		}

		id, err := extend.WindowsDiskIDFromPath(d.Device)
		if err != nil {
			logger.Warnf("get disk id for %s failed: %v", d.Device, err)
			continue
		}

		dynDisks = append(dynDisks, dynamicDisk{
			Disk: d,
			ID:   int(id),
		})
	}

	if len(dynDisks) == 0 {
		return nil
	}

	logger.Debugf("ImportForeignDisk: found %d disks", len(dynDisks))

	//
	// 每块盘上线
	//
	for _, d := range dynDisks {
		script := fmt.Sprintf(`
select disk %d
online disk
`, d.ID)

		_ = runDiskPart(script)
	}

	//
	// 每块盘清除只读
	//
	for _, d := range dynDisks {
		script := fmt.Sprintf(`
select disk %d
attributes disk clear readonly
`, d.ID)

		_ = runDiskPart(script)
	}

	//
	// 每块盘导入
	//
	for _, d := range dynDisks {
		script := fmt.Sprintf(`
select disk %d
import foreign
`, d.ID)

		_ = runDiskPart(script)
	}

	return nil
}

func runDiskPart(script string) error {

	file := filepath.Join(
		extend.ExecDir(),
		fmt.Sprintf("diskpart-%d.txt", time.Now().UnixNano()),
	)

	if err := os.WriteFile(file, []byte(script), 0644); err != nil {
		return err
	}
	defer os.Remove(file)

	_, _, err := command.Execute(
		fmt.Sprintf("diskpart /s %s", file),
		command.WithDebug(),
	)

	return err
}

// FindX2xLibraryDir 搜索本机驱动库目录。
// 搜索逻辑：
//  1. 枚举所有 CD/DVD 盘符
//  2. 搜索 */driverstore.H0nK1.db
//  3. 返回 xxx/*
func FindX2xLibraryDir() (string, error) {

	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return "", err
	}

	for i := 0; i < 26; i++ {

		if mask&(1<<uint(i)) == 0 {
			continue
		}

		root := fmt.Sprintf("%c:\\", 'A'+i)
		_rootPathName, err := windows.UTF16PtrFromString(root)
		if err != nil {
			continue
		}
		if t := windows.GetDriveType(_rootPathName); t != windows.DRIVE_CDROM {
			continue
		}

		if dir, err := findDriverStoreDir(root); err == nil {
			return dir, nil
		}
	}

	return "", fmt.Errorf("x2x driver library not found")
}

// findDriverStoreDir 在 root 下查找 driverstore.H0nK1.db。
func findDriverStoreDir(root string) (string, error) {

	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}

	for _, entry := range entries {

		path := filepath.Join(root, entry.Name())

		if entry.IsDir() {
			dir, err := findDriverStoreDir(path)
			if err == nil {
				return dir, nil
			}
			continue
		}

		if entry.Name() == x2xlib.DriverStoreDBName {
			return root, nil
		}
	}

	return "", os.ErrNotExist
}

func loadReg(key, regDBPath string) error {
	cmdline := fmt.Sprintf("REG LOAD HKLM\\%s %s", key, regDBPath)
	if _, _, e := command.Execute(cmdline, command.WithDebug()); e != nil {
		return e
	}
	return nil
}

func unloadReg(key string) error {
	cmdline := fmt.Sprintf("REG UNLOAD HKLM\\%s", key)
	if _, _, e := command.Execute(cmdline, command.WithDebug()); e != nil {
		return e
	}
	return nil
}

func detectWindowsVersion(
	productName string,
	currentVersion string,
	build int,
	major uint64,
) define.WindowsVersion {

	isServer := strings.Contains(strings.ToLower(productName), "server")

	// Windows 10 / 11 / Server 2016+
	if major >= 10 {
		if !isServer {
			if build >= 22000 {
				return define.Win11
			}
			return define.Win10
		}

		switch {
		case build >= 26100:
			return define.Win2k25
		case build >= 20348:
			return define.Win2k22
		case build >= 17763:
			return define.Win2k19
		default:
			return define.Win2k16
		}
	}

	switch currentVersion {
	case "5.0":
		return define.Win2k

	case "5.1":
		return define.WinXP

	case "5.2":
		// XP x64 也是 5.2，这里一般认为 Server 2003
		if isServer {
			return define.Win2k3
		}
		return define.WinXP

	case "6.0":
		if isServer {
			return define.Win2k8
		}
		return define.WinVista

	case "6.1":
		if isServer {
			return define.Win2k8r2
		}
		return define.Win7

	case "6.2":
		if isServer {
			return define.Win2k12
		}
		return define.Win8

	case "6.3":
		if isServer {
			return define.Win2k12r2
		}
		return define.Win81
	}

	return define.WinUnknown
}

func deleteRegistryTree(root registry.Key, path string) error {
	key, err := registry.OpenKey(
		root,
		path,
		registry.ENUMERATE_SUB_KEYS|registry.SET_VALUE|registry.QUERY_VALUE,
	)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return errors.Wrapf(err, "open registry key %q", path)
	}
	defer key.Close()

	names, err := key.ReadSubKeyNames(-1)
	if err != nil {
		return errors.Wrapf(err, "enumerate subkeys of %q", path)
	}

	for _, name := range names {
		if err := deleteRegistryTree(root, path+`\`+name); err != nil {
			return err
		}
	}

	if err := registry.DeleteKey(root, path); err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return errors.Wrapf(err, "delete registry key %q", path)
	}

	return nil
}

func filterMultiSzValue(
	key registry.Key,
	valueName string,
	blockList []string,
	regPath string,
) (bool, error) {

	vals, _, err := key.GetStringsValue(valueName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, errors.Wrapf(err, "read %s of %s failed", valueName, regPath)
	}

	// block list 转成 map，提高查找效率
	blockMap := make(map[string]struct{}, len(blockList))
	for _, s := range blockList {
		blockMap[strings.ToUpper(s)] = struct{}{}
	}

	logger.Debugf("filterMultiSzValue: %s\\%s = %v", regPath, valueName, vals)

	filtered := make([]string, 0, len(vals))
	modified := false

	for _, v := range vals {
		if _, ok := blockMap[strings.ToUpper(v)]; ok {
			logger.Debugf(
				"filterMultiSzValue: remove %q from %s\\%s",
				v, regPath, valueName,
			)
			modified = true
			continue
		}
		filtered = append(filtered, v)
	}

	if !modified {
		return false, nil
	}

	if len(filtered) == 0 {
		logger.Debugf(
			"filterMultiSzValue: delete empty value %s\\%s",
			regPath, valueName,
		)

		if err := key.DeleteValue(valueName); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, errors.Wrapf(err, "delete %s of %s failed", valueName, regPath)
		}

		return true, nil
	}

	logger.Debugf(
		"filterMultiSzValue: update %s\\%s => %v",
		regPath, valueName, filtered,
	)

	if err := key.SetStringsValue(valueName, filtered); err != nil {
		return false, errors.Wrapf(err, "set %s of %s failed", valueName, regPath)
	}

	return true, nil
}

type driverStore struct {
	PublishedName  string
	OriginFileName string
}

func parseDriverStore(output string) []driverStore {
	var (
		drivers []driverStore
		cur     *driverStore
	)

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		switch {
		case strings.HasPrefix(line, "Published Name"):
			// 保存上一项
			if cur != nil {
				drivers = append(drivers, *cur)
			}
			cur = &driverStore{
				PublishedName: value(line),
			}

		case cur != nil && strings.HasPrefix(line, "Original File Name"):
			cur.OriginFileName = value(line)
		}
	}

	// 保存最后一项
	if cur != nil {
		drivers = append(drivers, *cur)
	}

	return drivers
}

func value(line string) string {
	if i := strings.IndexByte(line, ':'); i >= 0 {
		return strings.TrimSpace(line[i+1:])
	}
	return ""
}
