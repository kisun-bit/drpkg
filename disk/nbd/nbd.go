//go:build linux

package nbd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kisun-bit/drpkg/logger"
	"github.com/pkg/errors"
)

// DevicePathPrefix 是 nbd 块设备节点前缀：设备编号 i 的节点为
// /dev/nbd<i>，其分区节点为 /dev/nbd<i>p<j>。
const DevicePathPrefix = "/dev/nbd"

const (
	// KernelModule nbd 内核模块名。
	KernelModule = "nbd"

	sysBlockDir      = "/sys/block"
	sysClassBlockDir = "/sys/class/block"
	procModulesFile  = "/proc/modules"
	devDir           = "/dev"
	modprobeCaller   = "modprobe"
	nbdClientTool    = "qemu-nbd"

	// DevicePollInterval 是设备状态轮询间隔；
	// DeviceReadyTimeout 是连接/断开/等待设备节点等操作的默认上限。
	DevicePollInterval = 50 * time.Millisecond
	DeviceReadyTimeout = 10 * time.Second

	// ConnectDelay 是连接成功后等待内核完成设备注册的延迟；
	// DisconnectSettleDelay 是断开后等待 IO 收尾的延迟。
	ConnectDelay          = 100 * time.Millisecond
	DisconnectSettleDelay = 300 * time.Millisecond
)

// Options 是资源管理器的初始化参数。
type Options struct {
	// MaxDevCount 是允许管理的 nbd 设备数量上限（即 nbd0..nbd<N-1>）。
	// 必须 >= 1；若当前内核已加载设备数量不足，模块将以
	// nbds_max=<MaxDevCount> 重新加载。
	MaxDevCount int

	// MaxPartCount 是单个 nbd 设备允许的最大分区数（0..15）。
	// 模块加载后不可变更：模块已加载且分区数不一致时，
	// Initialize 返回错误而非静默重载模块。
	MaxPartCount int

	// NbdClientPath 指定 qemu-nbd 可执行文件路径；为空时在 PATH 中查找。
	// 仅 Connect/Disconnect 以及 Alloc 自动扩容场景需要。
	NbdClientPath string
}

// DeviceState 描述一个 nbd 设备节点的运行时状态。
type DeviceState int

const (
	// DeviceFree 设备空闲，可被分配。
	DeviceFree DeviceState = iota
	// DeviceAllocated 设备已被管理器分配，但未连接后端。
	DeviceAllocated
	// DeviceConnected 设备已连接后端存储，可读写。
	DeviceConnected
)

func (s DeviceState) String() string {
	switch s {
	case DeviceAllocated:
		return "allocated"
	case DeviceConnected:
		return "connected"
	default:
		return "free"
	}
}

// DeviceStatus 是一个设备的状态快照。
type DeviceStatus struct {
	Index int         `json:"index"` // 设备编号，即 /dev/nbd<Index>
	State DeviceState `json:"state"`
	// Backend 是已连接的后端路径或远端地址，仅已连接设备非空。
	Backend string `json:"backend,omitempty"`
	// Managed 表示该设备是否由当前管理器分配。
	Managed bool `json:"managed"`
	// Pids 是正在占用该设备的进程号（来自 /sys/block/nbdN/pid）；
	// 设备被外部进程（非本管理器）占用时，Managed 为 false 而 Pids 非空。
	Pids []int `json:"pids,omitempty"`
}

// Manager 是 nbd 资源管理器，并发安全。
type Manager struct {
	mu       sync.Mutex
	indices  map[int]*device
	qemuNbd  string
	maxPart  int
	inited   bool
	external bool // 模块由外部预先加载，管理器不负责卸载
}

// device 是被管理器分配/连接的设备记录。
type device struct {
	index      int
	backend    string
	connecting bool
}

// DevicePath 返回编号 index 对应的块设备节点路径（/dev/nbdN）。
func DevicePath(index int) string {
	return DevicePathPrefix + strconv.Itoa(index)
}

// PartitionPath 返回编号 index 设备上第 part 个分区的节点路径
// （/dev/nbdNpP）；part 范围受 Initialize 的 MaxPartCount 约束。
func PartitionPath(index, part int) string {
	return fmt.Sprintf("%sp%d", DevicePath(index), part)
}

// Initialize 加载 nbd 内核模块并创建资源管理器。
//
// 参数说明见 [Options]。模块加载规则：
//   - 模块未加载：执行 `modprobe nbd nbds_max=<MaxDevCount> max_part=<MaxPartCount>`；
//   - 模块已加载且设备数量 >= MaxDevCount：复用现有设备（不重载、不卸载）；
//   - 模块已加载但设备数量不足：卸载后按新参数重新加载
//     （要求没有任何进程占用 nbd 设备，否则返回错误）。
//
// 模块就绪后会轮询等待 /dev/nbdN 设备节点创建完成（某些内核依赖
// udev 异步建节点），最长不超过 DeviceReadyTimeout。
func Initialize(opt *Options) (*Manager, error) {
	if opt == nil {
		opt = &Options{}
	}

	maxDev := opt.MaxDevCount
	maxPart := opt.MaxPartCount

	if maxDev < 1 {
		return nil, errors.Errorf("invalid max device count: %d (want >= 1)", maxDev)
	}
	if maxPart < 0 || maxPart > 15 {
		return nil, errors.Errorf("invalid max partition count: %d (want 0..15)", maxPart)
	}

	m := &Manager{
		indices: make(map[int]*device),
		maxPart: maxPart,
		qemuNbd: resolveNbdTool(opt.NbdClientPath),
	}

	loaded, err := moduleLoaded()
	if err != nil {
		return nil, err
	}

	if !loaded {
		if err = m.loadModule(maxDev, maxPart); err != nil {
			return nil, err
		}
	} else {
		// 模块已加载：校验分区数匹配，必要时扩容设备数量。
		if curMaxPart, e := moduleParamInt("max_part"); e == nil && curMaxPart != maxPart {
			return nil, errors.Errorf(
				"nbd module already loaded with max_part=%d, want %d; please unload the module first",
				curMaxPart, maxPart,
			)
		}

		curDevCount, err := detectDeviceCount()
		if err != nil {
			return nil, err
		}

		if curDevCount < maxDev {
			if err = m.reloadModule(maxDev, maxPart); err != nil {
				return nil, err
			}
		} else {
			m.external = true
			logger.Infof("nbd module already loaded (%d devices), reuse it", curDevCount)
		}
	}

	if err = waitForDeviceNodes(maxDev, DeviceReadyTimeout); err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.inited = true
	m.mu.Unlock()

	logger.Infof("nbd manager initialized: %d devices, max_part=%d, client=%s",
		maxDev, maxPart, m.qemuNbd)

	return m, nil
}

// MaxDeviceCount 返回当前可用的 nbd 设备总数。
func (m *Manager) MaxDeviceCount() (int, error) {
	if err := m.checkInit(); err != nil {
		return 0, err
	}

	return detectDeviceCount()
}

// MaxPartCount 返回初始化时约定的单设备最大分区数。
func (m *Manager) MaxPartCount() int {
	return m.maxPart
}

// Alloc 申请 count 个空闲的 nbd 设备，返回其编号列表（升序）。
//
// 空闲的判定条件是：设备未被本管理器占用，且内核侧没有任何进程
// 占用（/sys/block/nbdN/pid 不存在）。这能避免误抢到被其他进程
// （例如外部 qemu-nbd、nbd-client）正在使用的设备。
//
// 分配是原子性的：若空闲设备不足以满足 count，已预留的设备会全部
// 回滚，不会留下半分配状态。
//
// 若当前设备数量不足且没有任何设备被占用，Alloc 会尝试按当前分区
// 数扩容模块；扩容失败时返回不足错误。
func (m *Manager) Alloc(count int) ([]int, error) {
	if err := m.checkInit(); err != nil {
		return nil, err
	}

	if count < 1 {
		return nil, errors.Errorf("invalid alloc count: %d (want >= 1)", count)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	free, total, err := m.scanFreeDevicesLocked()
	if err != nil {
		return nil, err
	}

	if len(m.indices)+count > total {
		return nil, errors.Errorf(
			"alloc %d devices exceeds manager capacity: %d allocated, %d devices total",
			count, len(m.indices), total,
		)
	}

	if len(free) < count {
		if err = m.tryGrowLocked(total); err != nil {
			return nil, errors.Wrapf(err,
				"not enough free nbd devices: have %d, want %d", len(free), count)
		}

		free, _, err = m.scanFreeDevicesLocked()
		if err != nil {
			return nil, err
		}
		if len(free) < count {
			return nil, errors.Errorf("not enough free nbd devices: have %d, want %d", len(free), count)
		}
	}

	picked := free[:count]

	for _, idx := range picked {
		m.indices[idx] = &device{index: idx}
	}

	logger.Debugf("alloc nbd devices: %v", picked)

	return picked, nil
}

// Do 对设备 index 执行 fn 操作；仅允许操作已被本管理器分配的设备，
// 否则返回 [ErrNotAllocated]。fn 的返回值原样返回。
// 对多设备的批量操作请使用 [Manager.DoAll]。
func (m *Manager) Do(index int, fn func(index int) error) error {
	if err := m.checkInit(); err != nil {
		return err
	}

	if fn == nil {
		return errors.New("nil operation")
	}

	m.mu.Lock()
	_, ok := m.indices[index]
	m.mu.Unlock()

	if !ok {
		return errors.Wrapf(ErrNotAllocated, "nbd%d", index)
	}

	return fn(index)
}

// DoAll 对一批设备依次执行 fn；任一设备失败则立即返回错误，
// 后续设备不再执行（已执行的结果不回滚）。
func (m *Manager) DoAll(indices []int, fn func(index int) error) error {
	if len(indices) == 0 {
		return errors.New("empty indices")
	}

	for _, idx := range indices {
		if err := m.Do(idx, fn); err != nil {
			return errors.Wrapf(err, "do on nbd%d", idx)
		}
	}

	return nil
}

// Connect 将后端存储连接到设备，使其成为可读写的块设备：
//
//   - 本地镜像文件：qemu-nbd --connect=/dev/nbdN [--format=F] [--read-only] FILE
//   - 远端 NBD 服务：qemu-nbd --connect=/dev/nbdN 'nbd:HOST[:PORT]'
//
// 连接成功后等待内核完成设备注册（最长 DeviceReadyTimeout）。
// 并发连接同一设备会被拒绝；连接失败不会留下残留状态。
func (m *Manager) Connect(index int, opt *ConnectOptions) error {
	if opt == nil {
		return errors.New("nil connect options")
	}

	if err := m.checkInit(); err != nil {
		return err
	}

	if opt.Backend == "" && opt.Remote == "" {
		return errors.New("either Backend or Remote is required")
	}

	if opt.Format == "host_device" || opt.Format == "host_cdrom" {
		return errors.Errorf("backend format %q is not allowed for nbd", opt.Format)
	}

	if opt.Backend != "" && !pathExists(opt.Backend) {
		return errors.Errorf("backend %q does not exist", opt.Backend)
	}

	m.mu.Lock()
	dev, ok := m.indices[index]
	if !ok {
		m.mu.Unlock()
		return errors.Wrapf(ErrNotAllocated, "nbd%d", index)
	}
	if dev.backend != "" {
		m.mu.Unlock()
		return errors.Wrapf(ErrAlreadyConnected, "nbd%d: %s", index, dev.backend)
	}
	if dev.connecting {
		m.mu.Unlock()
		return errors.Errorf("nbd%d: connect already in progress", index)
	}
	dev.connecting = true
	m.mu.Unlock()

	succeeded := false
	defer func() {
		m.mu.Lock()
		dev.connecting = false
		if !succeeded {
			dev.backend = ""
		}
		m.mu.Unlock()
	}()

	cmdline := buildConnectCmd(m.qemuNbd, index, opt)

	ctx, cancel := context.WithTimeout(context.Background(), DeviceReadyTimeout)
	defer cancel()

	if err := runTool(ctx, cmdline); err != nil {
		return errors.Wrapf(err, "connect nbd%d", index)
	}

	time.Sleep(ConnectDelay)

	if !deviceConnected(index) {
		return errors.Errorf("nbd%d: qemu-nbd exited but device is not connected", index)
	}

	m.mu.Lock()
	if opt.Backend != "" {
		dev.backend = opt.Backend
	} else {
		dev.backend = opt.Remote
	}
	m.mu.Unlock()

	succeeded = true

	logger.Debugf("nbd%d connected to %q", index, dev.backend)

	return nil
}

// Disconnect 断开设备的后端连接并等待 IO 收尾；
// 设备未连接时直接返回 nil（幂等）。
//
// 断开前会自动清理设备之上的块设备堆栈（分区、LVM、dm-crypt、
// md/raid 等，见 [Manager.CleanupDeviceStack]），否则内核会以
// "device busy" 拒绝断开；断开后还会等待分区节点被内核回收。
// 注意：堆栈清理不会卸载文件系统，若分区仍被挂载请先自行 umount。
func (m *Manager) Disconnect(index int) error {
	if err := m.checkInit(); err != nil {
		return err
	}

	m.mu.Lock()
	dev, ok := m.indices[index]
	if !ok {
		m.mu.Unlock()
		return errors.Wrapf(ErrNotAllocated, "nbd%d", index)
	}
	m.mu.Unlock()

	if !deviceConnected(index) {
		return nil
	}

	// 先清理上层设备堆栈（分区/LVM/加密等），否则断开会被内核拒绝。
	result, err := m.CleanupDeviceStack(index)
	if err != nil {
		return errors.Wrapf(err, "disconnect nbd%d", index)
	}

	logger.Debugf("nbd%d stack cleanup: %+v", index, result)

	cmdline := fmt.Sprintf("%s --disconnect %s", m.qemuNbd, DevicePath(index))

	ctx, cancel := context.WithTimeout(context.Background(), DeviceReadyTimeout)
	defer cancel()

	if err := runTool(ctx, cmdline); err != nil {
		return errors.Wrapf(err, "disconnect nbd%d", index)
	}

	// 等待连接真正断开，避免后续释放/再连接时竞争。
	if err := waitDeviceDisconnected(index, DeviceReadyTimeout); err != nil {
		return errors.Wrapf(err, "disconnect nbd%d", index)
	}

	// 等待分区节点被内核回收完毕。
	if err := waitPartitionsGone(index, PartitionsSettleTimeout); err != nil {
		return errors.Wrapf(err, "disconnect nbd%d", index)
	}

	time.Sleep(DisconnectSettleDelay)

	m.mu.Lock()
	dev.backend = ""
	m.mu.Unlock()

	logger.Debugf("nbd%d disconnected", index)

	return nil
}

// Release 释放设备：若已连接则先断开，然后归还设备编号。
// 设备未分配时返回 [ErrNotAllocated]。
func (m *Manager) Release(index int) error {
	if err := m.checkInit(); err != nil {
		return err
	}

	m.mu.Lock()
	dev, ok := m.indices[index]
	if !ok {
		m.mu.Unlock()
		return errors.Wrapf(ErrNotAllocated, "nbd%d", index)
	}
	needDisconnect := dev.backend != ""
	m.mu.Unlock()

	if needDisconnect {
		if err := m.Disconnect(index); err != nil {
			return err
		}
	}

	m.mu.Lock()
	delete(m.indices, index)
	m.mu.Unlock()

	logger.Debugf("nbd%d released", index)

	return nil
}

// ReleaseAll 释放管理器持有的全部设备；
// 单个设备失败不中断整体，最终汇总返回所有错误。
func (m *Manager) ReleaseAll() error {
	if err := m.checkInit(); err != nil {
		return err
	}

	m.mu.Lock()
	indices := make([]int, 0, len(m.indices))
	for idx := range m.indices {
		indices = append(indices, idx)
	}
	m.mu.Unlock()

	var errs []string
	for _, idx := range indices {
		if err := m.Release(idx); err != nil {
			errs = append(errs, fmt.Sprintf("nbd%d: %v", idx, err))
		}
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}

	return nil
}

// Status 返回设备状态快照：不带参数返回全部设备，
// 传入 indices 时只返回这些编号的状态。
func (m *Manager) Status(indices ...int) ([]*DeviceStatus, error) {
	if err := m.checkInit(); err != nil {
		return nil, err
	}

	total, err := detectDeviceCount()
	if err != nil {
		return nil, err
	}

	want := make(map[int]bool, len(indices))
	for _, idx := range indices {
		want[idx] = true
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	list := make([]*DeviceStatus, 0, total)

	for i := 0; i < total; i++ {
		if len(want) > 0 && !want[i] {
			continue
		}

		st := &DeviceStatus{Index: i, State: DeviceFree}
		st.Pids = devicePids(i)

		if dev, ok := m.indices[i]; ok {
			st.Managed = true
			if dev.backend != "" {
				st.State = DeviceConnected
				st.Backend = dev.backend
			} else {
				st.State = DeviceAllocated
			}
		}

		list = append(list, st)
	}

	return list, nil
}

// Close 释放全部设备；若模块是本管理器加载的，还会卸载模块。
// Close 之后管理器不可再使用。
func (m *Manager) Close() error {
	if err := m.checkInit(); err != nil {
		return err
	}

	var firstErr error

	if err := m.ReleaseAll(); err != nil {
		firstErr = err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.external {
		if err := runTool(context.Background(), modprobeCaller+" -r "+KernelModule); err != nil {
			logger.Warnf("unload nbd module: %v", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	m.inited = false
	m.indices = make(map[int]*device)

	return firstErr
}

// ---------- 内部实现 ----------

func (m *Manager) checkInit() error {
	if m == nil {
		return errors.Wrap(ErrNotInitialized, "nil manager")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.inited {
		return ErrNotInitialized
	}

	return nil
}

// scanFreeDevicesLocked 扫描所有空闲设备，返回升序编号列表与设备总数。
// 调用方必须持有 m.mu。
func (m *Manager) scanFreeDevicesLocked() (free []int, total int, err error) {
	total, err = detectDeviceCount()
	if err != nil {
		return nil, 0, err
	}

	free = make([]int, 0, total)

	for i := 0; i < total; i++ {
		if _, managed := m.indices[i]; managed {
			continue
		}

		// 内核侧占用检查：/sys/block/nbdN/pid 存在即被占用。
		if len(devicePids(i)) > 0 {
			continue
		}

		free = append(free, i)
	}

	return free, total, nil
}

// tryGrowLocked 尝试扩容设备数量（卸载并按新参数重新加载模块）。
// 仅当没有任何设备被本管理器或外部进程占用时才允许扩容。
// 调用方必须持有 m.mu。
func (m *Manager) tryGrowLocked(curTotal int) error {
	if len(m.indices) > 0 {
		return errors.New("cannot grow device pool: devices are allocated")
	}

	for i := 0; i < curTotal; i++ {
		if len(devicePids(i)) > 0 {
			return errors.Errorf("cannot grow device pool: nbd%d is in use by others", i)
		}
	}

	newMax := curTotal * 2

	logger.Infof("grow nbd device pool: %d -> %d", curTotal, newMax)

	return m.reloadModule(newMax, m.maxPart)
}

// loadModule 加载 nbd 模块。
func (m *Manager) loadModule(maxDev, maxPart int) error {
	cmdline := fmt.Sprintf("%s %s nbds_max=%d max_part=%d",
		modprobeCaller, KernelModule, maxDev, maxPart)

	if err := runTool(context.Background(), cmdline); err != nil {
		return errors.Wrapf(err, "load nbd module")
	}

	return nil
}

// reloadModule 卸载并按新参数重新加载模块；
// 要求没有进程占用任何 nbd 设备，否则卸载会失败。
func (m *Manager) reloadModule(maxDev, maxPart int) error {
	loaded, err := moduleLoaded()
	if err != nil {
		return err
	}

	if loaded {
		if err = runTool(context.Background(), modprobeCaller+" -r "+KernelModule); err != nil {
			return errors.Wrapf(err, "unload nbd module for reload")
		}
	}

	return m.loadModule(maxDev, maxPart)
}

// moduleLoaded 判断 nbd 模块是否已加载。
func moduleLoaded() (bool, error) {
	if pathExists(filepath.Join("/sys/module", KernelModule)) {
		return true, nil
	}

	data, err := os.ReadFile(procModulesFile)
	if err != nil {
		return false, errors.Wrapf(err, "read %s", procModulesFile)
	}

	for _, line := range strings.Split(string(data), "\n") {
		if fields := strings.Fields(line); len(fields) > 0 && fields[0] == KernelModule {
			return true, nil
		}
	}

	return false, nil
}

// moduleParamInt 读取 /sys/module/nbd/parameters/<name> 的整数值。
func moduleParamInt(name string) (int, error) {
	path := filepath.Join("/sys/module", KernelModule, "parameters", name)

	data, err := os.ReadFile(path)
	if err != nil {
		return 0, errors.Wrapf(err, "read nbd parameter %s", name)
	}

	v, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, errors.Wrapf(err, "parse nbd parameter %s=%q", name, string(data))
	}

	return v, nil
}

// detectDeviceCount 通过 /sys/block 统计 nbd 设备数量。
func detectDeviceCount() (int, error) {
	entries, err := os.ReadDir(sysBlockDir)
	if err != nil {
		return 0, errors.Wrapf(err, "read %s", sysBlockDir)
	}

	count := 0

	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "nbd") {
			continue
		}

		if _, err = strconv.Atoi(name[3:]); err == nil {
			count++
		}
	}

	return count, nil
}

// devicePids 返回占用设备的进程号列表；
// /sys/block/nbdN/pid 内容为逗号分隔的 pid 集合，未占用时文件不存在。
func devicePids(index int) []int {
	path := filepath.Join(sysBlockDir, fmt.Sprintf("nbd%d", index), "pid")

	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var pids []int

	for _, s := range strings.Split(strings.TrimSpace(string(data)), ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}

		if pid, e := strconv.Atoi(s); e == nil && pid > 0 {
			pids = append(pids, pid)
		}
	}

	return pids
}

// deviceConnected 判断设备是否已连接（内核侧存在占用进程即视为已连接）。
func deviceConnected(index int) bool {
	return len(devicePids(index)) > 0
}

// waitDeviceDisconnected 轮询等待设备断开连接。
func waitDeviceDisconnected(index int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if !deviceConnected(index) {
			return nil
		}

		time.Sleep(DevicePollInterval)
	}

	return errors.Errorf("wait nbd%d disconnect timeout (%v)", index, timeout)
}

// waitForDeviceNodes 等待 /dev/nbd0..nbd<N-1> 节点全部就绪。
func waitForDeviceNodes(maxDev int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if deviceNodeReady(maxDev) {
			return nil
		}

		time.Sleep(DevicePollInterval)
	}

	return errors.Errorf("wait nbd device nodes ready timeout (%v)", timeout)
}

// deviceNodeReady 检查 0..maxDev-1 的节点是否全部存在。
func deviceNodeReady(maxDev int) bool {
	for i := 0; i < maxDev; i++ {
		if !pathExists(DevicePath(i)) {
			return false
		}
	}

	return true
}

// pathExists 判断路径是否存在。
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// resolveNbdTool 解析 qemu-nbd 路径：显式指定 > PATH 查找 > 裸命令名。
func resolveNbdTool(explicit string) string {
	if explicit != "" {
		return explicit
	}

	if p, err := exec.LookPath(nbdClientTool); err == nil {
		return p
	}

	return nbdClientTool
}

// runTool 执行外部命令并检查退出码。
func runTool(ctx context.Context, cmdline string) error {
	exit, out, err := commandExecute(ctx, cmdline)
	if err != nil {
		return err
	}

	if exit != 0 {
		return errors.Errorf("command %q exit with %d: %s", cmdline, exit, strings.TrimSpace(out))
	}

	return nil
}
