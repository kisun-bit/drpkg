//go:build linux

package repairvm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kisun-bit/drpkg/disk/image/qemublk"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
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

	if len(o.RecoveryParams.OfflineSystemDisks) == 0 {
		return errors.Errorf(
			"offline system disks is empty",
		)
	}

	seenIndex := make(map[int]struct{})

	for i, disk := range o.RecoveryParams.OfflineSystemDisks {

		if err := validateDisk(
			disk,
		); err != nil {
			return errors.Errorf(
				"offline system disk[%d]: %v",
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

	//if isqcow2, _ := isQCOW2(o.VmBootDiskFile); !isqcow2 {
	//	return errors.New("boot file format is not qcow2")
	//}

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
			"stat %s failed: %v",
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

func validateDisk(
	d x2xcore.Disk,
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

	if !extend.IsExisted(d.Path) {
		return errors.Wrapf(os.ErrNotExist, d.Path)
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
			"get boot file size failed: %v",
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
			"create qcow2 overlay failed: %v",
			err,
		)
	}

	return newBootFile, nil
}

// WaitSerialSocketReady 等待 virtio-serial socket 就绪并返回可用连接。
// 调用方负责关闭返回的 net.Conn。
func WaitSerialSocketReady(ctx context.Context, sockFile string, retryCount int, retryInterval time.Duration) (*net.Conn, error) {
	if retryCount <= 0 {
		retryCount = 1
	}
	if retryInterval <= 0 {
		retryInterval = time.Second
	}

	var lastErr error
	for i := 0; i < retryCount; i++ {
		select {
		case <-ctx.Done():
			return nil, errors.Errorf("waiting for serial socket %q cancelled: %v", sockFile, ctx.Err())
		default:
		}

		dialCtx, dialCancel := context.WithTimeout(ctx, retryInterval)
		conn, err := (&net.Dialer{}).DialContext(dialCtx, "unix", sockFile)
		dialCancel()

		if err == nil {
			logger.Debugf("[Host] connected to virtio-serial channel %s (attempt %d/%d)", sockFile, i+1, retryCount)
			return &conn, nil
		}

		lastErr = err
		logger.Debugf("[Host] failed to connect to %s (%d/%d): %v", sockFile, i+1, retryCount, err)

		if i < retryCount-1 {
			select {
			case <-ctx.Done():
				return nil, errors.Errorf("waiting for serial socket %q cancelled: %v", sockFile, ctx.Err())
			case <-time.After(retryInterval):
			}
		}
	}

	return nil, errors.Errorf("failed to connect to serial socket %q after %d retries: %v", sockFile, retryCount, lastErr)
}

// IsKVMAvailable checks whether current Linux supports qemu -enable-kvm
func IsKVMAvailable() bool {

	// Linux only
	if _, err := os.Stat("/dev/kvm"); err != nil {
		return false
	}

	// Check CPU virtualization feature
	cpuInfo, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return false
	}

	cpu := strings.ToLower(string(cpuInfo))

	// Intel VT-x
	if strings.Contains(cpu, "vmx") {
		// kvm_intel loaded
		modules, _ := os.ReadFile("/proc/modules")
		return strings.Contains(string(modules), "kvm_intel")
	}

	// AMD-V
	if strings.Contains(cpu, "svm") {
		// kvm_amd loaded
		modules, _ := os.ReadFile("/proc/modules")
		return strings.Contains(string(modules), "kvm_amd")
	}

	return false
}
