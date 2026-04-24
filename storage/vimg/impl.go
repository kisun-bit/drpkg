package vimg

type Manager interface {
	Create(opts CreateOptions) (*VImg, error)
	CreateFromBacking(opts CreateFromBackingOptions) (*VImg, error)

	Open(metaPath string) (*Image, error)

	Delete(guid string) error
}

type Image interface {
	Info() *VImg

	WriteAt(p []byte, off uint64) error
	ReadAt(p []byte, off uint64) error

	Commit() error // merge 到 backing
	Rebase(newBacking string) error

	Close() error
}

type CreateOptions struct {
	// Dir 镜像存放目录（filesystem 模式）
	// 例如：/data/vimg/
	Dir string

	// VirtualSize 虚拟磁盘大小（字节）
	// 必须 512 对齐
	VirtualSize uint64

	// ClusterSize 数据块大小（字节）
	// 推荐：64KB / 128KB / 256KB
	// 必须 512 对齐
	ClusterSize uint32

	// Compression 压缩算法
	Compression Compression

	// Encryption 加密算法
	Encryption Encryption

	// Preallocate 是否预分配空间（可选）
	// true  → 预分配 DATA 文件（提升顺序写性能）
	// false → 稀疏文件（默认）
	Preallocate bool

	// Description 描述信息（写入 META，可选）
	Description string

	// CustomMeta 用户自定义元数据（可选）
	CustomMeta map[string]string
}

type CreateFromBackingOptions struct {
	CreateOptions

	// BackingGuid 父镜像 GUID（推荐）
	BackingGuid string

	// BackingMetaPath 父镜像 META 路径（更可靠）
	// 如果填写，会优先使用这个
	BackingMetaPath string

	// CopyOnWrite 是否启用写时复制（默认 true）
	// 一般你这个架构必须是 true
	CopyOnWrite bool

	// ReadOnlyBacking 父镜像是否只读（默认 true）
	// 强烈建议 true，否则容易炸一致性
	ReadOnlyBacking bool
}
