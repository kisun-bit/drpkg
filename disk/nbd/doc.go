// Package nbd 提供 Linux NBD (Network Block Device) 资源管理器。
//
// 管理器负责 nbd 内核模块的加载与设备资源的全生命周期管理：
//
//	m, err := nbd.Initialize(&nbd.Options{MaxDevCount: 16, MaxPartCount: 8})
//
//	ids, err := m.Alloc(3)            // 申请空闲设备，如 [1,2,3]
//
//	err = m.Connect(ids[0], &nbd.ConnectOptions{Backend: "/path/to/image.qcow2"})
//	err = m.Do(ids[0], func(index int) error { ... })
//
//	err = m.Release(ids[0])           // 释放单个设备
//	err = m.ReleaseAll()              // 释放全部设备
//
// 释放前会自动清理设备堆栈：当连接的设备之上存在分区、LVM 逻辑卷、
// dm-crypt 加密设备或 md/raid 阵列时，Disconnect（以及 Release/Close）
// 会先停用这些上层设备（含作为 swap 使用的分区），再断开连接并等待
// 分区节点被内核回收。若上层存在已挂载的文件系统，需先自行 umount。
//
// 注意：管理器仅支持 Linux 平台，其余平台的所有操作均返回
// ErrUnsupportedPlatform。加载内核模块、操作 /dev/nbdN 设备节点
// 通常需要 root 权限。
//
// 内核要求：Linux >= 4.9（依赖 /sys/block/nbdN/pid 属性判断设备占用）；
// 若使用 Alloc 自动扩容或 Connect/Disconnect，还需宿主机提供
// 可执行文件 qemu-nbd。
package nbd

import "github.com/pkg/errors"

var (
	// ErrUnsupportedPlatform 当前平台不支持 nbd 资源管理（仅支持 Linux）。
	ErrUnsupportedPlatform = errors.New("nbd resource manager is only supported on linux")

	// ErrNotInitialized 管理器尚未初始化（Initialize 未成功调用）。
	ErrNotInitialized = errors.New("nbd manager is not initialized")

	// ErrAlreadyInitialized 管理器已经初始化，不允许重复初始化。
	ErrAlreadyInitialized = errors.New("nbd manager is already initialized")

	// ErrNotAllocated 目标设备未由当前管理器分配。
	ErrNotAllocated = errors.New("nbd device is not allocated by this manager")

	// ErrAlreadyConnected 目标设备已处于连接状态。
	ErrAlreadyConnected = errors.New("nbd device is already connected")
)
