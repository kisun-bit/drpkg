package drerr

import "fmt"

// Category 是灾备异常的分类维度。每个 Code 都归属且仅归属一个 Category，
// 语义判断方法与可重试性默认均由 Category 派生。
type Category uint8

const (
	CategoryUnknown       Category = 0x00 // 未分类
	CategoryInternal      Category = 0x01 // 内部异常（系统级、未处理或 panic）
	CategoryNetwork       Category = 0x02 // 网络通信异常
	CategoryStorage       Category = 0x03 // 存储介质 I/O 异常
	CategoryCanceled      Category = 0x04 // 任务被取消/中断
	CategoryTimeout       Category = 0x05 // 操作超时
	CategoryNotFound      Category = 0x06 // 目标不存在（快照/备份/卷等）
	CategoryAlreadyExists Category = 0x07 // 目标已存在
	CategoryPermission    Category = 0x08 // 权限不足
	CategoryQuota         Category = 0x09 // 空间/配额/授权不足
	CategoryBusy          Category = 0x0A // 资源被占用
	CategoryCorrupted     Category = 0x0B // 数据损坏/校验失败
	CategoryVersion       Category = 0x0C // 版本不兼容/不支持
	CategoryState         Category = 0x0D // 状态不合法/不一致
	CategoryArgument      Category = 0x0E // 参数不合法
)

var categoryNames = map[Category]string{
	CategoryUnknown:       "Unknown",
	CategoryInternal:      "Internal",
	CategoryNetwork:       "Network",
	CategoryStorage:       "Storage",
	CategoryCanceled:      "Canceled",
	CategoryTimeout:       "Timeout",
	CategoryNotFound:      "NotFound",
	CategoryAlreadyExists: "AlreadyExists",
	CategoryPermission:    "Permission",
	CategoryQuota:         "Quota",
	CategoryBusy:          "Busy",
	CategoryCorrupted:     "Corrupted",
	CategoryVersion:       "Version",
	CategoryState:         "State",
	CategoryArgument:      "Argument",
}

// String 返回分类的稳定名称，未知分类返回 "Unknown"。
func (c Category) String() string {
	if name, ok := categoryNames[c]; ok {
		return name
	}
	return categoryNames[CategoryUnknown]
}

// Code 是灾备异常的唯一、机器可读标识。
//
// 编码采用三段式 32 位布局（实际载体为 uint64，仅使用低 32 位）：
//
//	0x21 <分类> <子码>
//	 ^^   ^^^^   ^^^^
//	 ｜    ｜      └─ 分类内子码（8 bit）
//	 ｜    └──────── 分类（8 bit，取值见 Category）
//	 └────────────── 产品域，固定 0x21（灾备）
//
// 例：0x210601 = 域 0x21 + 分类 0x06(NotFound) + 子码 0x01。
type Code uint64

const (
	// 内部异常
	Internal      Code = 0x210101 // 内部/未知异常
	InternalPanic Code = 0x210102 // panic / 未捕获异常

	// 网络通信异常
	Network            Code = 0x210201 // 网络通信失败
	NetworkTimeout     Code = 0x210202 // 网络超时
	NetworkUnreachable Code = 0x210203 // 网络不可达

	// 存储介质异常
	Storage Code = 0x210301 // 存储介质读写失败

	// 取消/中断
	Canceled Code = 0x210401 // 任务被取消

	// 超时
	Timeout Code = 0x210501 // 操作超时

	// 不存在
	NotFound         Code = 0x210601 // 目标不存在
	SnapshotNotFound Code = 0x210602 // 快照不存在
	BackupNotFound   Code = 0x210603 // 备份不存在
	VolumeNotFound   Code = 0x210604 // 卷不存在

	// 已存在
	AlreadyExists  Code = 0x210701 // 目标已存在
	BackupExists   Code = 0x210702 // 备份已存在
	SnapshotExists Code = 0x210703 // 快照已存在

	// 权限
	PermissionDenied Code = 0x210801 // 权限不足

	// 空间/配额/授权
	OutOfSpace     Code = 0x210901 // 存储空间不足
	QuotaExceeded  Code = 0x210902 // 配额超限
	LicenseExpired Code = 0x210903 // 授权/许可过期

	// 资源占用
	Busy Code = 0x210A01 // 资源被占用

	// 数据损坏/校验
	Corrupted        Code = 0x210B01 // 数据损坏
	ChecksumMismatch Code = 0x210B02 // 校验和不匹配

	// 版本不兼容/不支持
	VersionMismatch Code = 0x210C01 // 版本不兼容
	Unsupported     Code = 0x210C02 // 不支持的版本/特性

	// 状态不合法/不一致
	InvalidState  Code = 0x210D01 // 状态不合法
	NotConsistent Code = 0x210D02 // 数据不一致（不可用于恢复）

	// 参数不合法
	InvalidArgument Code = 0x210E01 // 参数不合法
)

// codeNames 是 code 到稳定异常名的映射，用于保证 Error 输出中的名称一致。
var codeNames = map[Code]string{
	Internal:           "InternalException",
	InternalPanic:      "InternalPanic",
	Network:            "NetworkException",
	NetworkTimeout:     "NetworkTimeout",
	NetworkUnreachable: "NetworkUnreachable",
	Storage:            "StorageException",
	Canceled:           "Canceled",
	Timeout:            "Timeout",
	NotFound:           "NotFound",
	SnapshotNotFound:   "SnapshotNotFound",
	BackupNotFound:     "BackupNotFound",
	VolumeNotFound:     "VolumeNotFound",
	AlreadyExists:      "AlreadyExists",
	BackupExists:       "BackupExists",
	SnapshotExists:     "SnapshotExists",
	PermissionDenied:   "PermissionDenied",
	OutOfSpace:         "OutOfSpace",
	QuotaExceeded:      "QuotaExceeded",
	LicenseExpired:     "LicenseExpired",
	Busy:               "Busy",
	Corrupted:          "Corrupted",
	ChecksumMismatch:   "ChecksumMismatch",
	VersionMismatch:    "VersionMismatch",
	Unsupported:        "Unsupported",
	InvalidState:       "InvalidState",
	NotConsistent:      "NotConsistent",
	InvalidArgument:    "InvalidArgument",
}

// category 返回 code 编码中的分类字节对应的 Category。
func (c Code) category() Category { return Category((c >> 8) & 0xFF) }

// subcode 返回 code 编码中的子码字节。
func (c Code) subcode() uint8 { return uint8(c & 0xFF) }

// Category 返回 code 归属的异常分类。
func (c Code) Category() Category { return c.category() }

// Name 返回 code 的稳定异常名（不含十六进制值），未知 code 返回
// "Unknown-<子码>"。
func (c Code) Name() string {
	if name, ok := codeNames[c]; ok {
		return name
	}
	return fmt.Sprintf("Unknown-%02x", c.subcode())
}

// Hex 返回 code 的小写十六进制表示，形如 "0x210601"。
func (c Code) Hex() string { return fmt.Sprintf("0x%x", uint64(c)) }

// String 返回 code 的完整可读形式：<异常名>[0x<code>]。
func (c Code) String() string {
	return fmt.Sprintf("%s[%s]", c.Name(), c.Hex())
}

// Retryable 报告该 code 代表的异常是否可重试（瞬态）。默认按分类判定：
// 网络、超时、资源忙属于可重试场景；其余（取消、不存在、权限、空间不足、
// 数据损坏等）重试无意义，需人工或外部干预。
func (c Code) Retryable() bool {
	switch c.category() {
	case CategoryNetwork, CategoryTimeout, CategoryBusy:
		return true
	default:
		return false
	}
}
