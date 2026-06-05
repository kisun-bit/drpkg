package vimg

// StorageType 表示镜像数据的存储后端类型。
type StorageType uint32

const (
	// StorageTypeFilesystem 表示镜像存储在本地文件系统中。
	StorageTypeFilesystem StorageType = iota

	// StorageTypeS3 表示镜像存储在 S3 兼容的对象存储中。
	StorageTypeS3
)

// Layout 表示镜像文件的组织布局方式。
type Layout uint8

const (
	// LayoutFile 表示“多文件平铺布局”：
	// 每个镜像由多个文件组成，文件命名为：{guid}.{type}
	// 示例：
	//   vimg_xxx.DATA
	//   vimg_xxx.IDX
	//   vimg_xxx.META
	LayoutFile Layout = iota

	// FIXME: 当前版本暂不实现
	// LayoutDir 表示“目录布局”：
	// 每个镜像为一个目录，目录名为 {guid}，内部包含：
	//   DATA  数据文件
	//   IDX   索引文件
	//   META  元数据文件
	LayoutDir
)

// Compression 表示数据块的压缩算法。
type Compression uint8

const (
	// CompressionNone 表示不进行压缩。
	CompressionNone Compression = iota

	// CompressionLZ4 表示使用 LZ4 压缩。
	CompressionLZ4
)

// Encryption 表示数据块的加密算法。
type Encryption uint8

const (
	// EncryptionNone 表示不加密。
	EncryptionNone Encryption = iota

	// EncryptionAES256 表示使用 AES-256 加密。
	EncryptionAES256
)

// State 表示镜像当前的生命周期状态。
// 注意：该字段仅用于标识状态，不参与数据一致性控制。
type State uint8

const (
	// StateUnknown 表示状态未知或无法识别。
	StateUnknown State = iota

	// StateCreated 表示镜像已创建，但尚未写入数据。
	StateCreated

	// StateWriting 表示镜像正在写入数据。
	StateWriting

	// StateCompleted 表示镜像写入完成，可正常使用。
	StateCompleted
)

// VImg 表示一个虚拟磁盘镜像。
// 一个镜像由三个逻辑文件组成：DATA / IDX / META。
//
// 数据处理流程：
//   - 若同时启用压缩与加密：先压缩，再加密。
//
// DATA：
//   - 顺序追加写入 Cluster（不允许覆盖写）。
//   - 每次写入必须对齐并写满一个 Cluster：
//     若写入数据不足一个 Cluster，需要从 Backing 镜像读取剩余部分补齐。
//
// IDX：
//   - 顺序追加写入 IndexEntry，用于定位 DATA 中的 Cluster。
//   - 查找逻辑：
//     若 IDX 中存在该 Cluster → 从 DATA 读取
//     若不存在：
//     1. 有 Backing → 递归从 Backing 读取
//     2. 无 Backing → 返回全 0 数据
//
// META：
//   - 存储镜像元信息（大小、布局、压缩方式等）。
type VImg struct {
	// Layout 镜像布局方式（文件布局或目录布局）。
	Layout Layout `json:"layout"`

	// State 镜像状态，仅用于标识当前阶段。
	State State `json:"state"`

	// VirtualSize 虚拟磁盘大小（字节），支持任意正整数（字节粒度）。
	VirtualSize uint64 `json:"virtualSize"`

	// ClusterSize 数据块大小（字节），必须 512 对齐。
	ClusterSize uint32 `json:"clusterSize"`

	// Guid 镜像唯一标识。
	// 格式为："vimg_" + 32位十六进制字符串（UUID 去除连字符）。
	Guid string `json:"guid"`

	// BackingGuid 父镜像标识（可选）。
	// 若不为空，则表示该镜像为增量镜像。
	BackingGuid string `json:"backingGuid"`

	// Compression 数据压缩算法。
	Compression Compression `json:"compression"`

	// Encryption 数据加密算法。
	// 注意：若同时启用压缩与加密，则处理顺序为：先压缩，再加密。
	Encryption Encryption `json:"encryption"`

	// StorageType 存储类型（文件系统或对象存储）。
	StorageType StorageType `json:"storageType"`

	// StoragePrivateInfo 存储后端相关的私有配置（JSON 格式字符串）。
	// 其结构取决于 StorageType，不同类型对应不同字段。
	//
	// 通用约定：
	//   - 建议包含 "version" 字段，用于兼容未来扩展。
	//   - 所有路径 / key 指向“当前镜像的 META 文件”。
	//   - backingXXX 字段用于指向父镜像的 META 文件（可选）。
	//
	// 当 StorageType 为 StorageTypeFilesystem 时：
	// {
	//   "version": 1,
	//   "filePath": "/path/to/current/meta",        // 当前镜像 META 文件路径（必须）
	//   "backingFilePath": "/path/to/backing/meta"  // 父镜像 META 文件路径（可选）
	// }
	//
	// 当 StorageType 为 StorageTypeS3 时：
	// {
	//   "version": 1,
	//   "endpoint":   "xxxx",   // S3 endpoint（必须）
	//   "accessKey":  "xxxx",
	//   "secretKey":  "xxxx",
	//   "region":     "xxxx",   // 可选
	//   "style":      "path|virtual-hosted",
	//
	//   "bucket":     "xxxx",   // 必须
	//   "key":        "xxxx",   // 当前镜像 META 对象 key（必须）
	//
	//   "backingKey": "xxxx",   // 父镜像 META key（可选）
	//
	//   "secure":     true,     // 是否使用 HTTPS（默认 true）
	//   "timeoutSec": 30        // 请求超时（秒，可选）
	// }
	StoragePrivateInfo string `json:"storagePrivateInfo"`
}

// Cluster 表示 DATA 文件中的一个数据块。
type Cluster struct {
	// Index 数据块索引（从 0 开始）。
	Index uint64

	// RawSize 原始数据大小（未压缩、未加密）。
	RawSize uint32

	// DataCRC32 数据校验值（针对最终写入 DATA 的字节）。
	DataCRC32 uint32

	// DataLen 实际存储数据长度（压缩/加密后）。
	DataLen uint32 `struc:"sizeof=Data"`

	// Data 数据内容（可能已压缩或加密）。
	Data []byte
}

// IndexEntry 表示 IDX 文件中的一个索引项。
type IndexEntry struct {
	// ClusterIndex 对应的数据块索引。
	ClusterIndex uint64

	// OffsetInDATA 数据块在 DATA 文件中的偏移（字节）。
	OffsetInDATA uint64

	// LengthInDATA 数据块在 DATA 文件中的长度（字节）。
	LengthInDATA uint32
}

// MapSource 表示 Map 结果中某段数据的来源。
type MapSource uint8

const (
	// MapSourceData 表示该区间数据由当前镜像本层提供（IDX 命中）。
	MapSourceData MapSource = iota

	// MapSourceBacking 表示该区间数据来自 backing 链中的某一层。
	MapSourceBacking

	// MapSourceZero 表示该区间没有任何层提供数据，读取结果应为全 0。
	MapSourceZero
)

// MapSegment 表示一个连续字节区间在逻辑上的数据来源映射。
type MapSegment struct {
	Offset uint64    `json:"offset"`
	Length uint64    `json:"length"`
	Source MapSource `json:"source"`

	// OwnerGuid 为提供数据的镜像 GUID。
	// 当 Source=MapSourceZero 时为空字符串。
	OwnerGuid string `json:"ownerGuid,omitempty"`
}

// BackingRef 表示当前镜像关联的 backing 信息。
type BackingRef struct {
	Guid     string `json:"guid"`
	MetaPath string `json:"metaPath"`
}

// TODO 实现创建、基于backing创建、commit合并、rebase变基、delete删除等接口
