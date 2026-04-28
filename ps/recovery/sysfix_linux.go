package recovery

import (
	"context"

	"github.com/kisun-bit/drpkg/command"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/ps/info"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
)

type linuxSystemFixer struct {
	ctx context.Context

	opts *FixerCreateOptions

	logs <-chan LogEntry

	psinfo *info.PsInfo

	sysDevRoot   string
	sysDevBoot   string
	SysDeviceEfi string

	rootMountPoint string
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
	if err := fixer.mountSys(); err != nil {
		return err
	}
	return errors.New("implement me")
}

// Repair 执行修复流程
func (fixer *linuxSystemFixer) Repair() error {
	return errors.New("implement me")
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
	// TODO
	//  1. 如何挂载一个Linux的root环境？
	//     a. 探测root卷（boot、home、etc、opt、usr、var、）、boot卷（grub/grub2、vmlinuz*）、efi卷（EFI）
	//     b. 挂载root卷、boot卷、efi卷
	//     c. 挂载 mount --bind /var %s/var
	//        mount --bind /var %s/var
	//		  mount --bind /dev %s/dev
	//		  mount --bind /dev/pts %s/dev/pts
	//		  mount -t proc procfs %s/proc
	//		  mount -t sysfs sysfs %s/sys
	//  2. Linux下chroot后，有时无法执行ls、grep、df等基础命令，解决办法：chroot /mnt /bin/bash -c "export PATH=/sbin:/bin:$PATH; ls"
	//  3. virtio 正式进入 Linux 内核：2.6.24（2008年）
	//  4. initramfs打包命令使用，（可选dracut、update-initramfs、mkinitrd）
	//

	if err := fixer.activeLVM(); err != nil {
		return errors.Wrap(err, "active lvm")
	}

	if err := fixer.detectSysDevice(); err != nil {
		return errors.Wrap(err, "detect sys device")
	}

	return errors.New("implement me")
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
	logger.Debugf("activeLVM ++")
	defer logger.Debugf("activeLVM --")

	return errors.New("implement me")
}

// executeWithChroot 在chroot环境执行命令
func (fixer *linuxSystemFixer) executeWithChroot(cmdline string) error {
	return errors.New("implement me")
}

// cleanDattoSnapshot 清理datto(/elastio)快照
func (fixer *linuxSystemFixer) cleanDattoSnapshot() error {
	return errors.New("implement me")
}

// enumFsDevice 枚举所有的文件系统设备
func (fixer *linuxSystemFixer) enumFsDevice() (devs []string, err error) {
	psinfo, err := info.QueryPsInfo()
	if err != nil {
		return nil, err
	}

	for _, d := range psinfo.Public.Disks {
		if !funk.InStrings(fixer.opts.OfflineSysDisks, d.Device) {
			continue
		}

	}

	//
	// 候选路径：无签名的普通磁盘、有签名的磁盘分区、LV
	//

	for _, d := range psinfo.Private.Linux.Multipath {
		if !funk.InStrings(fixer.opts.OfflineSysDisks, d.Device) {
			continue
		}

	}

	for _, d := range psinfo.Private.Linux.Raid {
		_ = d
	}

	return nil, errors.New("implement me")
}
