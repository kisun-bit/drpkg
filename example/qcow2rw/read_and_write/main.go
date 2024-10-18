package main

import (
	"context"
	virtdisk "github.com/kisun-bit/drpkg/disk/image/qcow2"
	"github.com/kisun-bit/drpkg/util/logger"
	"sync"
)

func main() {
	virtdisk.QemuEnvSetup("", "/home/kisun/qemu-7.2.0/build/qemu-iow")

	q, err := virtdisk.NewQemuIOManager(context.Background(),
		"/home/kisun/qemu-7.2.0/full_1.qcow2",
		"qcow2",
		virtdisk.Direct, virtdisk.EnableRWSerialAccess())
	if err != nil {
		logger.Fatalf(">NewQemuIOManager(...): %v", err)
	}
	if err = q.Open(); err != nil {
		logger.Fatalf("Open(...), image=%s, err=%v", q.Image, err)
	}
	defer func() {
		if err == nil {
			err = q.Error()
		}
	}()
	defer func() {
		_ = q.Close()
	}()

	var wg sync.WaitGroup
	addrWithLen := map[int64]int{
		0:        4096,
		1048576:  4096,
		55574528: 4096,
		1347584:  4096,
		9117696:  4096,
		1351680:  4096,
		10485760: 4096,
		10424320: 20480,
		10444800: 16384,
		10219520: 20480,
		34603008: 69632,
		47185920: 69632,
	}

	for addr, len_ := range addrWithLen {
		wg.Add(1)
		go func(_addr int64, _len int) {
			defer wg.Done()
			_buf := make([]byte, _len)
			_, er := q.ReadAt(_buf, _addr)
			if er != nil {
				logger.Fatalf("ReadAt(off=%v): %v", _addr, er)
			}
			logger.Infof("read %v bytes from %v ok", _len, _addr)
		}(addr, len_)

		wg.Add(1)
		go func(_addr int64, _len int) {
			defer wg.Done()
			_buf := make([]byte, _len)
			_, er := q.WriteAt(_buf, _addr)
			if er != nil {
				logger.Fatalf("WriteAt(off=%v): %v", _addr, er)
			}
			logger.Infof("write %v bytes at %v ok", _len, _addr)
		}(addr, len_)
	}

	wg.Wait()
	logger.Infof("finished")
}
