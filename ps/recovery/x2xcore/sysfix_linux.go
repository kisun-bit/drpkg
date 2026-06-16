package x2xcore

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kisun-bit/drpkg/command"
	"github.com/kisun-bit/drpkg/define"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/ps/info"
	"github.com/kisun-bit/drpkg/ps/recovery/x2xlib"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
)

type linuxSystemFixer struct {
	ctx          context.Context
	opts         *FixerCreateOptions // 恢复参数
	logs         <-chan LogEntry     // 日志缓存通道
	x2xLib       *x2xlib.X2XLib      // 驱动库
	offsys       offlineSystem       // 离线系统的私有信息
	repairFinish bool
}

type offlineSystem struct {
	// crypto_LUKs设备
	luksDeviceList []LuksOpenResult

	// 探测到的文件系统设备（ext4/xfs/btrfs/swap 等）
	fsList []fsDevice

	// 文件系统最近一次挂载点映射
	// key: 文件系统设备
	// val: 最近一次挂载路径（如 "/", "/boot", "/var"）
	fsLastMount map[string]string

	// 文件系统设备
	devRoot  string   // 根文件系统设备（/）
	devBoot  string   // /boot 文件系统设备
	devEfi   string   // EFI System Partition（ESP）设备（/boot/efi）
	devVar   string   // /var 文件系统设备
	devUsr   string   // /usr 文件系统设备
	devSwaps []string // swap 设备列表

	// KVM 硬件配置
	kvmChipset     string // 主板芯片组（i440fx、q35）
	kvmVideo       string // 显卡类型（bochs、vga、virtio、ramfb）
	kvmDiskBus     string // 磁盘总线（ide、scsi、virtio、sata）
	kvmNetworkType string // 网卡类型（e1000e、rtl8192、virtio）

	// 启动模式（bios、uefi）
	bootMode define.BootMode

	// 磁盘设备映射关系（源设备 -> 目标设备）
	// 用于恢复后设备名变化映射（如 sda -> vda）
	devMaps []DeviceMap

	// 系统挂载顺序（从顶层到叶子节点）
	// 例如：["/", "/boot", "/boot/efi"]
	mounts []string

	// 离线系统根目录挂载点（如 /mnt/sysroot）
	root string

	// initrd/initramfs 生成工具
	initrdTl    string // dracut、mkinitrd、update-initramfs
	initrdTlVer string // 工具版本

	// 已安装内核列表
	kernels []kernel

	// grub 配置
	grubVer int    // 主版本（1: grub legacy, 2: grub2）
	grubCfg string // 配置文件路径（相对系统根目录）

	// 发行版信息（名称、版本、架构等）
	distro DistroInfo

	// 包管理器类型
	pkgMgrType PackageManager

	// udev 是否支持 UUID 寻址
	udevSupportUuid bool

	// supportSystemd 是否支持 systemd
	supportSystemd bool
}

type kernel struct {
	info.LinuxKernel
	KConfigs map[string]string `json:"-"`
}

func NewSysFixer(ctx context.Context, opts *FixerCreateOptions) (fixer SysFixer, err error) {
	logger.Debugf("NewSysFixer: opts:\n%s", extend.Pretty(opts))
	if err = CheckFixerCreateOptions(opts); err != nil {
		return nil, err
	}
	lf := &linuxSystemFixer{ctx: ctx, opts: opts, logs: make(<-chan LogEntry, 1000)}
	lf.x2xLib, err = x2xlib.NewX2XLib(opts.RecoveryParam.X2xLibrary, true)
	if err != nil {
		return nil, err
	}
	return lf, nil
}

// Prepare 准备修复环境（挂载/加载离线系统）
func (fixer *linuxSystemFixer) Prepare() error {
	logger.Debugf("Prepare: ++")
	defer logger.Debugf("Prepare: --")

	if err := fixer.closeAllLuksDevices(); err != nil {
		return errors.Wrap(err, "close crypto_LUKS")
	}

	if err := fixer.deactiveLvm(); err != nil {
		return errors.Wrap(err, "deactivate lvm")
	}

	if err := fixer.activeLVm(); err != nil {
		return errors.Wrap(err, "active lvm")
	}

	if err := fixer.openCryptoLUKS(); err != nil {
		return errors.Wrap(err, "open crypto_LUKS")
	}

	fsDevs, e := enumFilesystem(fixer.opts.OfflineSysDisks, fixer.offsys.luksDeviceList)
	if e != nil {
		return errors.Wrap(e, "enum filesystem")
	}

	fixer.offsys.fsList = append(fixer.offsys.fsList, fsDevs...)
	logger.Debugf("Prepare: fsList:\n%s",
		extend.Pretty(fixer.offsys.fsList))

	if err := fixer.detectLastMount(); err != nil {
		return errors.Wrap(err, "detect last mount")
	}

	if err := fixer.cleanDattoSnapshot(); err != nil {
		return errors.Wrap(err, "cleaning datto snapshot")
	}

	if err := fixer.detectSysDevice(); err != nil {
		return errors.Wrap(err, "detect sys device")
	}

	if err := fixer.mountSys(); err != nil {
		return errors.Wrap(err, "mounting offline system")
	}

	if err := fixer.detectKernels(); err != nil {
		return errors.Wrap(err, "detect kernels")
	}

	if err := fixer.detectGrub(); err != nil {
		return errors.Wrap(err, "detect grub")
	}

	if err := fixer.detectDistro(); err != nil {
		return errors.Wrap(err, "detect distro")
	}

	if err := fixer.deprecateSysMountPoint(); err != nil {
		return errors.Wrap(err, "deprecate sys mountpoint")
	}

	if err := fixer.detectPkgMgr(); err != nil {
		return errors.Wrap(err, "detect package manager")
	}

	if err := fixer.detectInitrdTool(); err != nil {
		return errors.Wrap(err, "detect initrd tool")
	}

	if err := fixer.detectDeviceMaps(); err != nil {
		return errors.Wrap(err, "detect device uuid")
	}

	fixer.detectKvmCfg()
	fixer.detectBootMode()
	fixer.detectUdevSupportUuid()

	fixer.offsys.supportSystemd = DetectSystemd(fixer.offsys.root)
	logger.Debugf("Prepare: supportSystemd=%v", fixer.offsys.supportSystemd)

	return nil
}

// Repair 执行修复流程
func (fixer *linuxSystemFixer) Repair() error {
	logger.Debugf("Repair: ++")
	defer logger.Debugf("Repair: --")

	defer func() {
		fixer.repairFinish = true
	}()

	if err := fixer.disableSeLinux(); err != nil {
		return errors.Wrap(err, "disable selinux")
	}

	if err := fixer.fixPamLogin(); err != nil {
		return errors.Wrap(err, "fix pam")
	}

	if err := fixer.fixEfiFirmware(); err != nil {
		return errors.Wrap(err, "fix efi-firmware")
	}

	if err := fixer.fixFstab(); err != nil {
		return errors.Wrap(err, "fix fstab")
	}

	if err := fixer.fixGrub(); err != nil {
		return errors.Wrap(err, "fix grub")
	}

	netijt, err := NewNetworkInjector(fixer.offsys.root, &fixer.opts.RecoveryParam.Network)
	if err != nil {
		// TODO 抛出警告
		logger.Warnf("NewNetworkInjector: %v", err)
	} else {
		if err = netijt.Inject(); err != nil {
			// TODO 抛出警告
			logger.Warnf("Inject: %v", err)
		}
	}

	var unconfigFun = fixer.unconfigBareMetal
	switch fixer.opts.RecoveryParam.Source.Virt {
	case define.HPVTXen:
		unconfigFun = fixer.unconfigXen
	case define.HPVTVmware:
		unconfigFun = fixer.unconfigVmware
	case define.HPVTKvm:
		unconfigFun = fixer.unconfigKvm
	case define.HPVTHyperV:
		unconfigFun = fixer.unconfigHyperV
	}

	var configFun = fixer.configBareMetal
	switch fixer.opts.RecoveryParam.Target.Virt {
	case define.HPVTXen:
		configFun = fixer.configXen
	case define.HPVTVmware:
		configFun = fixer.configVmware
	case define.HPVTKvm:
		configFun = fixer.configKvm
	case define.HPVTHyperV:
		configFun = fixer.configHyperV
	}

	if err := unconfigFun(); err != nil {
		return errors.Wrapf(err, "unconfig %s", fixer.opts.RecoveryParam.Source.Virt)
	}

	if err := configFun(); err != nil {
		return errors.Wrapf(err, "config %s", fixer.opts.RecoveryParam.Target.Virt)
	}

	return nil
}

func (fixer *linuxSystemFixer) CustomProcess(fn func() error) error {
	logger.Debugf("CustomProcess: ++")
	defer logger.Debugf("CustomProcess: --")

	if fn == nil {
		return errors.New("custom process is nil")
	}

	return fn()
}

// Cleanup 清理修复环境（卸载/释放资源）
func (fixer *linuxSystemFixer) Cleanup() error {
	logger.Debugf("Cleanup: ++")
	defer logger.Debugf("Cleanup: --")

	fixer.syncFs()

	if err := fixer.umountSys(); err != nil {
		return errors.Wrap(err, "umount sys")
	}

	if fixer.opts.RecoveryParam.FsckFs {
		fixer.fsckAllFs()
	}

	fixer.closeCryptoLUKS()

	return nil
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

func (fixer *linuxSystemFixer) GetPreferHostConfig(virtual define.HPVirtType) (cfg PreferConfig, err error) {
	if !fixer.repairFinish {
		return cfg, errors.New("please repair firstly")
	}

	switch virtual {
	case define.HPVTKvm:
		cfg.Chipset = fixer.offsys.kvmChipset
		cfg.Video = fixer.offsys.kvmVideo
		cfg.DiskBus = fixer.offsys.kvmDiskBus
		cfg.NetworkType = fixer.offsys.kvmNetworkType
		return cfg, nil
	default:
		return cfg, errors.New("GetPreferHostConfig: unsupported virtual type")
	}
}

// mountSys 挂载离线系统
func (fixer *linuxSystemFixer) mountSys() error {
	logger.Debugf("mountSys: ++")
	defer logger.Debugf("mountSys: --")

	if fixer.offsys.devRoot == "" {
		return errors.New("root filesystem is empty")
	}

	// 固定离线系统的挂载点
	if e := os.MkdirAll(rootDir, 0755); e != nil {
		return errors.Wrapf(e, "mkdir %s", rootDir)
	}
	_ = Umount(rootDir, true)

	if _, err := Mount(fixer.ctx, fixer.offsys.devRoot, rootDir, false); err != nil {
		return err
	}
	fixer.offsys.root = rootDir
	fixer.offsys.mounts = append(fixer.offsys.mounts, rootDir)

	if fixer.offsys.devUsr != "" {
		usrMountpoint := filepath.Join(rootDir, "usr")
		if _, err := Mount(fixer.ctx, fixer.offsys.devUsr, usrMountpoint, false); err != nil {
			return err
		}
		fixer.offsys.mounts = append(fixer.offsys.mounts, usrMountpoint)
	}

	if fixer.offsys.devBoot != "" {
		// FIXME：项目上发现，未修复的Boot可能影响新的initrd文件生成
		bootMountpoint := filepath.Join(rootDir, "boot")
		if _, err := Mount(fixer.ctx, fixer.offsys.devBoot, bootMountpoint, false); err != nil {
			return err
		}
		fixer.offsys.mounts = append(fixer.offsys.mounts, bootMountpoint)
	}

	if fixer.offsys.devEfi != "" {
		efiMountpoint := filepath.Join(rootDir, "boot", "efi")
		if extend.IsExisted(efiMountpoint) {
			if _, err := Mount(fixer.ctx, fixer.offsys.devEfi, efiMountpoint, false); err != nil {
				return err
			}
			fixer.offsys.mounts = append(fixer.offsys.mounts, efiMountpoint)
		}
	}

	if fixer.offsys.devVar != "" {
		varMountpoint := filepath.Join(rootDir, "var")
		if _, err := Mount(fixer.ctx, fixer.offsys.devVar, varMountpoint, false); err != nil {
			return err
		}
		fixer.offsys.mounts = append(fixer.offsys.mounts, varMountpoint)
	}

	chrootDevPath := filepath.Join(rootDir, "dev")
	chrootProcPath := filepath.Join(rootDir, "proc")
	chrootSysPath := filepath.Join(rootDir, "sys")
	chrootRunPath := filepath.Join(rootDir, "run")

	releatedMounts := map[string]string{
		chrootDevPath:  fmt.Sprintf("mount --bind /dev %s", chrootDevPath),
		chrootProcPath: fmt.Sprintf("mount -t proc procfs %s", chrootProcPath),
		chrootSysPath:  fmt.Sprintf("mount -t sysfs sysfs %s", chrootSysPath),
		chrootRunPath:  fmt.Sprintf("mount --bind /run %s", chrootRunPath),
	}

	// NOTE:
	// 1. 不要使用--rbind和--make-rslave，会造成卸载rootDir时vg资源释放不干净

	for mp, cmdline := range releatedMounts {
		if !extend.IsExisted(mp) {
			_ = os.MkdirAll(mp, 0755)
		}
		if _, _, e := command.Execute(cmdline, command.WithDebug()); e != nil {
			return e
		}

		fixer.offsys.mounts = append(fixer.offsys.mounts, mp)
	}

	fixer.offsys.root = rootDir
	return nil
}

func (fixer *linuxSystemFixer) umountSys() error {
	logger.Debugf("umountSys: ++")
	defer logger.Debugf("umountSys: --")

	for _, mp := range funk.ReverseStrings(fixer.offsys.mounts) {
		if err := Umount(mp, false); err != nil {
			logger.Warnf("umountSys: umount %s failed: %s", mp, err)
			return err
		}
	}

	return nil
}

func (fixer *linuxSystemFixer) deprecateSysMountPoint() error {
	logger.Debugf("deprecateSysMountPoint: ++")
	defer logger.Debugf("deprecateSysMountPoint: --")

	// 注意：
	// centos4/rhel4 等系统，若挂载了/dev到/mnt/sysroot/dev，那么在执行了mkinitrd之后，系统启动会报错，如下：
	// """
	// mount: error 6 mounting ext3
	// mount: error 2 mounting none
	// switchroot: mount failed: 22
	// umount /initrd/dev failed: 2
	// Kernel panic - not syncing: Attempted to kill init!
	// """

	if fixer.offsys.root == "" {
		return ErrorRootEnvNotMounted
	}

	umountDev := false
	if fixer.offsys.distro.Family == define.LinuxFamilyRHEL && fixer.offsys.distro.Major <= 4 {
		umountDev = true
	}
	// FIXME: 待补充，其他系统是否也存在此问题

	if !umountDev {
		return nil
	}

	chrootDevPath := filepath.Join(rootDir, "dev")
	newMounts := make([]string, 0)
	oldMounts := make([]string, len(fixer.offsys.mounts))
	copy(oldMounts[:], fixer.offsys.mounts[:])
	funk.ReverseStrings(oldMounts)

	for _, mp := range oldMounts {
		if !extend.IsExisted(mp) {
			continue
		}
		if strings.HasPrefix(mp, chrootDevPath+"/") || mp == chrootDevPath {
			logger.Debugf("deprecateSysMountPoint: umount %s", mp)
			_, _, e := command.Execute("umount " + mp)
			if e != nil {
				return errors.Wrapf(e, "umount %s", mp)
			}
			continue
		}
		newMounts = append(newMounts, mp)
	}

	logger.Debugf("deprecateSysMountPoint: mounts [before]:\n%s", extend.Pretty(fixer.offsys.mounts))
	fixer.offsys.mounts = funk.ReverseStrings(newMounts)
	logger.Debugf("deprecateSysMountPoint: mounts [after]:\n%s", extend.Pretty(fixer.offsys.mounts))

	return nil
}

func (fixer *linuxSystemFixer) closeAllLuksDevices() error {
	if _, _, e := command.Execute(`for dev in $(sudo dmsetup ls --target crypt | awk '{print $1}'); do
    sudo cryptsetup luksClose "$dev"
done`, command.WithDebug()); e != nil {
		return e
	}
	return nil
}

func (fixer *linuxSystemFixer) deactiveLvm() error {
	logger.Debugf("deactiveLvm ++")
	defer logger.Debugf("deactiveLvm --")

	return DeactivateVgs()
}

// activeLVm 激活LVM
func (fixer *linuxSystemFixer) activeLVm() error {
	logger.Debugf("activeLVm ++")
	defer logger.Debugf("activeLVm --")

	return ActivateVgs()
}

func (fixer *linuxSystemFixer) openCryptoLUKS() error {
	logger.Debugf("openCryptoLUKS: ++")
	defer logger.Debugf("openCryptoLUKS: --")

	if fixer.opts.RecoveryParam.LuksGlobalPassword == "" {
		return nil
	}

	rets, err := OpenAllLUKS(fixer.opts.RecoveryParam.LuksGlobalPassword)
	if err != nil {
		return err
	}

	for _, ret := range rets {
		if ret.Skipped {
			logger.Debugf("openCryptLUKS: skip %s(%s)", ret.Device, ret.Mapper)
			continue
		}
		logger.Debugf("openCryptLUKS: open %s -> %s", ret.Device, ret.Mapper)
		_, _, _ = command.Execute("partprobe "+ret.Mapper, command.WithDebug())

		fixer.offsys.luksDeviceList = append(fixer.offsys.luksDeviceList, ret)
	}

	if len(fixer.offsys.luksDeviceList) != 0 {
		if err = fixer.activeLVm(); err != nil {
			return errors.Wrap(err, "active lvm")
		}
	}

	return nil
}

// detectSysDevice 探测系统根环境
func (fixer *linuxSystemFixer) detectSysDevice() error {
	logger.Debugf("detectSysDevice ++")
	defer logger.Debugf("detectSysDevice --")

	//if len(fixer.offsys.fsList) == 0 {
	//	fsDevs, err := enumFilesystem(fixer.opts.OfflineSysDisks)
	//	if err != nil {
	//		return err
	//	}
	//	fixer.offsys.fsList = fsDevs
	//}

	for _, dev := range fixer.offsys.fsList {
		if dev.FsType == "swap" {
			continue
		}
		switch {
		case IsRootDevice(fixer.ctx, dev.Device):
			fixer.offsys.devRoot = dev.Device
		case IsBootDevice(fixer.ctx, dev.Device):
			fixer.offsys.devBoot = dev.Device
		case IsEfiDevice(fixer.ctx, dev.Device):
			fixer.offsys.devEfi = dev.Device
		case IsVarDevice(fixer.ctx, dev.Device):
			fixer.offsys.devVar = dev.Device
		case IsUsrDevice(fixer.ctx, dev.Device):
			fixer.offsys.devUsr = dev.Device
		}
	}

	logger.Debugf("detectSysDevice: root=`%s`, boot=`%s`, efi=`%s`, var=`%s`, usr=`%s`",
		fixer.offsys.devRoot, fixer.offsys.devBoot, fixer.offsys.devEfi, fixer.offsys.devVar, fixer.offsys.devUsr)

	fixer.offsys.devSwaps = make([]string, 0)
	for _, dev := range fixer.offsys.fsList {
		if dev.FsType == "swap" {
			fixer.offsys.devSwaps = append(fixer.offsys.devSwaps, dev.Device)
			continue
		}
	}
	logger.Debugf("detectSysDevice: swap=`%s`", strings.Join(fixer.offsys.devSwaps, ","))

	if fixer.offsys.devRoot == "" {
		return errors.Errorf("no root filesystem device detected from candidates: %v", fixer.offsys.fsList)
	}

	return nil
}

type initrdTool struct {
	name        string
	featureFile string
	cmd         string
}

var initrdTools = []initrdTool{
	{
		name:        define.InitrdToolDracut,
		featureFile: "/etc/dracut.conf",
		cmd:         define.InitrdToolDracut,
	},
	{
		name:        define.InitrdToolUpdateInitramfs,
		featureFile: "/etc/initramfs-tools/update-initramfs.conf",
		cmd:         define.InitrdToolUpdateInitramfs,
	},
	{
		name: define.InitrdToolMkinitrd,
		cmd:  define.InitrdToolMkinitrd,
	},
}

// detectInitrdTool 探测initrd生成工具
func (fixer *linuxSystemFixer) detectInitrdTool() error {
	logger.Debugf("detectInitrdTool ++")
	defer logger.Debugf("detectInitrdTool --")

	if fixer.offsys.root == "" {
		return ErrorRootEnvNotMounted
	}

	for _, tool := range initrdTools {
		// 1. feature file 检测（如果有）
		if tool.featureFile != "" {
			path := filepath.Join(fixer.offsys.root, tool.featureFile)
			if !extend.IsExisted(path) {
				continue
			}
		}

		// 2. 命令探测
		rc, _, _ := fixer.executeWithChroot(tool.cmd + " --e1y728ety172eg")

		// 3. 127 = not found
		if rc == 127 {
			logger.Debugf("initrd tool %s not found (rc=127)", tool.name)
			continue
		}

		// 4. 成功命中
		logger.Debugf("detectInitrdTool: initrd tool detected: %s", tool.name)
		fixer.offsys.initrdTl = tool.name

		initrdTlVerCmdline := ""
		switch tool.name {
		case define.InitrdToolMkinitrd:
			if fixer.offsys.pkgMgrType == PackageManagerRPM {
				initrdTlVerCmdline = `rpm -q mkinitrd`
				break
			}
		case define.InitrdToolDracut:
			// TODO
		case define.InitrdToolUpdateInitramfs:
			// TODO
		}

		if initrdTlVerCmdline != "" {
			_, o, e := fixer.executeWithChroot(initrdTlVerCmdline)
			if e == nil && strings.TrimSpace(o) != "" {
				fixer.offsys.initrdTlVer = strings.TrimSpace(o)
				logger.Debugf("detectInitrdTool: initrdTlVer=`%s`", fixer.offsys.initrdTlVer)
			}
		}

		return nil
	}

	return errors.New("no initramfs tool detected")
}

// detectLastMount 探测文件系统最近一次挂载点
func (fixer *linuxSystemFixer) detectLastMount() error {
	logger.Debugf("detectLastMount ++")
	defer logger.Debugf("detectLastMount --")

	fixer.offsys.fsLastMount = make(map[string]string)

	for _, fdev := range fixer.offsys.fsList {
		ft, _ := DetectFSTypeByBlkid(fdev.Device)

		switch ft {
		case "ext2", "ext3", "ext4":

			logger.Debugf(
				"detectLastMount: ext filesystem detected: %s, start to query last mountpoint",
				fdev,
			)

			_, output, err := command.Execute("dumpe2fs -h " + fdev.Device)
			if err != nil {
				logger.Warnf(
					"detectLastMount: dumpe2fs failed for %s: %v",
					fdev,
					err,
				)
				continue
			}

			var (
				volumeName string
				lastMount  string
			)

			scanner := bufio.NewScanner(strings.NewReader(output))

			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())

				// Filesystem volume name:   /boot
				if strings.HasPrefix(line, "Filesystem volume name:") {
					v := strings.TrimSpace(
						strings.TrimPrefix(
							line,
							"Filesystem volume name:",
						),
					)

					if strings.HasPrefix(v, "/") &&
						v != "<none>" &&
						v != "<not available>" {
						volumeName = v
					}

					continue
				}

				// Last mounted on:          /
				if strings.HasPrefix(line, "Last mounted on:") {
					v := strings.TrimSpace(
						strings.TrimPrefix(
							line,
							"Last mounted on:",
						),
					)

					if strings.HasPrefix(v, "/") &&
						v != "<none>" &&
						v != "<not available>" {
						lastMount = v
					}

					continue
				}
			}

			if err = scanner.Err(); err != nil {
				logger.Warnf(
					"detectLastMount: scanner failed for %s: %v",
					fdev,
					err,
				)
			}

			// 原则：
			// 优先 Filesystem volume name
			// 其次 Last mounted on
			if volumeName != "" {
				fixer.offsys.fsLastMount[fdev.Device] = volumeName

				logger.Debugf(
					"detectLastMount: %s -> %s (from `volume name`)",
					fdev,
					volumeName,
				)
			} else if lastMount != "" {
				fixer.offsys.fsLastMount[fdev.Device] = lastMount

				logger.Debugf(
					"detectLastMount: %s -> %s (from `last mounted on`)",
					fdev,
					lastMount,
				)
			} else {
				logger.Debugf(
					"detectLastMount: %s no valid mountpoint found",
					fdev,
				)
			}
		}
	}

	return nil
}

// detectDeviceMaps 探测设备映射表
func (fixer *linuxSystemFixer) detectDeviceMaps() error {
	logger.Debugf("detectDeviceMaps ++")
	defer logger.Debugf("detectDeviceMaps --")

	if fixer.offsys.root == "" {
		return ErrorRootEnvNotMounted
	}

	if len(fixer.opts.RecoveryParam.SourceDeviceMap) != 0 {
		logger.Debugf("detectDeviceMaps: source device map detected")
		fixer.offsys.devMaps = fixer.opts.RecoveryParam.SourceDeviceMap
		return nil
	}

	fixer.offsys.devMaps = make([]DeviceMap, 0)

	//
	// 基于文件系统最近一次挂载点进行补充
	//

	for fsDev, lastMount := range fixer.offsys.fsLastMount {
		uuidStr, _ := DetectUuidByBlkid(fsDev)
		fixer.offsys.devMaps = append(fixer.offsys.devMaps,
			DeviceMap{
				Origin:     "",
				Mountpoint: lastMount,
				UUID:       uuidStr,
			},
		)
	}

	//
	// 基于探测的系统卷设备进行补充
	//

	if fixer.offsys.devRoot != "" {
		uuidStr, _ := DetectUuidByBlkid(fixer.offsys.devRoot)
		fixer.offsys.devMaps = append(fixer.offsys.devMaps,
			DeviceMap{
				Origin:     "",
				Mountpoint: "/",
				UUID:       uuidStr,
			},
		)
	}
	if fixer.offsys.devBoot != "" {
		uuidStr, _ := DetectUuidByBlkid(fixer.offsys.devBoot)
		fixer.offsys.devMaps = append(fixer.offsys.devMaps,
			DeviceMap{
				Origin:     "",
				Mountpoint: "/boot",
				UUID:       uuidStr,
			},
		)
	}
	if fixer.offsys.devEfi != "" {
		uuidStr, _ := DetectUuidByBlkid(fixer.offsys.devEfi)
		fixer.offsys.devMaps = append(fixer.offsys.devMaps,
			DeviceMap{
				Origin:     "",
				Mountpoint: "/boot/efi",
				UUID:       uuidStr,
			},
		)
	}
	if fixer.offsys.devVar != "" {
		uuidStr, _ := DetectUuidByBlkid(fixer.offsys.devVar)
		fixer.offsys.devMaps = append(fixer.offsys.devMaps,
			DeviceMap{
				Origin:     "",
				Mountpoint: "/var",
				UUID:       uuidStr,
			},
		)
	}
	if fixer.offsys.devUsr != "" {
		uuidStr, _ := DetectUuidByBlkid(fixer.offsys.devUsr)
		fixer.offsys.devMaps = append(fixer.offsys.devMaps,
			DeviceMap{
				Origin:     "",
				Mountpoint: "/usr",
				UUID:       uuidStr,
			},
		)
	}

	//
	// 基于blkid的缓存文件进行探测
	//

	//
	// /run/blkid/blkid.tab：新版 Linux（systemd / util-linux 新版本），处于内存文件系统不可作为探测文件
	// /etc/blkid.tab：老系统，可作为探测文件
	// /dev/.blkid.tab：老系统，处于内存文件系统不可作为探测文件
	//

	blkidCacheFile := filepath.Join(fixer.offsys.root, "etc/blkid.tab")
	if extend.IsExisted(blkidCacheFile) {
		// <device DEVNO="0xfd01" TIME="1779244003" PRI="45" TYPE="swap">/dev/mapper/VolGroup00-LogVol01</device>
		// <device DEVNO="0xfd00" TIME="1779244003" PRI="45" UUID="c007fd05-8d6d-47ee-bf30-7bffb2fc2896" TYPE="ext3">/dev/mapper/VolGroup00-LogVol00</device>
		// <device DEVNO="0x0801" TIME="1779244003" LABEL="/boot" UUID="60a80797-2810-48e7-93ed-599c3648c5d7" TYPE="ext3" SEC_TYPE="ext2">/dev/sda1</device>
		// <device DEVNO="0x0300" TIME="1631714685" LABEL="VMware Tools" TYPE="iso9660">/dev/hda</device>
		// <device DEVNO="0xfd00" TIME="1779244003" UUID="c007fd05-8d6d-47ee-bf30-7bffb2fc2896" TYPE="ext3">/dev/VolGroup00/LogVol00</device>
		// <device DEVNO="0xfd01" TIME="1779244003" TYPE="swap">/dev/VolGroup00/LogVol01</device>
		// <device DEVNO="0x0300" TIME="1669787575" LABEL="CentOS_5.6_Final" TYPE="iso9660">/dev/cdrom</device>
		blkidContent, err := os.ReadFile(blkidCacheFile)
		if err != nil {
			logger.Warnf("detectDeviceMaps: failed to read blkid.tab: %v", err)
		}
		logger.Debugf("detectDeviceMaps: blkid.tab: %s", string(blkidContent))
		blkidCacheRe := regexp.MustCompile(`UUID="([0-9a-fA-F-]+)".*?>(/dev/[^<]+)</device>`)
		matches := blkidCacheRe.FindAllStringSubmatch(string(blkidContent), -1)
		if len(matches) != 0 {
			for i := len(matches) - 1; i >= 0; i-- {
				m := matches[i]
				uuid := m[1]
				dev := m[2]
				fixer.offsys.devMaps = append(fixer.offsys.devMaps,
					DeviceMap{
						Origin:     dev,
						Mountpoint: "",
						UUID:       uuid,
					})
				logger.Debugf("detectDeviceMaps: Detecting blkid.tab. uuid=%s device=%s ", uuid, dev)
			}
		}
	}

	//
	// 基于系统启动日志进行补充
	//

	// 文件系统：btrfs
	// 命令：grep -i "btrfs" /var/log/messages* | grep fsid
	// 结果：
	// 2025-09-23T17:05:40.910399+08:00 localhost kernel: [    4.514460] BTRFS: device fsid b3334026-ccb6-4c25-82f4-e94e72dab24e devid 1 transid 63 /dev/dm-1
	// 2026-05-12T14:27:56.262124+08:00 localhost kernel: [    6.154912] BTRFS info (device dm-0): device fsid b3334026-ccb6-4c25-82f4-e94e72dab24e devid 1 moved old:/dev/dm-0 new:/dev/mapper/system-root

	syslogDetectBtrfsCmd := fmt.Sprintf(`grep -i "btrfs" %s/var/log/messages* | grep fsid`, fixer.offsys.root)
	_, o, _ := command.Execute(syslogDetectBtrfsCmd)
	btrfsUuids := make([]string, 0)
	btrfsUuidRe := regexp.MustCompile(`fsid\s+([a-fA-F0-9-]+).*?(/dev/\S+)`)
	btrfsOutputLines := funk.ReverseStrings(strings.Split(o, "\n"))
	for _, line := range btrfsOutputLines {
		matches := btrfsUuidRe.FindStringSubmatch(line)
		if len(matches) == 3 {
			fsid := matches[1]
			dev := matches[2]
			if funk.InStrings(btrfsUuids, fsid) {
				continue
			}
			fixer.offsys.devMaps = append(fixer.offsys.devMaps,
				DeviceMap{
					Origin:     dev,
					Mountpoint: "",
					UUID:       fsid,
				})
			btrfsUuids = append(btrfsUuids, fsid)
			logger.Debugf("detectDeviceMaps: Detecting btrfs by syslog. line=`%s` uuid=`%s` device=`%s`", line, fsid, dev)
		}
	}

	// 文件系统：xfs
	// 命令：grep -i "kernel: xfs" /var/log/* | grep -i mounting
	// 结果：
	// /var/log/messages-20260517:May  9 16:04:22 node1 kernel: XFS (dm-0): Mounting V5 Filesystem 48153c2d-a7a3-4f37-8998-dfe87705ebcb
	// /var/log/messages-20260517:May  9 16:04:24 node1 kernel: XFS (sda2): Mounting V5 Filesystem 9d6bbc00-4ff1-42fd-8b34-5ffad8f2fbc8

	syslogDetectXfsCmd := fmt.Sprintf(
		`grep -i "kernel: xfs" %s/var/log/* | grep -i mounting`,
		fixer.offsys.root,
	)

	_, o, _ = command.Execute(syslogDetectXfsCmd)

	xfsUuids := make([]string, 0)

	// 匹配：XFS (dm-0): Mounting V5 Filesystem UUID
	xfsUuidRe := regexp.MustCompile(
		`XFS\s+\(([^)]+)\):.*?Filesystem\s+([a-fA-F0-9-]+)`,
	)
	xfsOutputLines := funk.ReverseStrings(strings.Split(o, "\n"))

	for _, line := range xfsOutputLines {
		matches := xfsUuidRe.FindStringSubmatch(line)
		if len(matches) == 3 {
			dev := matches[1]
			uuid := matches[2]

			// 补齐 /dev/
			if !strings.HasPrefix(dev, "/dev/") {
				dev = "/dev/" + dev
			}

			if funk.InStrings(xfsUuids, uuid) {
				continue
			}

			fixer.offsys.devMaps = append(
				fixer.offsys.devMaps,
				DeviceMap{
					Origin:     dev,
					Mountpoint: "",
					UUID:       uuid,
				},
			)

			xfsUuids = append(xfsUuids, uuid)

			logger.Debugf(
				"detectDeviceMaps: Detecting xfs by syslog. line=`%s` uuid=`%s` device=`%s`",
				line,
				uuid,
				dev,
			)
		}
	}

	logger.Debugf("detectDeviceMaps: Device map:\n%s", extend.Pretty(fixer.offsys.devMaps))

	return nil
}

// detectKvmCfg 探测KVM虚机的推荐配置
func (fixer *linuxSystemFixer) detectKvmCfg() {
	logger.Debugf("detectKvmCfg ++")
	defer logger.Debugf("detectKvmCfg --")

	// virt-v2v 的 machine type 原则：
	//
	// "以 2007 为分界时间线，早于 2007 发布的 Linux 使用 i440fx，
	// 2007 及之后发布的 Linux 使用 q35。"

	chipset := define.ChipsetQ35

	switch fixer.offsys.distro.ID {
	case "fedora":
		break
	case "rhel", "centos", "circle", "scientificlinux", "redhat-based", "oraclelinux", "rocky":
		//if fixer.offlineSystem.distro.Major <= 4 {
		//	chipset = ChipsetI440fx
		//}
		if fixer.offsys.distro.Major <= 6 {
			chipset = define.ChipsetI440fx
		}
	case "sles", "suse-based", "opensuse":
		//if fixer.offlineSystem.distro.Major <= 10 {
		//	chipset = ChipsetI440fx
		//}
		if fixer.offsys.distro.Major <= 12 {
			chipset = define.ChipsetI440fx
		}
	case "debian", "ubuntu", "linuxmint", "kaillinux":
		if fixer.offsys.distro.Major <= 4 {
			chipset = define.ChipsetI440fx
		}
	}

	fixer.offsys.kvmChipset = chipset
	logger.Debugf("detectChipset: chipset detected: %s", chipset)

	fixer.offsys.kvmVideo = define.VideoBochs
	// FIXME: 后续不要根据主板去决定显卡类型
	if fixer.offsys.kvmChipset == define.ChipsetQ35 {
		fixer.offsys.kvmVideo = define.VideoVGA
	}

	fixer.offsys.kvmDiskBus = define.DiskBusVirtioScsi
	fixer.offsys.kvmNetworkType = define.NetworkTypeE1000

	// FIXME:
	// 但在实际测试中发现，SUSE11 SP4（kernel 3.0.101）是一个例外：
	//
	// 当 initrd 已包含 VirtIO 驱动：
	//   virtio
	//   virtio_ring
	//   virtio_pci
	//   virtio_blk
	//   virtio_scsi
	//   virtio_net
	//
	// 启动兼容性如下：
	//
	// 1. root disk 使用 virtio-blk / virtio-scsi：
	//    - i440fx：可正常启动
	//    - q35：无法启动
	//
	//    原因：
	//    SUSE11 SP4 的 virtio 驱动较旧，对 q35 (PCIe +
	//    modern virtio) 兼容性较差，表现为：
	//      - lspci 可见 virtio-scsi controller
	//      - virtio_scsi 驱动无法 bind controller
	//      - root disk 无法枚举
	//      - initrd 无法找到 root device
	//
	// 2. root disk 使用 IDE / SATA（AHCI）：
	//    - i440fx：可正常启动
	//    - q35：可正常启动
	//
	// 因此：
	// 对于 SUSE11 SP4，如果 root disk bus 为
	// virtio / virtio-scsi，建议强制使用 i440fx。
	// 若使用 IDE/SATA，则可允许 q35。
}

func (fixer *linuxSystemFixer) detectBootMode() {
	logger.Debugf("detectBootMode: ++")
	defer logger.Debugf("detectBootMode: --")

	if fixer.offsys.root == "" {
		return
	}

	fixer.offsys.bootMode = define.BootModeBIOS

	efiDir := filepath.Join(fixer.offsys.root, "boot/efi")
	if extend.IsExisted(efiDir) {
		_ = filepath.WalkDir(efiDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if extend.IsNilType(d) {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if filename := strings.ToLower(filepath.Base(path)); strings.HasSuffix(filename, ".efi") {
				fixer.offsys.bootMode = define.BootModeUEFI
				return filepath.SkipAll
			}
			return nil
		})
	}

	logger.Debugf("detectBootMode: boot mode is `%s`", fixer.offsys.bootMode)
}

func (fixer *linuxSystemFixer) detectUdevSupportUuid() {
	logger.Debugf("detectUdevSupportUuid: ++")
	defer logger.Debugf("detectUdevSupportUuid: --")

	if fixer.offsys.root == "" {
		return
	}

	fixer.offsys.udevSupportUuid = true

	switch fixer.offsys.distro.Family {
	case define.LinuxFamilyRHEL:
		if fixer.offsys.distro.Major <= 4 {
			fixer.offsys.udevSupportUuid = false
		}
	}

	logger.Debugf("detectUdevSupportUuid: udevSupportUuid=`%v`", fixer.offsys.udevSupportUuid)

	if !fixer.offsys.udevSupportUuid {
		// TODO 警告不支持UUID，grub.cfg、fstab将无法得到更新
		logger.Warnf("detectUdevSupportUuid: Unsupported udev-uuid")
	}
}

// detectKernels 探测离线系统的内核
func (fixer *linuxSystemFixer) detectKernels() error {
	logger.Debugf("detectKernels: ++")
	defer logger.Debugf("detectKernels: --")

	if fixer.offsys.root == "" {
		return ErrorRootEnvNotMounted
	}

	ks, err := info.QueryLinuxKernels(fixer.offsys.root)
	if err != nil {
		return err
	}

	// 过滤掉非启动内核
	ks2 := make([]kernel, 0)
	for _, k := range ks {
		if !k.Bootable {
			continue
		}

		k2 := kernel{}
		k2.LinuxKernel = k
		k2.KConfigs = make(map[string]string)

		if k.Config != "" {
			cfgs, ec := Kconfig(filepath.Join(fixer.offsys.root, "boot", k.Config))
			if ec != nil {
				logger.Warnf("detectKernels: failed to parse kernel configs for %s: %v", k.Name, ec)
			} else {
				k2.KConfigs = cfgs
			}
		}

		ks2 = append(ks2, k2)
	}

	fixer.offsys.kernels = ks2
	logger.Debugf("detectKernels: bootable kernels:\n%s", extend.Pretty(ks2))

	return nil
}

// linuxSystemFixer 探测离线系统的grub
func (fixer *linuxSystemFixer) detectGrub() error {
	logger.Debugf("detectGrub: ++")
	defer logger.Debugf("detectGrub: --")

	if fixer.offsys.root == "" {
		return ErrorRootEnvNotMounted
	}

	ver, cfg := DetectGrub(fixer.offsys.root)
	if ver == -1 {
		return errors.New("grub not found")
	}

	fixer.offsys.grubVer, fixer.offsys.grubCfg = ver, cfg
	logger.Debugf("detectGrub: version=%d config=%s", ver, cfg)

	return nil
}

// linuxSystemFixer 探测Linux发行版信息
func (fixer *linuxSystemFixer) detectDistro() error {
	logger.Debugf("detectDistro: ++")
	defer logger.Debugf("detectDistro: --")

	if fixer.offsys.root == "" {
		return ErrorRootEnvNotMounted
	}

	distro, err := DetectDistro(fixer.offsys.root)
	if err != nil {
		return err
	}

	fixer.offsys.distro = *distro
	logger.Debugf("detectDistro: distro=\n%s", extend.Pretty(distro))

	return nil
}

func (fixer *linuxSystemFixer) detectPkgMgr() error {
	logger.Debugf("detectPkgMgr: ++")
	defer logger.Debugf("detectPkgMgr: --")

	if fixer.offsys.root == "" {
		return ErrorRootEnvNotMounted
	}

	pm := DetectPackageManager(fixer.offsys.root)
	logger.Debugf("detectPkgMgr: pkgmgr=`%s`", pm)

	fixer.offsys.pkgMgrType = pm
	return nil
}

// executeWithChroot 在chroot环境执行命令
func (fixer *linuxSystemFixer) executeWithChroot(cmdline string) (exitcode int, output string, err error) {
	if fixer.offsys.root == "" {
		return -1, "", ErrorRootEnvNotMounted
	}

	chrootCmdline := fmt.Sprintf(
		`chroot %s /bin/bash -c "export PATH=/sbin:/bin:/usr/sbin:/usr/bin:/usr/local/sbin:/usr/local/bin:$PATH; %s"`,
		fixer.offsys.root, cmdline)

	return command.Execute(chrootCmdline, command.WithDebug())
}

// cleanDattoSnapshot 清理 datto(/elastio) 快照
func (fixer *linuxSystemFixer) cleanDattoSnapshot() error {
	logger.Debugf("cleanDattoSnapshot: ++")
	defer logger.Debugf("cleanDattoSnapshot: --")

	tmpMp, err := os.MkdirTemp("", "cleanDattoCow-*")
	if err != nil {
		return err
	}
	defer func() {
		if extend.IsEmptyDir(tmpMp) {
			_ = os.RemoveAll(tmpMp)
		}
	}()

	logger.Debugf("cleanDattoSnapshot: tmpMp=%s", tmpMp)

	for _, dev := range fixer.offsys.fsList {
		if dev.FsType == "swap" {
			continue
		}

		if err := fixer.cleanDattoSnapshotOnDevice(dev, tmpMp); err != nil {
			logger.Warnf("cleanDattoSnapshot: device=%s err=%v", dev.Device, err)
		}
	}

	return nil
}

func (fixer *linuxSystemFixer) cleanDattoSnapshotOnDevice(
	dev fsDevice,
	tmpMp string,
) error {
	_, err := Mount(fixer.ctx, dev.Device, tmpMp, false)
	if err != nil {
		return fmt.Errorf("mount %s: %w", dev.Device, err)
	}
	defer func() {
		if err := Umount(tmpMp, false); err != nil {
			logger.Warnf("cleanDattoSnapshot: umount %s failed: %v", tmpMp, err)
		}
	}()

	logger.Debugf("cleanDattoSnapshot: device=%s", dev.Device)

	candDirs := []string{
		filepath.Join(tmpMp, "lost+found"),
		filepath.Join(tmpMp, ".runstorsnap"),
	}

	for _, cand := range candDirs {
		foundCmdline := fmt.Sprintf("find %s -name '*.cow'", cand)

		_, output, err := command.Execute(foundCmdline)
		if err != nil {
			continue
		}

		for _, line := range strings.Split(output, "\n") {
			cowPath := strings.TrimSpace(line)
			if cowPath == "" {
				continue
			}

			rmCmdline := fmt.Sprintf(
				"chattr -i %q && rm -f %q",
				cowPath,
				cowPath,
			)

			_, _, err := command.Execute(rmCmdline)
			if err != nil {
				logger.Warnf(
					"cleanDattoSnapshot: remove %s failed: %v",
					cowPath,
					err,
				)
				continue
			}

			logger.Debugf("cleanDattoSnapshot: removed %s", cowPath)
		}
	}

	return nil
}

// fixFstab 修复fstab
func (fixer *linuxSystemFixer) fixFstab() error {
	logger.Debugf("fixFstab: ++")
	defer logger.Debugf("fixFstab: --")

	if fixer.offsys.root == "" {
		return ErrorRootEnvNotMounted
	}

	fstabPath := filepath.Join(fixer.offsys.root, "etc/fstab")

	//
	// 编辑
	// fstab结构如下：
	// <file system> <mount point>   <type>  <options>       <dump>  <pass>
	//

	contentBin, err := os.ReadFile(fstabPath)
	if err != nil {
		return err
	}
	content := string(contentBin)
	logger.Debugf("fixFstab: history configuration:\n%s", content)

	newContentLines := make([]string, 0)
	changed := false
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			newContentLines = append(newContentLines, line)
			continue
		}
		items := strings.Fields(line)
		if len(items) < 6 {
			newContentLines = append(newContentLines, line)
			continue
		}

		// 避免迁移后设备ID出现变化，
		// 一般而言稳定性如下：
		// 安全：by-partuuid、by-uuid、by-label、tmpfs/proc/sysfs/devpts、lv

		if isValidFstabDevice(items[0]) ||
			funk.InStrings([]string{"tmpfs", "devpts", "sysfs", "proc", "debugfs"}, items[2]) {
			newContentLines = append(newContentLines, line)
			continue
		}

		uuid := ""

		// 修复swap
		if len(fixer.offsys.devSwaps) == 1 && items[2] == "swap" {
			logger.Debugf("fixFstab: Prepare to fix `%s`", line)
			swapDev := fixer.offsys.devSwaps[0]
			uuid, _ = DetectUuidByBlkid(swapDev)
		}

		// 基于匹配`挂载路径`进行`设备`的修复
		if uuid == "" {
			for _, dm := range fixer.offsys.devMaps {
				if dm.Mountpoint == items[1] && strings.HasPrefix(dm.Mountpoint, "/") {
					uuid = dm.UUID
					break
				}
			}
		}

		// 基于匹配`设备`进行`设备`的修复
		if uuid == "" {
			for _, dm := range fixer.offsys.devMaps {
				if dm.Origin == items[0] && dm.UUID != "" {
					uuid = dm.UUID
					break
				}
			}
		}

		if uuid != "" && fixer.offsys.udevSupportUuid {
			logger.Debugf("fixFstab: Old configuration: `%s`", line)
			items[0] = fmt.Sprintf("/dev/disk/by-uuid/%s", uuid)
			newLine := strings.Join(items, "    ")
			logger.Debugf("fixFstab: New configuration: `%s`", newLine)
			newContentLines = append(newContentLines, "# "+line)
			newContentLines = append(newContentLines, newLine)
			changed = true
			continue
		}

		// TODO 抛出警告，提示可能存在恢复后系统无法启动的情况
		logger.Warnf("fixFstab: Warn-Config: `%s`", line)
		newContentLines = append(newContentLines, line)
	}

	if !changed {
		logger.Debugf("fixFstab: No changes detected")
		return nil
	}

	// 备份历史配置
	fstabBkPath := filepath.Join(fixer.offsys.root, fmt.Sprintf("etc/fstab.bk.%d", time.Now().Unix()))
	_, _, e := command.Execute("cp " + fstabPath + " " + fstabBkPath)
	if e != nil {
		return errors.Wrap(e, "backup /etc/fstab")
	}
	logger.Debugf("fixFstab: copy `etc/fstab` to `%s`", fstabBkPath)

	newContent := strings.Join(newContentLines, "\n")
	logger.Debugf("fixFstab: new configuration:\n%s", newContent)

	return os.WriteFile(fstabPath, []byte(newContent), 0644)
}

// fixEfiFirmware 修复Efi固件
func (fixer *linuxSystemFixer) fixEfiFirmware() error {
	logger.Debugf("fixEfiFirmware: ++")
	defer logger.Debugf("fixEfiFirmware: --")

	if fixer.offsys.bootMode != define.BootModeUEFI {
		logger.Debugf("fixEfiFirmware: Ignored when bootmode is `%s`", fixer.offsys.bootMode)
		return nil
	}

	// 已知规则：
	// 若 EFI/BOOT/BOOT{ARCH}.EFI 缺失，则进行补充：
	//   情况1：未开启 Secure Boot
	//     - 将 \EFI\{distro}\grub{arch}.efi 拷贝至 EFI/BOOT/BOOT{ARCH}.EFI
	//     - 或创建 startup.nsh，内容为引导该 grub.efi 的路径
	//   情况2：已开启 Secure Boot
	//     - 将 \EFI\{distro}\shim{arch}.efi 拷贝至 EFI/BOOT/BOOT{ARCH}.EFI
	//     - 同时确保同目录下有对应的 grub.efi（shim 需要）
	//
	// 若 EFI/BOOT/BOOT{ARCH}.EFI 存在，则进行修正：
	//   情况1：未开启 Secure Boot
	//     - 用 \EFI\{distro}\grub{arch}.efi 覆盖 BOOT{ARCH}.EFI
	//   情况2：已开启 Secure Boot
	//     - 用 \EFI\{distro}\shim{arch}.efi 覆盖 BOOT{ARCH}.EFI
	//     - 确保 grub.efi 也更新到 shim 期望的位置

	// 实际测试中发现：
	// 欧拉24系统，在未开启secure boot的情况下，使用shimx64.efi启动，系统能够正常启动
	// 麒麟v10系统，在未开启secure boot的情况下，使用默认的bootaa64.efi无法启动，使用grubaa64.efi能够启动
	// 因为国产信创系统会对shim进行魔改，那么针对非国产信创系统（如麒麟v10，统信），我们就忽略BOOT*.EFI的覆盖拷贝操作

	// 修复原则：
	// 若 EFI/BOOT/BOOT{ARCH}.EFI 缺失，则使用shim*.efi、grub*.efi路径写入start.nsh
	// 若 EFI/BOOT/BOOT{ARCH}.EFI 存在，则使用shim*.efi、grub*.efi进行覆盖

	efiImgName, ok := getEfiImageName()
	if !ok {
		return errors.New("no matched efi image name")
	}
	logger.Debugf("fixEfiFirmware: efiImgName:\n%s", extend.Pretty(efiImgName))

	findFirstEfi := func(globs []string) string {
		for _, fglob := range globs {
			logger.Debugf("fixEfiFirmware: Glob `%s`", fglob)

			files, err := filepath.Glob(fglob)
			if err != nil {
				logger.Warnf("fixEfiFirmware: Glob error: `%s`", err)
				continue
			}

			logger.Debugf(
				"fixEfiFirmware: Glob `%s`:\n%s",
				fglob,
				strings.Join(files, "\n"),
			)

			for _, f := range files {
				efiDirName := filepath.Base(filepath.Dir(f))
				if strings.EqualFold(efiDirName, "boot") {
					continue
				}
				return f
			}
		}

		return ""
	}

	efiRoot := filepath.Join(fixer.offsys.root, "boot/efi/EFI")
	uefiFallbackDir := filepath.Join(efiRoot, "BOOT")
	uefiFallbackPath := filepath.Join(uefiFallbackDir, efiImgName.Default)

	defaultEfiPath := ""
	if extend.IsExisted(uefiFallbackPath) {
		defaultEfiPath = uefiFallbackPath
	}

	grubEfiPath := findFirstEfi([]string{
		filepath.Join(efiRoot, "*", efiImgName.Grub),
		filepath.Join(efiRoot, "*", "grub.efi"),
		filepath.Join(efiRoot, "*", "elilo.efi"),
	})

	shimEfiPath := findFirstEfi([]string{
		filepath.Join(efiRoot, "*", efiImgName.Shim),
		filepath.Join(efiRoot, "*", "shim.efi"),
	})

	//
	// 注意：
	// UEFI 安全启动（Secure Boot）的设计初衷是防范底层恶意软件，其信任根源掌握在主板厂商（OEM）手中，绝非仅限微软。
	// shim*.efi 说明：持有微软签名
	// grub*.efi 说明：通常无签名（或由发行版自签名））
	//

	logger.Debugf("fixEfiFirmware: defaultEfiPath=`%s`", defaultEfiPath)
	logger.Debugf("fixEfiFirmware: grubEfiPath=`%s`", grubEfiPath)
	logger.Debugf("fixEfiFirmware: shimEfiPath=`%s`", shimEfiPath)

	if defaultEfiPath == "" && grubEfiPath == "" && shimEfiPath == "" {
		return nil
	}

	if defaultEfiPath == "" {
		startupScript := filepath.Join(uefiFallbackDir, "startup.nsh")
		if extend.IsExisted(startupScript) {
			startupHistoryBin, _ := os.ReadFile(startupScript)
			startupHistoryContent := strings.TrimSpace(string(startupHistoryBin))
			if strings.HasPrefix(startupHistoryContent, "\\") {
				logger.Debugf("fixEfiFirmware: startupHistoryContent=`%s`", startupHistoryContent)
				return nil
			}
			_ = os.RemoveAll(startupScript)
		}

		logger.Debugf("fixEfiFirmware: create startup.nsh")
		// [root@localhost ~]# ll /boot/efi/EFI/
		// 总计 8
		// drwx------ 2 root root 4096  5月11日 23:34 BOOT
		_ = os.MkdirAll(uefiFallbackDir, 0o700)

		startupContent := ""
		// 保证优先使用shim*.efi
		for _, p := range []string{shimEfiPath, grubEfiPath} {
			if rel, err := filepath.Rel(filepath.Dir(efiRoot), p); err == nil &&
				rel != "." &&
				!strings.HasPrefix(rel, "..") {
				rel = strings.ReplaceAll(rel, "/", "\\")
				startupContent = rel
				if !strings.HasPrefix(startupContent, "\\") {
					startupContent = "\\" + startupContent
				}
				break
			}
		}
		if startupContent == "" {
			// TODO 警告无efi，启动后需要在uefi shell中手动选择efi固件
			return nil
		}
		logger.Debugf("fixEfiFirmware: Write `%s` to %s", startupContent, startupScript)
		if err := os.WriteFile(startupScript, []byte(startupContent), 0o700); err != nil {
			return errors.Wrapf(err, "write startup.nsh")
		}
		return nil
	}

	defaultEfiBackupPath := defaultEfiPath + fmt.Sprintf(".%d.backup.efi", time.Now().Unix())
	if err := os.Rename(defaultEfiPath, defaultEfiBackupPath); err != nil {
		return errors.Wrapf(err, "rename default efi")
	}

	// FIXME：测试shim*.efi优先的情况下，是否所有系统都能成功引导

	efiSource := ""

	// 优先 shim
	if shimEfiPath != "" {
		efiSource = shimEfiPath
	}

	// 没有 shim 时用 grub
	if efiSource == "" && grubEfiPath != "" {
		efiSource = grubEfiPath
	}

	if efiSource == "" {
		return errors.New("no available shim/grub efi")
	}

	cmdline := fmt.Sprintf("cp -f '%s' '%s'", efiSource, defaultEfiPath)
	if _, _, err := command.Execute(cmdline, command.WithDebug()); err != nil {
		return errors.Wrapf(err, "copy %s to %s", efiSource, defaultEfiPath)
	}

	return nil
}

func (fixer *linuxSystemFixer) fixPamLogin() error {
	// 实际测试环境中发现
	// [root@restoremachine home]# cat /etc/pam.d/login
	// #%PAM-1.0
	// auth [user_unknown=ignore success=ok ignore=ignore default=bad] pam_securetty.so
	// auth       include      system-auth
	// account    required     pam_nologin.so
	// account    include      system-auth
	// password   include      system-auth
	// # pam_selinux.so close should be the first session rule
	// session    required     pam_selinux.so close
	// session    required     pam_loginuid.so
	// session    optional     pam_console.so
	// # pam_selinux.so open should only be followed by sessions to be executed in the user context
	// session    required     pam_selinux.so open
	// session    required     pam_namespace.so
	// session    optional     pam_keyinit.so force revoke
	// session    include      system-auth
	// -session   optional     pam_ck_connector.so
	//
	// session required /lib/security/pam_limits.so
	// session required pam_limits.so
	// [root@restoremachine home]#
	// [root@restoremachine home]#
	// [root@restoremachine home]# ll /lib/security/pam_limits.so
	// ls: cannot access /lib/security/pam_limits.so: No such file or directory
	// [root@restoremachine home]# ll /lib64/security/pam_limits.so
	// -rwxr-xr-x. 1 root root 18592 Oct  7  2013 /lib64/security/pam_limits.so
	// 这里可以看到，用户配置了一条`session required /lib/security/pam_limits.so`，但是`/lib/security/pam_limits.so`是不存在的，
	// 只有`/lib64/security/pam_limits.so`存在，改成`/lib64/security/pam_limits.so`后就正常了。

	// 检查手段：
	// 遍历/etc/pam.d下所有的绝对路径so文件，若此文件不存在，那么就将其换成文件名即可

	if fixer.offsys.root == "" {
		return ErrorRootEnvNotMounted
	}

	pamDir := filepath.Join(fixer.offsys.root, "etc", "pam.d")

	entries, err := os.ReadDir(pamDir)
	if err != nil {
		return fmt.Errorf("read pam dir failed: %v", err)
	}

	// 匹配绝对路径 so
	//
	// 例如：
	// session required /lib/security/pam_limits.so
	// auth optional /usr/lib64/security/xxx.so
	//
	// 捕获：
	// 1: 前半部分
	// 2: 绝对路径
	// 3: 后半部分（参数）
	soRegexp := regexp.MustCompile(
		`^(\s*\S+\s+\S+\s+)(/[^ \t]+\.so)(.*)$`,
	)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filePath := filepath.Join(pamDir, entry.Name())
		//logger.Debugf("fixPamLogin: Prepare to repair `%s`", filePath)

		content, err := os.ReadFile(filePath)
		if err != nil {
			logger.Warnf(
				"fixPamLogin: read file failed: %s, err=%v",
				filePath,
				err,
			)
			continue
		}

		lines := strings.Split(string(content), "\n")

		modified := false

		for i, line := range lines {
			trimLine := strings.TrimSpace(line)

			// 跳过空行和注释
			if trimLine == "" || strings.HasPrefix(trimLine, "#") {
				continue
			}

			matches := soRegexp.FindStringSubmatch(line)
			if len(matches) != 4 {
				continue
			}

			absSoPath := matches[2]

			// 离线系统路径
			offlineSoPath := filepath.Join(
				fixer.offsys.root,
				strings.TrimPrefix(absSoPath, "/"),
			)

			//offlineSoRootDir := parseRootDir(strings.TrimPrefix(offlineSoPath, fixer.offsys.root))
			////logger.Debugf("fixPamLogin: offlineSoRootDir=`%s`", offlineSoRootDir)

			if strings.Contains(offlineSoPath, "$") {
				logger.Debugf("fixPamLogin: ignore `%s`", offlineSoPath)
				continue
			}

			// 文件存在则不处理
			if _, err = os.Stat(offlineSoPath); err == nil {
				continue
			}

			// 文件不存在，替换为 basename
			soName := filepath.Base(absSoPath)

			newLine := matches[1] + soName + matches[3]

			logger.Infof(
				"fixPamLogin: fix pam module path: file=%s line=%d old=%q new=%q",
				filePath,
				i+1,
				line,
				newLine,
			)

			lines[i] = newLine
			modified = true
		}

		if modified {
			newContent := strings.Join(lines, "\n")

			if err := os.WriteFile(
				filePath,
				[]byte(newContent),
				0644,
			); err != nil {
				return fmt.Errorf(
					"write pam file failed: %s, err=%v",
					filePath,
					err,
				)
			}
		}
	}

	return nil
}

func (fixer *linuxSystemFixer) disableSeLinux() error {
	logger.Debugf("disableSeLinux: ++")
	defer logger.Debugf("disableSeLinux: --")

	// 1. 修改 /etc/selinux/config
	configPath := filepath.Join(rootDir, "etc", "selinux", "config")

	if _, err := os.Stat(configPath); err == nil {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return errors.Wrapf(err, "read selinux config failed")
		}

		content := string(data)

		// 替换 SELINUX=
		lines := strings.Split(content, "\n")
		found := false

		for i, line := range lines {
			trimLine := strings.TrimSpace(line)

			// 跳过注释
			if strings.HasPrefix(trimLine, "#") {
				continue
			}

			if strings.HasPrefix(trimLine, "SELINUX=") {
				lines[i] = "SELINUX=disabled"
				found = true
			}
		}

		if !found {
			lines = append(lines, "SELINUX=disabled")
		}

		logger.Debugf("disableSeLinux: `%s` [before]:"+
			"\n----------------------------------------\n%s\n----------------------------------------\n",
			configPath, string(data))

		newContent := strings.Join(lines, "\n")

		if err := os.WriteFile(configPath, []byte(newContent), 0644); err != nil {
			return fmt.Errorf("write selinux config failed: %v", err)
		}

		logger.Debugf("disableSeLinux: `%s` [modified]:"+
			"\n----------------------------------------\n%s\n----------------------------------------\n",
			configPath, string(data))
	}

	// 2. 清理 grub cmdline 中的 selinux 参数

	removeKernelArgs := func(content string) string {
		reList := []*regexp.Regexp{
			regexp.MustCompile(`\s+selinux=\S+`),
			regexp.MustCompile(`\s+enforcing=\S+`),
		}

		for _, re := range reList {
			content = re.ReplaceAllString(content, "")
		}

		return content
	}

	if _, err := os.Stat(fixer.offsys.grubCfg); err != nil {
		return err
	}

	data, err := os.ReadFile(fixer.offsys.grubCfg)
	if err != nil {
		logger.Warnf("disableSeLinux: read grub file failed: %s err=%v",
			fixer.offsys.grubCfg, err)
		return nil
	}

	logger.Debugf("disableSeLinux: `%s` [before]:"+
		"\n----------------------------------------\n%s\n----------------------------------------\n",
		fixer.offsys.grubCfg, string(data))

	newContent := removeKernelArgs(string(data))

	if err = os.WriteFile(fixer.offsys.grubCfg, []byte(newContent), 0644); err != nil {
		logger.Warnf("disableSeLinux: write grub file failed: %s err=%v",
			fixer.offsys.grubCfg, err)
		return nil
	}

	logger.Infof("disableSeLinux: `%s` [modified]:"+
		"\n----------------------------------------\n%s\n----------------------------------------\n",
		fixer.offsys.grubCfg, newContent)

	// 3. 创建 autorelabel 标记（迁移后更安全）
	autorelabelPath := filepath.Join(rootDir, ".autorelabel")

	if err = os.WriteFile(autorelabelPath, []byte{}, 0644); err != nil {
		logger.Warnf("disableSeLinux: create .autorelabel failed: %v", err)
	} else {
		logger.Infof("disableSeLinux: created %s", autorelabelPath)
	}

	return nil
}

func (fixer *linuxSystemFixer) disableMultipathModule() error {
	logger.Debugf("disableMultipathModule: ++")
	defer logger.Debugf("disableMultipathModule: --")

	// TODO

	return errors.New("disableMultipathModule: not implemented yet")
}

// fixGrub 修复Grub
func (fixer *linuxSystemFixer) fixGrub() error {
	logger.Debugf("fixGrub: ++")
	defer logger.Debugf("fixGrub: --")

	// 修复原则：
	// s1. root/resume设备名使用UUID形式
	// s2. 删除无效的resume
	// s3. 自动加rootdelay=15
	// s4. 修复 console 参数（xvc0/hvc0）, FIXME：后续实现
	// s5. 删除物理机 multipath/san 参数，FIXME：后续实现
	// s6. ......

	// 其他：
	// 1. https://blog.csdn.net/weixin_39833509/article/details/115633386

	rootUUID, _ := DetectUuidByBlkid(fixer.offsys.devRoot)
	resumeUUID := ""
	if len(fixer.offsys.devSwaps) == 1 {
		resumeUUID, _ = DetectUuidByBlkid(fixer.offsys.devSwaps[0])
	}

	if rootUUID == "" {
		logger.Warnf("fixGrub: uuid of %s is empty", fixer.offsys.devRoot)
		return nil
	}

	logger.Debugf("fixGrub: uuid of `%s` is `%s`", fixer.offsys.devRoot, rootUUID)
	logger.Debugf("fixGrub: uuid of SWAP is `%s`", resumeUUID)

	uuidCandFiles := []string{
		filepath.Join(fixer.offsys.root, "etc", "sysconfig", "bootloader"),
		fixer.offsys.grubCfg,
	}
	for _, f := range uuidCandFiles {
		if err := fixer.fixOneGrub(f, rootUUID, resumeUUID); err != nil {
			return err
		}
	}

	return nil
}

func (fixer *linuxSystemFixer) fixOneGrub(
	file string,
	rootUUID string,
	resumeUUID string,
) error {
	logger.Debugf("fixOneGrub: ++")
	defer logger.Debugf("fixOneGrub: --")

	if rootUUID == "" {
		return nil
	}

	if !fixer.offsys.udevSupportUuid {
		return nil
	}

	if extend.IsDir(file) || !extend.IsExisted(file) {
		return nil
	}

	logger.Debugf("fixOneGrub: Prepare to repair `%s`", file)

	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}

	before := string(data)

	logger.Debugf(
		"fixOneGrub: `%s` [before]:"+
			"\n----------------------------------------\n%s\n----------------------------------------\n",
		file,
		before,
	)

	content := before

	// root=
	rootRe := regexp.MustCompile(`(^|\s+)root=\S+`)
	content = rootRe.ReplaceAllString(
		content,
		`${1}root=UUID=`+rootUUID,
	)

	// resume=
	if resumeUUID != "" {
		//	resumeRe := regexp.MustCompile(`(^|\s+)resume=\S+`)
		resumeRe := regexp.MustCompile(
			`resume=\S+`,
		)

		content = resumeRe.ReplaceAllString(
			content,
			"resume=UUID="+resumeUUID,
		)
	}

	// rootdelay=10
	rootDelayRe := regexp.MustCompile(`(^|\s+)rootdelay=\d+`)
	if rootDelayRe.MatchString(content) {
		content = rootDelayRe.ReplaceAllString(
			content,
			`${1}rootdelay=10`,
		)
	} else {
		// 只加到 kernel/linux 行
		lineRe := regexp.MustCompile(
			`(?m)^(\s*(kernel|linux|linux16|linuxefi)\s+.*)$`,
		)

		content = lineRe.ReplaceAllString(
			content,
			`${1} rootdelay=10`,
		)
	}

	// rootwait 避免重复
	rootWaitRe := regexp.MustCompile(`(^|\s+)rootwait(\s|$)`)
	if !rootWaitRe.MatchString(content) {
		lineRe := regexp.MustCompile(
			`(?m)^(\s*(kernel|linux|linux16|linuxefi)\s+.*)$`,
		)

		content = lineRe.ReplaceAllString(
			content,
			`${1} rootwait`,
		)
	}

	// 去掉 quiet 启动参数（保留原缩进和格式）
	quietRe := regexp.MustCompile(`\s+quiet(\s|$)`)

	lineRe := regexp.MustCompile(
		`(?m)^(\s*(kernel|linux|linux16|linuxefi)\s+.*)$`,
	)

	content = lineRe.ReplaceAllStringFunc(
		content,
		func(line string) string {
			// 删除 quiet
			line = quietRe.ReplaceAllString(line, "$1")

			// 清理 quiet 删除后可能出现的尾部空格
			line = strings.TrimRight(line, " \t")

			return line
		},
	)

	if content == before {
		logger.Debugf("fixOneGrub: `%s` no change", file)
		return nil
	}

	logger.Debugf(
		"fixOneGrub: `%s` [after]:"+
			"\n----------------------------------------\n%s\n----------------------------------------\n",
		file,
		content,
	)

	if err = os.WriteFile(file, []byte(content), 0644); err != nil {
		return err
	}

	logger.Infof("fixOneGrub: repaired `%s`", file)

	return nil
}

func (fixer *linuxSystemFixer) initrdAddModule(k kernel, modules ...string) error {
	logger.Debugf("initrdAddModule: ++")
	defer logger.Debugf("initrdAddModule: --")

	logger.Debugf("initrdAddModule: kernel:\n%s\nmodules:\n%v\n", extend.Pretty(k), modules)

	if fixer.offsys.root == "" {
		return ErrorRootEnvNotMounted
	}

	if !k.Bootable {
		return nil
	}

	switch fixer.offsys.initrdTl {
	case define.InitrdToolMkinitrd:
		return fixer.initrdAddModuleByMkinitrd(k, modules...)
	case define.InitrdToolDracut:
		if err := fixer.addModulesToDracutConf(modules...); err != nil {
			return err
		}
		return fixer.generateInitrdByDracut(k)
	case define.InitrdToolUpdateInitramfs:
		if err := fixer.addModulesToInitramfsConf(modules...); err != nil {
			return err
		}
		return fixer.generateInitrdByUpdateInitramfs(k)
	}

	return nil
}

func (fixer *linuxSystemFixer) addModulesToSysconfig(modules ...string) error {
	logger.Debugf("addModulesToSysconfig: ++")
	defer logger.Debugf("addModulesToSysconfig: --")

	cfgFile := filepath.Join(
		fixer.offsys.root,
		"etc/sysconfig/kernel",
	)

	content, err := os.ReadFile(cfgFile)
	if err != nil {
		//if os.IsNotExist(err) {
		//	return nil
		//}
		return errors.Wrapf(
			err,
			"read `%s` failed",
			cfgFile,
		)
	}

	s := string(content)

	re := regexp.MustCompile(
		`(?m)^(\s*INITRD_MODULES\s*=\s*")([^"]*)(".*)$`,
	)

	found := false

	s = re.ReplaceAllStringFunc(s, func(line string) string {
		found = true

		m := re.FindStringSubmatch(line)
		if len(m) != 4 {
			return line
		}

		// 已有模块
		existSet := map[string]struct{}{}
		existMods := strings.Fields(m[2])

		for _, mod := range existMods {
			mod = strings.TrimSpace(mod)
			if mod == "" {
				continue
			}
			existSet[mod] = struct{}{}
		}

		// 追加且去重
		for _, mod := range modules {
			mod = strings.TrimSpace(mod)
			if mod == "" {
				continue
			}

			if _, ok := existSet[mod]; ok {
				continue
			}

			existMods = append(existMods, mod)
			existSet[mod] = struct{}{}
		}

		return m[1] +
			strings.Join(existMods, " ") +
			m[3]
	})

	// 没有 INITRD_MODULES 则追加
	if !found {
		modSet := map[string]struct{}{}
		finalMods := make([]string, 0, len(modules))

		for _, mod := range modules {
			mod = strings.TrimSpace(mod)
			if mod == "" {
				continue
			}

			if _, ok := modSet[mod]; ok {
				continue
			}

			modSet[mod] = struct{}{}
			finalMods = append(finalMods, mod)
		}

		s += fmt.Sprintf(
			`\nINITRD_MODULES="%s"`+"\n",
			strings.Join(finalMods, " "),
		)
	}

	err = os.WriteFile(cfgFile, []byte(s), 0644)
	if err != nil {
		return errors.Wrapf(
			err,
			"write `%s` failed",
			cfgFile,
		)
	}

	return nil
}

func (fixer *linuxSystemFixer) initrdAddModuleByMkinitrd(
	k kernel,
	modules ...string,
) error {
	logger.Debugf("initrdAddModuleByMkinitrd: ++")
	defer logger.Debugf("initrdAddModuleByMkinitrd: --")

	if len(modules) == 0 {
		return nil
	}

	majVer := 0
	if fixer.offsys.initrdTlVer != "" {
		verItems := strings.Split(
			strings.TrimPrefix(
				fixer.offsys.initrdTlVer,
				define.InitrdToolMkinitrd+"-",
			),
			".",
		)

		v, _ := strconv.Atoi(verItems[0])
		majVer = v
	}

	// mkinitrd <= 2:
	// 修改 /etc/sysconfig/kernel 的 INITRD_MODULES
	if majVer <= 2 {
		err := fixer.addModulesToSysconfig(modules...)
		if err != nil {
			return err
		}

		// 重建 initrd
		cmdline := `mkinitrd`
		_, _, err = fixer.executeWithChroot(cmdline)
		if err != nil {
			return errors.Wrap(err, "execute mkinitrd failed")
		}

		return nil
	}

	// mkinitrd > 2:
	// 使用 --preload
	preloads := make([]string, 0, len(modules))
	for _, mod := range modules {
		mod = strings.TrimSpace(mod)
		if mod == "" {
			continue
		}

		preloads = append(
			preloads,
			fmt.Sprintf("--preload=%s", mod),
		)
	}

	cmdline := fmt.Sprintf(
		`mkinitrd %s -v -f "/boot/%s" "%s"`,
		strings.Join(preloads, " "),
		k.Initrd,
		k.Name,
	)

	_, _, err := fixer.executeWithChroot(cmdline)
	if err != nil {
		return errors.Wrap(err, "execute mkinitrd failed")
	}

	return nil
}

func (fixer *linuxSystemFixer) addModulesToInitramfsConf(modules ...string) error {
	logger.Debugf("addModulesToInitramfsConf: ++")
	defer logger.Debugf("addModulesToInitramfsConf: --")

	if len(modules) == 0 {
		return nil
	}

	// /etc/initramfs-tools/modules
	modulesFile := filepath.Join(
		fixer.offsys.root,
		"etc/initramfs-tools/modules",
	)

	// 已有模块
	existSet := map[string]struct{}{}
	lines := make([]string, 0)

	if bs, err := os.ReadFile(modulesFile); err == nil {
		for _, line := range strings.Split(string(bs), "\n") {
			line = strings.TrimSpace(line)

			// 保留注释和空行
			if line == "" || strings.HasPrefix(line, "#") {
				lines = append(lines, line)
				continue
			}

			mod := strings.Fields(line)[0]
			existSet[mod] = struct{}{}

			lines = append(lines, line)
		}
	}

	// 追加模块（去重）
	added := false

	for _, mod := range modules {
		mod = strings.TrimSpace(mod)
		if mod == "" {
			continue
		}

		if _, ok := existSet[mod]; ok {
			continue
		}

		lines = append(lines, mod)
		existSet[mod] = struct{}{}
		added = true

		logger.Debugf(
			"initrdAddModuleByUpdateInitramfs: add module `%s`",
			mod,
		)
	}

	// 写回
	if added {
		content := strings.Join(lines, "\n")
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}

		err := os.WriteFile(
			modulesFile,
			[]byte(content),
			0644,
		)
		if err != nil {
			return errors.Wrapf(
				err,
				"write `%s` failed",
				modulesFile,
			)
		}
	}

	return nil
}

func (fixer *linuxSystemFixer) generateInitrdByUpdateInitramfs(
	k kernel,
) error {
	logger.Debugf("generateInitrdByUpdateInitramfs: ++")
	defer logger.Debugf("generateInitrdByUpdateInitramfs: --")

	// 重建指定 kernel 的 initramfs
	cmdline := fmt.Sprintf(
		`update-initramfs -u -k %s`,
		k.Name,
	)

	logger.Debugf(
		"generateInitrdByUpdateInitramfs: cmd=`%s`",
		cmdline,
	)

	_, _, err := fixer.executeWithChroot(cmdline)
	if err != nil {
		return errors.Wrap(
			err,
			"execute update-initramfs failed",
		)
	}

	return nil
}

func (fixer *linuxSystemFixer) addModulesToDracutConf(modules ...string) error {
	logger.Debugf("addModulesToDracutConf: ++")
	defer logger.Debugf("addModulesToDracutConf: --")

	if len(modules) == 0 {
		return nil
	}

	confDir := filepath.Join(
		fixer.offsys.root,
		"etc/dracut.conf.d",
	)

	confFile := filepath.Join(
		confDir,
		"99-restore.conf",
	)

	if err := os.MkdirAll(confDir, 0755); err != nil {
		return errors.Wrapf(
			err,
			"mkdir `%s` failed",
			confDir,
		)
	}

	// 已存在模块
	existSet := map[string]struct{}{}

	// 保留原配置
	lines := make([]string, 0)

	if bs, err := os.ReadFile(confFile); err == nil {
		content := string(bs)

		// 解析已有 add_drivers
		re := regexp.MustCompile(
			`(?m)add_drivers\+\s*=\s*"([^"]*)"`,
		)

		matches := re.FindAllStringSubmatch(content, -1)

		for _, m := range matches {
			if len(m) < 2 {
				continue
			}

			for _, mod := range strings.Fields(m[1]) {
				mod = strings.TrimSpace(mod)
				if mod == "" {
					continue
				}

				existSet[mod] = struct{}{}
			}
		}

		lines = strings.Split(content, "\n")
	}

	added := false

	// 追加模块
	for _, mod := range modules {
		mod = strings.TrimSpace(mod)
		if mod == "" {
			continue
		}

		if _, ok := existSet[mod]; ok {
			continue
		}

		existSet[mod] = struct{}{}
		added = true

		logger.Debugf(
			"addModulesToDracutConf: add module `%s`",
			mod,
		)
	}

	// 如果没有新增，不必改配置
	if added || len(lines) == 0 {
		finalMods := make([]string, 0, len(existSet))

		for mod := range existSet {
			finalMods = append(finalMods, mod)
		}

		sort.Strings(finalMods)

		newLine := fmt.Sprintf(
			`add_drivers+=" %s "`,
			strings.Join(finalMods, " "),
		)

		replaced := false

		re := regexp.MustCompile(
			`(?m)^.*add_drivers\+\s*=.*$`,
		)

		for i, line := range lines {
			if re.MatchString(line) {
				lines[i] = newLine
				replaced = true
			}
		}

		if !replaced {
			lines = append(lines, newLine)
		}

		content := strings.Join(lines, "\n")
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}

		err := os.WriteFile(
			confFile,
			[]byte(content),
			0644,
		)
		if err != nil {
			return errors.Wrapf(
				err,
				"write `%s` failed",
				confFile,
			)
		}
	}

	return nil
}

func (fixer *linuxSystemFixer) generateInitrdByDracut(
	k kernel,
) error {
	logger.Debugf("generateInitrdByDracut: ++")
	defer logger.Debugf("generateInitrdByDracut: --")

	// 重建 initramfs
	cmdline := fmt.Sprintf(
		`dracut -v -f /boot/%s %s`,
		k.Initrd,
		k.Name,
	)

	logger.Debugf(
		"generateInitrdByDracut: cmd=`%s`",
		cmdline,
	)

	_, _, err := fixer.executeWithChroot(cmdline)
	if err != nil {
		return errors.Wrap(
			err,
			"execute dracut failed",
		)
	}

	return nil
}

func (fixer *linuxSystemFixer) kernelContainsModule(k kernel, module string) (bool, error) {
	foundFromLib := false
	libDir := filepath.Join(fixer.offsys.root, "lib/modules", k.Name)
	ew := filepath.WalkDir(libDir, func(path_ string, d_ fs.DirEntry, err_ error) error {
		if strings.HasSuffix(path_, ".ko") ||
			strings.HasSuffix(path_, ".ko.xz") ||
			strings.HasSuffix(path_, ".ko.zst") {
			fn_ := filepath.Base(path_)
			mn_ := moduleName(fn_)
			if mn_ == module {
				foundFromLib = true
				return filepath.SkipAll
			}
		}
		return nil
	})
	if ew != nil {
		return false, ew
	}
	return foundFromLib, nil
}

func (fixer *linuxSystemFixer) closeCryptoLUKS() {
	_ = fixer.deactiveLvm()
	for _, luk := range fixer.offsys.luksDeviceList {
		_, _, _ = command.Execute(
			"kpartx -d "+luk.Mapper,
			command.WithDebug())
		_, _, _ = command.Execute(
			"cryptsetup close "+luk.Mapper,
			command.WithDebug())
	}
	_ = fixer.deactiveLvm()
}

func (fixer *linuxSystemFixer) fsckAllFs() {
	logger.Debugf("fsckAllFs: ++")
	defer logger.Debugf("fsckAllFs: --")

	for _, d := range fixer.offsys.fsList {
		fsckCmd, ok := DetectFSRepairCmdline(d.Device)
		if !ok {
			continue
		}
		_, _, _ = command.Execute(fsckCmd, command.WithDebug())
	}
}

func (fixer *linuxSystemFixer) syncFs() {
	_, _, _ = command.Execute("sync;echo 3 > /proc/sys/vm/drop_caches", command.WithDebug())
}

func (fixer *linuxSystemFixer) batchInjectPackages(
	pkgDir string,
	installCmd string,
) error {
	logger.Debugf("batchInjectPackages: ++")
	defer logger.Debugf("batchInjectPackages: --")

	if fixer.offsys.root == "" {
		return ErrorRootEnvNotMounted
	}

	tmpDir, err := os.MkdirTemp(
		fixer.offsys.root,
		"x2x.packages.*",
	)
	if err != nil {
		return err
	}

	defer func(path string) {
		_ = os.RemoveAll(path)
	}(tmpDir)

	tmpDirChroot := "/" + filepath.Base(tmpDir)

	cpCmdline := fmt.Sprintf(
		"cp -r %s/* %s/",
		pkgDir,
		tmpDir,
	)

	if _, _, err = command.Execute(
		cpCmdline,
		command.WithDebug(),
	); err != nil {
		return errors.Wrapf(
			err,
			"copy %s/* to %s",
			pkgDir,
			tmpDir,
		)
	}

	logger.Debugf(
		"injecting packages from %s ...",
		pkgDir,
	)

	cmdline := fmt.Sprintf(
		"cd %s && %s",
		tmpDirChroot,
		installCmd,
	)

	_, _, err = fixer.executeWithChroot(cmdline)

	return err
}

func (fixer *linuxSystemFixer) batchInjectPackagesByZypper(
	pkgDir string,
) error {
	return fixer.batchInjectPackages(
		pkgDir,
		"zypper --non-interactive install *.rpm",
	)
}

func (fixer *linuxSystemFixer) batchInjectPackagesByRpm(
	pkgDir string,
) error {
	return fixer.batchInjectPackages(
		pkgDir,
		"rpm -Uvh *.rpm",
	)
}

func (fixer *linuxSystemFixer) batchInjectPackagesByDnf(
	pkgDir string,
) error {
	return fixer.batchInjectPackages(
		pkgDir,
		"dnf install -y *.rpm --disablerepo='*'",
	)
}

func (fixer *linuxSystemFixer) batchInjectPackagesByYum(
	pkgDir string,
) error {
	return fixer.batchInjectPackages(
		pkgDir,
		"yum localinstall -y *.rpm",
	)
}

func (fixer *linuxSystemFixer) batchInjectPackagesByDpkg(
	pkgDir string,
) error {
	return fixer.batchInjectPackages(
		pkgDir,
		"dpkg -i *.deb",
	)
}

func (fixer *linuxSystemFixer) batchInjectPackagesByApt(
	pkgDir string,
) error {
	return fixer.batchInjectPackages(
		pkgDir,
		"apt install -y ./*.deb",
	)
}

func (fixer *linuxSystemFixer) batchInjectPackage(
	pkgDir string,
) error {
	switch fixer.offsys.pkgMgrType {
	case PackageManagerRPM:
		return fixer.batchInjectPackagesByRpm(pkgDir)
	case PackageManagerDEB:
		return fixer.batchInjectPackagesByDpkg(pkgDir)
	default:
		return errors.Errorf("unsupported package manager type: %s", fixer.offsys.pkgMgrType)
	}
}
