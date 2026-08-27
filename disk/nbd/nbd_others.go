//go:build !linux

package nbd

import (
	"strconv"
	"time"
)

// 本文件是 nbd 资源管理器在非 Linux 平台上的桩实现：
// 所有操作均返回 [ErrUnsupportedPlatform]，仅保证跨平台编译通过。

const DevicePathPrefix = "/dev/nbd"

const (
	KernelModule = "nbd"

	DevicePollInterval = 50 * time.Millisecond
	DeviceReadyTimeout = 10 * time.Second

	ConnectDelay          = 100 * time.Millisecond
	DisconnectSettleDelay = 300 * time.Millisecond

	StackRemoveTimeout      = 15 * time.Second
	PartitionsSettleTimeout = DeviceReadyTimeout
)

// Options 是资源管理器的初始化参数（非 Linux 平台不生效）。
type Options struct {
	MaxDevCount   int
	MaxPartCount  int
	NbdClientPath string
}

// DeviceState 描述 nbd 设备状态（非 Linux 平台恒为 DeviceFree）。
type DeviceState int

const (
	DeviceFree DeviceState = iota
	DeviceAllocated
	DeviceConnected
)

func (s DeviceState) String() string { return "free" }

// DeviceStatus 是设备状态快照（非 Linux 平台为空结构）。
type DeviceStatus struct {
	Index   int
	State   DeviceState
	Backend string
	Managed bool
	Pids    []int
}

// Manager 是 nbd 资源管理器（非 Linux 平台不可用）。
type Manager struct{}

// ConnectOptions 描述一次设备连接的参数（非 Linux 平台不生效）。
type ConnectOptions struct {
	Backend   string
	Remote    string
	Format    string
	ReadOnly  bool
	Cache     string
	Discard   string
	ExtraArgs []string
}

// StackCleanupResult 汇总一次设备堆栈清理的实际动作（非 Linux 平台为空结构）。
type StackCleanupResult struct {
	RemovedDM  []string
	RemovedMD  []string
	SwappedOff []string
	Partitions []string
}

// DevicePath 返回设备节点路径。
func DevicePath(index int) string { return DevicePathPrefix + strconv.Itoa(index) }

// PartitionPath 返回分区节点路径。
func PartitionPath(index, part int) string { return DevicePath(index) + "p" + strconv.Itoa(part) }

// Initialize 在非 Linux 平台上总是返回 [ErrUnsupportedPlatform]。
func Initialize(_ *Options) (*Manager, error) {
	return nil, ErrUnsupportedPlatform
}

func (m *Manager) MaxDeviceCount() (int, error)      { return 0, ErrUnsupportedPlatform }
func (m *Manager) MaxPartCount() int                 { return 0 }
func (m *Manager) Alloc(_ int) ([]int, error)        { return nil, ErrUnsupportedPlatform }
func (m *Manager) Do(_ int, _ func(int) error) error { return ErrUnsupportedPlatform }
func (m *Manager) DoAll(_ []int, _ func(int) error) error {
	return ErrUnsupportedPlatform
}
func (m *Manager) Connect(_ int, _ *ConnectOptions) error { return ErrUnsupportedPlatform }
func (m *Manager) CleanupDeviceStack(_ int) (*StackCleanupResult, error) {
	return nil, ErrUnsupportedPlatform
}
func (m *Manager) Disconnect(_ int) error { return ErrUnsupportedPlatform }
func (m *Manager) Release(_ int) error    { return ErrUnsupportedPlatform }
func (m *Manager) ReleaseAll() error      { return ErrUnsupportedPlatform }
func (m *Manager) Status(_ ...int) ([]*DeviceStatus, error) {
	return nil, ErrUnsupportedPlatform
}
func (m *Manager) Close() error { return ErrUnsupportedPlatform }
