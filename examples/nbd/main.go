//go:build linux

// nbd 示例演示 disk/nbd 资源管理器的完整生命周期：
//
//	初始化 -> 申请设备 -> 连接后端 -> 执行操作 -> 释放
//
// 用法：
//
//	sudo ./nbd [设备数] [分区数] [镜像文件]
package main

import (
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/kisun-bit/drpkg/disk/nbd"
)

func main() {
	maxDev, maxPart, backend := 8, 8, ""

	if len(os.Args) > 1 {
		maxDev = atoi(os.Args[1])
	}
	if len(os.Args) > 2 {
		maxPart = atoi(os.Args[2])
	}
	if len(os.Args) > 3 {
		backend = os.Args[3]
	}

	m, err := nbd.Initialize(&nbd.Options{
		MaxDevCount:  maxDev,
		MaxPartCount: maxPart,
	})
	if err != nil {
		log.Fatalf("initialize: %v", err)
	}
	defer m.Close()

	total, err := m.MaxDeviceCount()
	if err != nil {
		log.Fatalf("max device count: %v", err)
	}
	fmt.Printf("nbd devices available: %d (max_part=%d)\n", total, m.MaxPartCount())

	ids, err := m.Alloc(2)
	if err != nil {
		log.Fatalf("alloc: %v", err)
	}
	fmt.Printf("allocated devices: %v\n", ids)

	if backend != "" {
		if err = m.Connect(ids[0], &nbd.ConnectOptions{Backend: backend}); err != nil {
			log.Fatalf("connect nbd%d: %v", ids[0], err)
		}
		fmt.Printf("nbd%d connected to %s -> %s\n", ids[0], backend, nbd.DevicePath(ids[0]))
	}

	// 对申请到的设备执行自定义操作。
	err = m.DoAll(ids, func(index int) error {
		st, e := m.Status(index)
		if e != nil {
			return e
		}
		fmt.Printf("nbd%d state=%s backend=%q managed=%v\n",
			st[0].Index, st[0].State, st[0].Backend, st[0].Managed)
		return nil
	})
	if err != nil {
		log.Fatalf("do: %v", err)
	}

	if err = m.ReleaseAll(); err != nil {
		log.Fatalf("release all: %v", err)
	}
	fmt.Println("all devices released")
}

func atoi(s string) int {
	v, err := strconv.Atoi(s)
	if err != nil {
		log.Fatalf("invalid number %q: %v", s, err)
	}
	return v
}
