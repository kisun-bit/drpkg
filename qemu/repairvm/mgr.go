package repairvm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/pkg/errors"
)

// Create 创建一个修复虚拟机资源
func Create(opt *Option) (*Vm, error) {
	if opt == nil {
		return nil, errors.New("opt is nil")
	}

	if err := opt.Validate(); err != nil {
		return nil, err
	}

	vm := &Vm{
		opt:   opt,
		uuid_: uuid.New(),
	}

	vm.cacheDir = filepath.Join(
		os.TempDir(),
		vm.uuid_.String(),
	)

	if err := os.MkdirAll(vm.cacheDir, 0755); err != nil {
		return nil, err
	}

	ok := false

	defer func() {
		if !ok {
			_ = os.RemoveAll(vm.cacheDir)
		}
	}()

	if err := vm.prepare(); err != nil {
		return nil, err
	}

	if err := vm.buildCommand(); err != nil {
		return nil, err
	}

	ok = true

	logger.Debugf(
		"qemu command:\n%s",
		strings.Join(vm.cmdArgs, " "),
	)

	return vm, nil
}

func (vm *Vm) prepare() error {

	// virtio serial socket
	vm.sockFile = filepath.Join(
		vm.cacheDir,
		"repairvm.sock",
	)

	if err := vm.prepareBoot(); err != nil {
		return err
	}

	if err := vm.prepareSimulator(); err != nil {
		return err
	}

	return nil
}

func (vm *Vm) prepareBoot() error {

	path := vm.opt.VmBootDiskFile

	ok, err := isQCOW2(path)

	if err != nil {
		return err
	}

	if ok {

		overlay, err := createBootOverlay(
			context.Background(),
			path,
		)

		if err != nil {
			return err
		}

		vm.vmBootDisk = overlay

	} else {

		vm.vmBootImage = path

	}

	return nil
}

func (vm *Vm) prepareSimulator() error {

	arch := vm.opt.RecoveryParams.Source.Arch

	if vm.opt.RecoveryParams.OSType == "windows" {
		arch = "amd64"
	}

	if arch == "" {
		return errors.New(
			"unsupported architecture",
		)
	}

	sims, err := loadSimulatorsFromFile(
		vm.opt.SimulatorConfigFile,
	)

	if err != nil {
		return err
	}

	sim, ok := sims[arch]

	if !ok {
		return errors.Errorf(
			"simulator not found: %s",
			arch,
		)
	}

	vm.arch = arch
	vm.simulator = sim
	vm.cmdCaller = sim

	return nil
}

func (vm *Vm) buildCommand() error {

	vm.cmdArgs = append(
		vm.cmdArgs,

		fmt.Sprintf(
			"-name guest=repairvm.%s.%s.%s",
			vm.opt.RecoveryParams.OSType,
			vm.arch,
			vm.uuid_.String(),
		),

		"-machine q35",

		"-accel tcg,thread=multi",

		fmt.Sprintf(
			"-uuid %s",
			vm.uuid_,
		),

		"-m 2G",

		"-smp 4",
	)

	vm.addVirtioSerial()

	vm.addStorage()

	vm.addCDROM()

	return nil
}

func (vm *Vm) addVirtioSerial() {

	vm.cmdArgs = append(
		vm.cmdArgs,

		"-device virtio-serial",
	)

	vm.cmdArgs = append(
		vm.cmdArgs,

		fmt.Sprintf(
			"-chardev socket,id=agent,path=%s,server=on,wait=off",
			vm.sockFile,
		),
	)

	vm.cmdArgs = append(
		vm.cmdArgs,

		"-device virtserialport,chardev=agent,name=repair.agent",
	)

}

func (vm *Vm) addStorage() {

	vm.cmdArgs = append(
		vm.cmdArgs,

		"-device virtio-scsi-pci,id=scsi0",
	)

	index := 0

	add := func(
		path string,
		format string,
		boot bool,
	) {

		vm.cmdArgs = append(
			vm.cmdArgs,

			fmt.Sprintf(
				"-drive id=disk%d,if=none,file=%s,format=%s",
				index,
				path,
				format,
			),
		)

		arg := fmt.Sprintf(
			"-device scsi-hd,drive=disk%d,bus=scsi0.%d",
			index,
			index,
		)

		if boot {
			arg += ",bootindex=1"
		}

		vm.cmdArgs = append(
			vm.cmdArgs,
			arg,
		)

		index++
	}

	// boot disk

	if vm.vmBootDisk != "" {

		add(
			vm.vmBootDisk,
			"qcow2",
			true,
		)

	}

	// offline disks

	for _, d := range vm.opt.OfflineSystemDisks {

		format := "raw"

		if ok, _ := isQCOW2(d.Path); ok {
			format = "qcow2"
		}

		add(
			d.Path,
			format,
			false,
		)

	}

}

func (vm *Vm) addCDROM() {

	vm.cmdArgs = append(
		vm.cmdArgs,

		"-device ich9-ahci,id=sata",
	)

	// pe

	if vm.vmBootImage != "" {

		vm.cmdArgs = append(
			vm.cmdArgs,

			fmt.Sprintf(
				"-drive id=tmpos,if=none,media=cdrom,file=%s",
				vm.vmBootImage,
			),
		)

		vm.cmdArgs = append(
			vm.cmdArgs,

			"-device ide-cd,drive=tmpos,bus=sata.2",
		)

	}

	// driver iso

	if vm.opt.DriverDBImageFile != "" {

		vm.cmdArgs = append(
			vm.cmdArgs,

			fmt.Sprintf(
				"-drive id=driver,if=none,media=cdrom,file=%s",
				vm.opt.DriverDBImageFile,
			),
		)

		vm.cmdArgs = append(
			vm.cmdArgs,

			"-device ide-cd,drive=driver,bus=sata.3",
		)

	}

	bootOrderStr := "c"
	if vm.vmBootImage != "" {
		bootOrderStr = "d"
	}

	vm.cmdArgs = append(
		vm.cmdArgs,

		fmt.Sprintf(
			"-boot menu=off,order=%s",
			bootOrderStr,
		),
	)

}

func (vm *Vm) Repair() error {
	// 启动虚拟机，若意外退出，则报错

	// 向sock发送修复指令

	// 异步读取sock的修复日志和结果，若出现错误，则报错

	// TODO
	return errors.New("not implemented")
}

func (vm *Vm) Release() error {

	if vm == nil {
		return nil
	}

	if vm.cmd != nil && vm.cmd.Process != nil {

		// 优雅关闭
		if err := vm.cmd.Process.Signal(os.Interrupt); err != nil {

			// 强制结束
			if err := vm.cmd.Process.Kill(); err != nil {
				return err
			}
		}

		// 等待进程退出
		_, _ = vm.cmd.Process.Wait()
	}

	// 清理临时目录
	if vm.cacheDir != "" {

		if err := os.RemoveAll(vm.cacheDir); err != nil {
			return err
		}
	}

	return nil
}
