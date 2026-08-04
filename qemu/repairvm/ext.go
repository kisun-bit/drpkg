package repairvm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kisun-bit/drpkg/disk/image/qemublk"
	"github.com/kisun-bit/drpkg/ps/recovery/x2xcore"
	"github.com/pkg/errors"
)

func (o *Option) Validate() error {

	if o == nil {
		return errors.Errorf("option is nil")
	}

	if err := validatePath(
		o.VmBootDiskFile,
		"vm boot disk",
	); err != nil {
		return err
	}

	if isqcow2, _ := isQCOW2(o.VmBootDiskFile); !isqcow2 {
		return errors.New("boot file format is not qcow2")
	}

	if len(o.OfflineSystemDisks) == 0 {
		return errors.Errorf(
			"offline system disks is empty",
		)
	}

	seenIndex := make(map[int]struct{})

	for i, disk := range o.OfflineSystemDisks {

		if err := validateDisk(
			disk,
		); err != nil {
			return errors.Errorf(
				"offline system disk[%d]: %w",
				i,
				err,
			)
		}

		if _, ok := seenIndex[disk.Index]; ok {
			return errors.Errorf(
				"duplicate disk index: %d",
				disk.Index,
			)
		}

		seenIndex[disk.Index] = struct{}{}
	}

	if err := x2xcore.CheckAndFillRecoveryParameter(&o.RecoveryParams); err != nil {
		return err
	}

	if o.SimulatorConfigFile != "" {

		if err := validateFile(
			o.SimulatorConfigFile,
			"simulator config",
		); err != nil {
			return err
		}
	}

	return nil
}

func validateDisk(
	d Disk,
) error {

	if d.Index < 0 {
		return errors.Errorf(
			"invalid index: %d",
			d.Index,
		)
	}

	if d.Path == "" {
		return errors.Errorf(
			"path is empty",
		)
	}

	if err := validatePath(
		d.Path,
		"disk",
	); err != nil {
		return err
	}

	if d.Size < 0 {
		return errors.Errorf(
			"invalid size: %d",
			d.Size,
		)
	}

	if d.LBA < 0 {
		return errors.Errorf(
			"invalid LBA: %d",
			d.LBA,
		)
	}

	if d.PBA < 0 {
		return errors.Errorf(
			"invalid PBA: %d",
			d.PBA,
		)
	}

	if d.LBA >= d.Size/512 {
		return errors.Errorf("invalid LBA: %d", d.LBA)
	}

	if d.PBA >= d.Size/512 {
		return errors.Errorf("invalid PBA: %d", d.LBA)
	}

	return nil
}

func validatePath(
	path string,
	name string,
) error {

	if path == "" {
		return errors.Errorf(
			"%s path is empty",
			name,
		)
	}

	_, err := os.Stat(path)

	if err != nil {

		if os.IsNotExist(err) {
			return errors.Errorf(
				"%s not found: %s",
				name,
				path,
			)
		}

		return errors.Errorf(
			"stat %s failed: %w",
			name,
			err,
		)
	}

	return nil
}

func validateFile(
	path string,
	name string,
) error {

	info, err := os.Stat(path)

	if err != nil {
		return errors.Errorf(
			"%s not found: %s",
			name,
			path,
		)
	}

	if info.IsDir() {
		return errors.Errorf(
			"%s is directory: %s",
			name,
			path,
		)
	}

	return nil
}

var qcow2Magic = []byte{'Q', 'F', 'I', 0xfb}

// isQCOW2 判断文件是否为 qcow2 镜像
func isQCOW2(path string) (bool, error) {

	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	header := make([]byte, 4)

	n, err := io.ReadFull(f, header)
	if err != nil {
		return false, err
	}

	if n != 4 {
		return false, nil
	}

	return bytes.Equal(header, qcow2Magic), nil
}

func isISOFile(path string) bool {
	ext := filepath.Ext(path)
	return strings.EqualFold(ext, ".iso")
}

func loadSimulatorsFromFile(file string) (simulators map[string]string, err error) {
	simulators = make(map[string]string)
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(data, &simulators)
	return simulators, err
}

func createBootOverlay(
	ctx context.Context,
	bootFile string,
) (newBootFile string, err error) {

	if bootFile == "" {
		return "", errors.New("boot file is empty")
	}

	ext := filepath.Ext(bootFile)

	base := strings.TrimSuffix(
		bootFile,
		ext,
	)

	newBootFile = fmt.Sprintf(
		"%s-overlay-%d%s",
		base,
		time.Now().UnixMilli(),
		ext,
	)

	size, _, err := qemublk.GetSizeAndFormat(bootFile)

	if err != nil {
		return "", errors.Errorf(
			"get boot file size failed: %w",
			err,
		)
	}

	err = qemublk.CreateQCow2WithBackingFile(
		ctx,
		newBootFile,
		bootFile,
		size,
	)

	if err != nil {
		return "", errors.Errorf(
			"create qcow2 overlay failed: %w",
			err,
		)
	}

	return newBootFile, nil
}
