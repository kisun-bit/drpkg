package main

import (
	"context"
	"github.com/dustin/go-humanize"
	virtdisk "github.com/kisun-bit/drpkg/disk/image/qcow2"
	"github.com/kisun-bit/drpkg/util/logger"
	"github.com/pkg/errors"
	"io"
	"os"
	"time"
)

// copyRawDeviceToImageSingleThread (单线程版本)将一个设备文件拷贝至镜像文件中.
// 如您可以将/dev/sda2拷贝到sda2.qcow2中.
func copyRawDeviceToImageSingleThread(originRawDevice, destImage, destImageType string, mode virtdisk.QemuIOCacheMode, blockSize int) (err error) {
	// 初始化qemu-io操作环境.
	q, err := virtdisk.NewQemuIOManager(context.TODO(), destImage, destImageType, mode)
	if err != nil {
		return errors.Wrapf(err, "copyRawDeviceToImageSingleThread->NewQemuIOManager(...)")
	}
	// 打开目标镜像文件.
	if err = q.Open(); err != nil {
		return errors.Wrapf(err, "copyRawDeviceToImageSingleThread->Open(...), image=%s", q.Image)
	}
	defer func() {
		if err == nil {
			err = q.Error()
		}
	}()
	// 退出时关闭qemu-io操作环境
	defer func() {
		_ = q.Close()
	}()

	// 打开源设备文件.
	fp, err := os.Open(originRawDevice)
	if err != nil {
		return errors.Wrapf(err,
			"copyRawDeviceToImageSingleThread->Open(...), origin-device=%s",
			originRawDevice)
	}
	defer func() {
		_ = fp.Close()
	}()

	start := time.Now()

	// 单线程拷贝
	buf := make([]byte, blockSize)
	offset := int64(0)
	for {
		nr, er := fp.Read(buf)
		if er != nil && er != io.EOF {
			return errors.Wrapf(er,
				"copyRawDeviceToImageSingleThread->Read(...), read origin-device(%s) error",
				originRawDevice)
		}
		if nr == 0 {
			break
		}
		_, ew := q.WriteAt(buf[:nr], offset)
		if ew != nil {
			return errors.Wrapf(ew,
				"copyRawDeviceToImageSingleThread->WriteAt(...), failed to write data to %s at offset(%v)",
				destImage, offset)
		}
		offset += int64(nr)
	}
	end := time.Now()

	bytesPerSec := uint64(float64(offset) * 1000 / float64(end.Sub(start).Milliseconds()))
	logger.Debugf("speed: %v/s", humanize.IBytes(bytesPerSec))
	logger.Debugf("written: %v", humanize.IBytes(uint64(offset)))

	return err
}

func main() {
	virtdisk.QemuEnvSetup("", "/home/kisun/qemu-7.2.0/build/qemu-iow")
	err := copyRawDeviceToImageSingleThread(
		"/dev/sda2",
		"/home/kisun/qemu-7.2.0/full.qcow2",
		"qcow2",
		virtdisk.WritebackWithAio,
		1<<20, // 1MiB
	)
	if err != nil {
		logger.Errorf("failed to call copyRawDeviceToImageSingleThread, err: %v", err)
		os.Exit(1)
	}
	os.Exit(0)
}
