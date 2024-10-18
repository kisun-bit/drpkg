package main

import (
	"context"
	virtdisk "github.com/kisun-bit/drpkg/disk/image/qcow2"
	"github.com/kisun-bit/drpkg/util/logger"
	"os"
)

func main() {
	//ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	//defer cancel()

	reader, err := virtdisk.NewImageEffectiveReader(context.Background(),
		"/home/kisun/qemu-7.2.0/full.qcow2",
		//"/vms/backup/ZK_qyrzmbj",
		"qcow2", virtdisk.WithReadCores(8))
	if err != nil {
		logger.Fatalf("NewImageEffectiveReader(): %v", err)
	}
	defer reader.Close()

	writer, err := os.OpenFile("/dev/nbd10", os.O_WRONLY, 0)
	if err != nil {
		logger.Fatalf("OpenFile: %v", err)
	}
	defer writer.Close()

	for block := range reader.Blocks() {
		_, ew := writer.WriteAt(block.Payload, block.Off)
		if ew != nil {
			logger.Fatalf("OpenFile: %v", err)
		}
		logger.Infof("written block: off=%v, len=%v", block.Off, len(block.Payload))
	}

	if err = reader.Error(); err != nil {
		logger.Fatalf("Error(): %v", err)
	}
}
