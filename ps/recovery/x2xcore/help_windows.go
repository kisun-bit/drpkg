package x2xcore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kisun-bit/drpkg/command"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/ps/info"
	"github.com/pkg/errors"
	"github.com/yusufpapurcu/wmi"
	"golang.org/x/sys/windows"
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

		fmt.Fprintf(
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
