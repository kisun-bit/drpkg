package x2xcore

import (
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
	}
	_, _, e := command.Execute(fmt.Sprintf("mountvol.exe %s %s", driveLetter, deviceId), command.WithDebug())
	return e
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

	dymDisks := make([]info.Disk, 0, len(disks))
	for _, d := range disks {
		if d.IsMsDynamic {
			dymDisks = append(dymDisks, d)
		}
	}

	if len(dymDisks) == 0 {
		return nil
	}

	logger.Debugf("ImportForeignDisk: Adding %d disks", len(dymDisks))

	var script strings.Builder
	script.WriteString("list disk\r\n")

	for _, d := range dymDisks {
		id, err := extend.WindowsDiskIDFromPath(d.Device)
		if err != nil {
			return err
		}

		_, _ = fmt.Fprintf(
			&script,
			"select disk %d\r\nimport\r\ndetail disk\r\n",
			id,
		)
	}

	content := script.String()
	logger.Debugf("ImportForeignDisk: Shell:\n%s", content)

	scriptFile := filepath.Join(
		extend.ExecDir(),
		fmt.Sprintf("impDym-%d.txt", time.Now().UnixNano()),
	)

	if err := os.WriteFile(scriptFile, []byte(content), 0644); err != nil {
		return err
	}
	defer os.Remove(scriptFile)

	if _, _, err := command.Execute(
		fmt.Sprintf("diskpart /s %s", scriptFile),
		command.WithDebug(),
	); err != nil {
		return err
	}

	return nil
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
	cmdline := fmt.Sprintf("reg load %s %s", key, regDBPath)
	if _, _, e := command.Execute(cmdline, command.WithDebug()); e != nil {
		return e
	}
	return nil
}

func unloadReg(key string) error {
	cmdline := fmt.Sprintf("reg unload %s", key)
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
	key, err := registry.OpenKey(root, path, registry.ALL_ACCESS)
	if err != nil {
		return err
	}
	defer key.Close()

	names, err := key.ReadSubKeyNames(-1)
	if err != nil {
		return err
	}

	for _, name := range names {
		if err := deleteRegistryTree(root, path+`\`+name); err != nil {
			return err
		}
	}

	return registry.DeleteKey(root, path)
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
