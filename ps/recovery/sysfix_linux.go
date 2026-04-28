package recovery

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kisun-bit/drpkg/command"
	"github.com/kisun-bit/drpkg/disk/table"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/ps/info"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
)

const (
	InitrdToolDracut          = "dracut"
	InitrdToolUpdateInitramfs = "update-initramfs"
	InitrdToolMkinitrd        = "mkinitrd"
)

type linuxSystemFixer struct {
	ctx            context.Context
	opts           *FixerCreateOptions
	logs           <-chan LogEntry
	psinfo         *info.PsInfo
	fsDevices      []string
	sysDevRoot     string
	sysDevBoot     string
	SysDevEfi      string
	rootMountPoint string
	initrdTool     string
}

func NewSysFixer(ctx context.Context, opts *FixerCreateOptions) (fixer SysFixer, err error) {
	logger.Debugf("NewSysFixer: opts:\n%s", extend.Pretty(opts))
	if err = CheckFixerCreateOptions(opts); err != nil {
		return nil, err
	}
	return &linuxSystemFixer{ctx: ctx, opts: opts, logs: make(<-chan LogEntry, 1000)}, nil
}

// Prepare 准备修复环境（挂载/加载离线系统）
func (fixer *linuxSystemFixer) Prepare() error {
	logger.Debugf("Prepare: ++")
	defer logger.Debugf("Prepare: --")

	if err := fixer.activeLVM(); err != nil {
		return errors.Wrap(err, "active lvm")
	}

	fsDevs, e := enumFsDevice(fixer.opts.OfflineSysDisks)
	if e != nil {
		return e
	}
	fixer.fsDevices = fsDevs

	if err := fixer.cleanDattoSnapshot(); err != nil {
		return errors.Wrap(err, "cleaning datto snapshot")
	}

	if err := fixer.detectSysDevice(); err != nil {
		return errors.Wrap(err, "detect sys device")
	}

	if err := fixer.mountSys(); err != nil {
		return errors.Wrap(err, "mounting offline system")
	}

	return nil
}

// Repair 执行修复流程
func (fixer *linuxSystemFixer) Repair() error {
	logger.Debugf("Repair: ++")
	defer logger.Debugf("Repair: --")

	initrdTool, err := fixer.detectInitrdTool()
	if err != nil {
		return errors.Wrap(err, "detect initrd tool")
	}
	fixer.initrdTool = initrdTool

	return nil
}

// Cleanup 清理修复环境（卸载/释放资源）
func (fixer *linuxSystemFixer) Cleanup() error {
	return errors.New("implement me")
}

// GetLog 获取日志
func (fixer *linuxSystemFixer) GetLog() (LogEntry, bool) {
	select {
	case entry := <-fixer.logs:
		return entry, true
	default:
		return LogEntry{}, false
	}
}

// mountSys 挂载离线系统
func (fixer *linuxSystemFixer) mountSys() error {
	if fixer.sysDevRoot == "" {
		return errors.New("root filesystem is empty")
	}

	mountpoint, err := os.MkdirTemp("", "mountSys-*")
	if err != nil {
		return err
	}

	if _, err = Mount(fixer.ctx, fixer.sysDevRoot, mountpoint, false); err != nil {
		return err
	}

	if fixer.sysDevBoot != "" {
		bootMountpoint := filepath.Join(mountpoint, "boot")
		if _, err = Mount(fixer.ctx, bootMountpoint, mountpoint, false); err != nil {
			return err
		}
	}

	if fixer.SysDevEfi != "" {
		efiMountpoint := filepath.Join(mountpoint, "boot", "efi")
		if extend.IsExisted(efiMountpoint) {
			if _, err = Mount(fixer.ctx, efiMountpoint, mountpoint, false); err != nil {
				return err
			}
		}
	}

	if _, _, err = command.Execute(
		fmt.Sprintf("mount --bind /var %s", filepath.Join(mountpoint, "var"))); err != nil {
		return err
	}
	if _, _, err = command.Execute(
		fmt.Sprintf("mount --bind /dev %s", filepath.Join(mountpoint, "dev"))); err != nil {
		return err
	}
	if _, _, err = command.Execute(
		fmt.Sprintf("mount --bind /dev/pts %s", filepath.Join(mountpoint, "dev/pts"))); err != nil {
		return err
	}
	if _, _, err = command.Execute(
		fmt.Sprintf("mount -t proc procfs %s", filepath.Join(mountpoint, "proc"))); err != nil {
		return err
	}
	if _, _, err = command.Execute(
		fmt.Sprintf("mount -t sysfs sysfs %s", filepath.Join(mountpoint, "sys"))); err != nil {
		return err
	}

	fixer.rootMountPoint = mountpoint
	return nil
}

// activeLVM 激活LVM
func (fixer *linuxSystemFixer) activeLVM() error {
	logger.Debugf("activeLVM ++")
	defer logger.Debugf("activeLVM --")

	_, _, e := command.Execute("vgchange -an", command.WithDebug())
	if e != nil {
		return e
	}
	_, _, e = command.Execute("rm -f /etc/lvm/devices/system.devices", command.WithDebug())
	if e != nil {
		return e
	}
	_, _, e = command.Execute("pvscan", command.WithDebug())
	if e != nil {
		return e
	}
	_, _, e = command.Execute("vgscan", command.WithDebug())
	if e != nil {
		return e
	}
	_, _, e = command.Execute("vgchange -ay", command.WithDebug())
	if e != nil {
		return e
	}
	return nil
}

// detectSysDevice 探测系统根环境
func (fixer *linuxSystemFixer) detectSysDevice() error {
	logger.Debugf("detectSysDevice ++")
	defer logger.Debugf("detectSysDevice --")

	if len(fixer.fsDevices) == 0 {
		fsDevs, err := enumFsDevice(fixer.opts.OfflineSysDisks)
		if err != nil {
			return err
		}
		fixer.fsDevices = fsDevs
	}

	for _, dev := range fixer.fsDevices {
		switch {
		case IsRootDevice(fixer.ctx, dev):
			fixer.sysDevRoot = dev
		case IsBootDevice(fixer.ctx, dev):
			fixer.sysDevBoot = dev
		case IsEfiDevice(fixer.ctx, dev):
			fixer.SysDevEfi = dev
		}
	}

	logger.Debugf("detectSysDevice: root=`%s`, boot=`%s`, efi=`%s`",
		fixer.sysDevRoot, fixer.sysDevBoot, fixer.SysDevEfi)

	if fixer.sysDevRoot == "" {
		return errors.Errorf("no root filesystem device detected from candidates: %v", fixer.fsDevices)
	}

	return nil
}

// detectInitrdTool 探测离线系统的Initrd生成工具
func (fixer *linuxSystemFixer) detectInitrdTool() (string, error) {
	logger.Debugf("detectInitrdTool ++")
	defer logger.Debugf("detectInitrdTool --")

	if fixer.rootMountPoint == "" {
		return "", errors.New("root environment not mounted")
	}

	dracutFeatureFile := filepath.Join(fixer.rootMountPoint, "etc", "dracut.conf")
	if extend.IsExisted(dracutFeatureFile) {
		return InitrdToolDracut, nil
	}

	updateInitramfsFeatureFile := filepath.Join(fixer.rootMountPoint, "etc", "initramfs-tools", "update-initramfs.conf")
	if extend.IsExisted(updateInitramfsFeatureFile) {
		return InitrdToolUpdateInitramfs, nil
	}

	return InitrdToolMkinitrd, nil
}

// executeWithChroot 在chroot环境执行命令
func (fixer *linuxSystemFixer) executeWithChroot(cmdline string) error {
	if fixer.rootMountPoint == "" {
		return errors.New("root environment not mounted")
	}

	chrootCmdline := fmt.Sprintf(`chroot %s /bin/bash -c "export PATH=/sbin:/bin:/usr/sbin:/usr/bin:$PATH; %s"`,
		fixer.rootMountPoint, cmdline)
	_, _, e := command.Execute(chrootCmdline, command.WithDebug())

	return e
}

// cleanDattoSnapshot 清理datto(/elastio)快照
func (fixer *linuxSystemFixer) cleanDattoSnapshot() error {
	logger.Debugf("cleanDattoSnapshot: ++")
	defer logger.Debugf("cleanDattoSnapshot: --")

	return nil
}

func enumFsDevice(offline []string) ([]string, error) {
	psinfo, err := info.QueryPsInfo()
	if err != nil {
		return nil, err
	}

	slaveSet := make(map[string]struct{})
	addSlaves := func(slaves []string) {
		for _, s := range slaves {
			slaveSet[s] = struct{}{}
		}
	}

	for _, mp := range psinfo.Private.Linux.Multipath {
		addSlaves(mp.Slaves)
	}
	for _, rd := range psinfo.Private.Linux.Raid {
		addSlaves(rd.Slaves)
	}

	var fsDevList []string

	// 统一处理函数
	handleDevice := func(device string, dt info.DiskTable) {
		switch dt.Type {
		case table.TableTypeMBR, table.TableTypeGPT:
			for _, p := range dt.Partitions {
				if isInvalidPartition(p.Type) {
					continue
				}
				fsDevList = append(fsDevList, p.Device)
			}
		default:
			fsDevList = append(fsDevList, device)
		}
	}

	// 1. 普通磁盘
	for _, d := range psinfo.Public.Disks {
		if !funk.InStrings(offline, d.Device) {
			continue
		}
		if _, ok := slaveSet[d.Device]; ok {
			continue
		}
		handleDevice(d.Device, d.Table)
	}

	// 2. multipath
	for _, mp := range psinfo.Private.Linux.Multipath {
		if !allInOffline(mp.Slaves, offline) {
			continue
		}
		handleDevice(mp.Device, mp.Table)
	}

	// 3. raid
	for _, rd := range psinfo.Private.Linux.Raid {
		if !allInOffline(rd.Slaves, offline) {
			continue
		}
		handleDevice(rd.Device, rd.Table)
	}

	// 4. lvm
	for _, vg := range psinfo.Private.Linux.LVM.VGList {
		for _, lv := range vg.LVList {
			// 非标准卷
			if len(lv.Segments) == 0 {
				continue
			}
			lvDisks := make([]string, 0)
			for _, seg := range lv.Segments {
				lvDisks = append(lvDisks, seg.Device)
			}
			if !allInOffline(lvDisks, offline) {
				continue
			}
			fsDevList = append(fsDevList, lv.Device)
		}
	}

	fsDevList = funk.UniqString(fsDevList)

	// 过滤文件系统类型
	var devs []string
	for _, dev := range fsDevList {
		fsStr, _ := DetectFSTypeByBlkid(dev)
		if funk.InStrings(SupportedFsTypes, fsStr) {
			devs = append(devs, dev)
		}
	}

	return devs, nil
}

func isInvalidPartition(ptype string) bool {
	return funk.InStrings(table.MBRExtendPartTypes, ptype) ||
		ptype == table.GPT_LINUX_LVM ||
		ptype == table.MBR_LINUX_LVM_PARTITION
}

func allInOffline(slaves []string, offline []string) bool {
	if len(slaves) == 0 {
		return false
	}
	for _, s := range slaves {
		if !funk.InStrings(offline, s) {
			return false
		}
	}
	return true
}
