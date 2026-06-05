package x2xcore

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kisun-bit/drpkg/command"
)

type LuksOpenResult struct {
	Device  string
	Mapper  string
	Skipped bool
}

func ListLUKSDevices() ([]string, error) {
	r, out, err := command.Execute(
		"blkid -t TYPE=crypto_LUKS -o device",
		command.WithDebug(),
	)

	if err != nil {
		if r == 2 {
			return nil, nil
		}
		return nil, fmt.Errorf("blkid failed: %w", err)
	}

	return strings.Fields(out), nil
}

func OpenedLUKSDevices() (map[string]string, error) {
	result := make(map[string]string)

	mappers, err := filepath.Glob("/dev/mapper/*")
	if err != nil {
		return nil, err
	}

	for _, mapperPath := range mappers {

		mapper := filepath.Base(mapperPath)

		_, out, err := command.Execute(
			"cryptsetup status "+mapper,
			command.WithDebug(),
		)

		if err != nil {
			continue
		}

		var device string

		for _, line := range strings.Split(out, "\n") {

			line = strings.TrimSpace(line)

			if strings.HasPrefix(line, "device:") {

				device = strings.TrimSpace(
					strings.TrimPrefix(line, "device:"),
				)

				break
			}
		}

		if device != "" {
			result[device] = mapper
		}
	}

	return result, nil
}

func OpenLUKS(
	device string,
	mapper string,
	password string,
) error {

	cmd := exec.Command(
		"cryptsetup",
		"luksOpen",
		device,
		mapper,
		"--key-file=-",
	)

	cmd.Stdin = bytes.NewBufferString(password)

	out, err := cmd.CombinedOutput()

	if err != nil {
		return fmt.Errorf(
			"cryptsetup luksOpen %s failed: %s (%w)",
			device,
			strings.TrimSpace(string(out)),
			err,
		)
	}

	return nil
}

func OpenAllLUKS(password string) ([]LuksOpenResult, error) {

	devices, err := ListLUKSDevices()
	if err != nil {
		return nil, err
	}

	if len(devices) == 0 {
		return nil, nil
	}

	opened, err := OpenedLUKSDevices()
	if err != nil {
		return nil, err
	}

	var results []LuksOpenResult
	var errs []string

	for _, device := range devices {

		if mapper, ok := opened[device]; ok {

			results = append(results, LuksOpenResult{
				Device:  device,
				Mapper:  "/dev/mapper/" + mapper,
				Skipped: true,
			})

			continue
		}

		mapper := "luks_" + filepath.Base(device)

		if err := OpenLUKS(
			device,
			mapper,
			password,
		); err != nil {

			errs = append(errs, err.Error())
			continue
		}

		results = append(results, LuksOpenResult{
			Device: device,
			Mapper: "/dev/mapper/" + mapper,
		})
	}

	_, _, _ = command.Execute("vgchange -ay", command.WithDebug())

	if len(errs) > 0 {
		return results, fmt.Errorf(
			strings.Join(errs, "\n"),
		)
	}

	return results, nil
}
