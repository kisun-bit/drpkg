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

// copyImageToRawDeviceSingleThread (单线程版本)将一个镜像文件拷贝至设备文件中.
// 如您可以将sda2.qcow2拷贝到/dev/sdb2中.
func copyImageToRawDeviceSingleThread(originImage, originImageType, destRawDevice string, mode virtdisk.QemuIOCacheMode, blockSize int) (err error) {
	// 初始化qemu-io操作环境.
	q, err := virtdisk.NewQemuIOManager(context.TODO(), originImage, originImageType, mode, virtdisk.EnableRWSerialAccess())
	if err != nil {
		return errors.Wrapf(err, "copyImageToRawDeviceSingleThread->NewQemuIOManager(...)")
	}
	// 打开目标镜像文件.
	if err = q.Open(); err != nil {
		return errors.Wrapf(err, "copyImageToRawDeviceSingleThread->Open(...), image=%s", q.Image)
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

	// 打开目标设备文件.
	fp, err := os.OpenFile(destRawDevice, os.O_WRONLY, 0)
	if err != nil {
		return errors.Wrapf(err,
			"copyImageToRawDeviceSingleThread->OpenFile(...), dest-device=%s",
			destRawDevice)
	}
	defer func() {
		_ = fp.Close()
	}()

	start := time.Now()

	// 单线程拷贝
	buf := make([]byte, blockSize)
	offset := int64(0)
	for {
		nr, er := q.ReadAt(buf, offset)
		if er != nil && er != io.EOF {
			return errors.Wrapf(er,
				"copyImageToRawDeviceSingleThread->ReadAt(...), read origin-image(%s) error",
				destRawDevice)
		}
		if nr == 0 {
			break
		}
		_, ew := fp.WriteAt(buf[:nr], offset)
		if ew != nil {
			return errors.Wrapf(ew,
				"copyRawDeviceToImageSingleThread->WriteAt(...), failed to write data to %s at offset(%v)",
				originImage, offset)
		}
		offset += int64(nr)
	}
	end := time.Now()

	bytesPerSec := uint64(float64(offset) * 1000 / float64(end.Sub(start).Milliseconds()))
	logger.Debugf("speed: %v/s", humanize.IBytes(bytesPerSec))
	logger.Debugf("read: %v", humanize.IBytes(uint64(offset)))

	return err
}

func main() {
	virtdisk.QemuEnvSetup("", "/home/kisun/qemu-7.2.0/build/qemu-iow")
	err := copyImageToRawDeviceSingleThread(
		"/home/kisun/qemu-7.2.0/full.qcow2",
		"qcow2",
		"/dev/nbd10",
		virtdisk.WritebackWithAio,
		1<<20, // 1MiB
	)
	if err != nil {
		logger.Errorf("failed to call copyImageToRawDeviceSingleThread, err: %v", err)
		os.Exit(1)
	}
	os.Exit(0)
}
