package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/kisun-bit/drpkg/disk/scan"
)

var _handleMutex sync.Mutex

func _handleBlock(data []byte) {
	_handleMutex.Lock()
	defer _handleMutex.Unlock()

	time.Sleep(1 * time.Millisecond)
}

func DemoNormal(disk string) {
	start := time.Now()
	defer func() {
		end := time.Now()
		duration := end.Sub(start)
		fmt.Printf("[NORMAL] read %s in %s\n", disk, duration)
	}()

	fd, err := os.Open(disk)
	if err != nil {
		log.Fatalln("DemoNormal: Open: ", err)
	}
	defer fd.Close()

	buf := make([]byte, 1<<20)
	for {
		nr, er := fd.Read(buf)
		if er != nil {
			if er != io.EOF {
				log.Fatalln("DemoNormal: Read: ", er)
			} else {
				break
			}
		}
		_handleBlock(buf[:nr])
	}
}

func DemoRegionScan(disk string) {
	start := time.Now()
	defer func() {
		end := time.Now()
		duration := end.Sub(start)
		fmt.Printf("[SCAN  ] read %s in %s\n", disk, duration)
	}()

	s := scan.Scanner{
		Path:        disk,
		BlockSize:   1 << 20,
		Concurrency: 1,
		OnData: func(r scan.Range) {
			_handleBlock(r.Data)
		},
	}

	if err := s.Run(); err != nil {
		log.Fatalln("DemoRegionScan: Run: ", err)
	}
}

func main() {
	DemoNormal(os.Args[1])
	//DemoRegionScan(os.Args[1])
}
