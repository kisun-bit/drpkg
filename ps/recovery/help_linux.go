package recovery

import (
	"context"
	"fmt"
	"os"

	"github.com/kisun-bit/drpkg/command"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/pkg/errors"
)

// Mount 挂载设备到指定挂载点
func Mount(ctx context.Context, device string, mountpoint string, readonly bool) (supported bool, err error) {
	logger.Debugf("Mount() ++")
	defer logger.Debugf("Mount() --")

	logger.Debugf("Mount() Mount %s at %s (readonly=%v)", device, mountpoint, readonly)

	mountCmd := fmt.Sprintf("mount %s %s", device, mountpoint)
	if readonly {
		mountCmd = fmt.Sprintf("mount -o ro %s %s", device, mountpoint)
	}

	_, output, err := command.ExecuteWithContext(ctx, mountCmd)
	if err == nil {
		return true, nil
	}

	logger.Warnf("Mount() Mount %s failed\noutput:\n%s\nerror:\n%s", device, output, err)

	repairCmd, ok := DetectFSRepairCmdline(device)
	if !ok {
		logger.Warnf("Mount() Mount %s failed. No fix-cmd matched", device)
		return false, nil
	}

	_, output, err = command.ExecuteWithContext(ctx, repairCmd)
	logger.Debugf("Mount() Fix %s with `%s`\noutput:\n%s\nerror:\n%v", device, repairCmd, output, err)

	logger.Debugf("Mount() Remount %s at %s", device, mountpoint)
	_, output, err = command.ExecuteWithContext(ctx, mountCmd)
	if err == nil {
		return true, nil
	}

	return false, errors.Wrapf(err, "remount %s failed: %s", device, output)
}

// Mount 取消设备的挂载
func Umount(deviceOrMountpoint string) error {
	logger.Debugf("Umount() ++")
	defer logger.Debugf("Umount() --")

	logger.Debugf("Umount() target=%s", deviceOrMountpoint)

	// 1. 普通卸载
	cmd := fmt.Sprintf("umount %s", deviceOrMountpoint)
	_, output, err := command.Execute(cmd)
	if err == nil {
		return nil
	}

	logger.Warnf("Umount() normal failed target=%s output=%s err=%v",
		deviceOrMountpoint, output, err)

	//// 2. 尝试 lazy umount（避免 busy 卡死）
	//cmd = fmt.Sprintf("umount -l %s", deviceOrMountpoint)
	//_, output, err = command.Execute(cmd)
	//if err == nil {
	//	logger.Warnf("Umount() lazy umount success target=%s", deviceOrMountpoint)
	//	return nil
	//}
	//
	//logger.Warnf("Umount() lazy failed target=%s output=%s err=%v",
	//	deviceOrMountpoint, output, err)

	// 3. 尝试 force（主要用于 NFS / 某些异常情况）
	cmd = fmt.Sprintf("umount -f %s", deviceOrMountpoint)
	_, output, err = command.Execute(cmd)
	if err == nil {
		logger.Warnf("Umount() force umount success target=%s", deviceOrMountpoint)
		return nil
	}

	logger.Warnf("Umount() force failed target=%s output=%s err=%v",
		deviceOrMountpoint, output, err)

	// 4. 尝试杀占用进程（谨慎使用）
	// fuser -km 会 kill 所有占用该挂载点的进程
	killCmd := fmt.Sprintf("fuser -km %s", deviceOrMountpoint)
	_, killOut, killErr := command.Execute(killCmd)
	logger.Warnf("Umount() fuser kill target=%s output=%s err=%v",
		deviceOrMountpoint, killOut, killErr)

	// 再尝试一次卸载
	cmd = fmt.Sprintf("umount %s", deviceOrMountpoint)
	_, output, err = command.Execute(cmd)
	if err == nil {
		logger.Warnf("Umount() success after kill target=%s", deviceOrMountpoint)
		return nil
	}

	return errors.Wrapf(err, "umount failed target=%s output=%s", deviceOrMountpoint, output)
}

func DeactivateVgs() error {
	logger.Debugf("DeactivateVgs() ++")
	defer logger.Debugf("DeactivateVgs() --")

	cmdline := fmt.Sprintf("vgchange -an")
	_, output, err := command.Execute(cmdline)
	if err == nil {
		return nil
	}

	logger.Warnf("DeactivateVgs() failed\noutput:\n%s\nerror:\n%v", output, err)
	return errors.Wrapf(err, "deactivateVg failed: %s", output)
}

func ActivateVgs() error {
	logger.Debugf("ActivateVgs() ++")
	defer logger.Debugf("ActivateVgs() --")

	e := os.RemoveAll("/etc/lvm/devices/system.devices")
	logger.Debugf("ActivateVgs() Remove system.devices: %s", e)

	rescanLvmCmd := "pvscan; vgscan; vgchange -ay"
	_, output, err := command.Execute(rescanLvmCmd)
	if err == nil {
		return nil
	}

	logger.Warnf("ActivateVgs() Scan lvm failed\noutput:\n%s\nerror:\n%v", output, err)
	return errors.Wrapf(err, "scan lvm failed: %s", output)
}

func vmbusExisted() (bool, error) {
	items, err := os.ReadDir("/sys/bus/vmbus/devices")
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	return len(items) > 0, nil
}
