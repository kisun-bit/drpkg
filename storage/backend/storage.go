// Package backend 提供对存储介质的统一访问能力，屏蔽本地文件系统、CIFS、
// NFS 与 S3 对象存储之间的访问差异。
//
// # 扩展介质
//
// 新增一种存储介质只需：
//  1. 定义该介质的配置结构，实现 Config 接口（Type 方法返回介质类型）；
//  2. 实现 Accessor 接口（生命周期与文件操作，可复用 fsState/netFs 基类）；
//  3. 在实现中通过 Register 注册工厂函数。
//
// 无需修改本包的入口与既有介质实现。
//
// # 扩展能力
//
// 新增一种存储能力（如对象列举、分片上传）时，定义独立的小接口，由支持的
// 介质选择性实现，调用方通过类型断言获取该能力，不改动 Accessor 基础接口。
//
// # 当前未实现的介质
//
// 磁带库（磁带机、介质池、块大小、压缩/加密、保留期等）本期暂不实现，
// 后续按同样方式新增。
package backend

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Type 表示存储介质类型。
type Type string

const (
	// StorageTypeLocalFilesystem 表示本地文件系统存储。
	StorageTypeLocalFilesystem Type = "localfs"

	// StorageTypeS3 表示 S3 对象存储。
	StorageTypeS3 Type = "s3"

	// StorageTypeCIFS 表示 CIFS/SMB 网络文件存储。
	StorageTypeCIFS Type = "cifs"

	// StorageTypeNFS 表示 NFS 网络文件存储。
	StorageTypeNFS Type = "nfs"
)

// Status 表示存储介质当前状态。
type Status string

const (
	// StatusOnline 表示存储介质当前可访问。
	StatusOnline Status = "online"

	// StatusOffline 表示存储介质当前未建立访问连接。
	StatusOffline Status = "offline"

	// StatusError 表示存储介质处于异常状态。
	StatusError Status = "error"
)

// Config 是存储介质配置的统一抽象。
//
// 每种存储介质提供一个独立的配置实现（如 LocalFsConfig、CIFSConfig、
// NFSConfig、S3Config），各字段仅与其所属介质相关，不再平铺到一个结构体里。
// New 依据 Type 在注册表中查找对应的工厂函数完成创建。
//
// 新增介质时，只需新增一个配置实现、实现 Accessor 并在其上调用 Register，
// 无需改动本包已有代码。
type Config interface {
	// Type 返回该配置对应的介质类型。
	Type() Type
}

// Factory 基于具体配置创建对应的存储介质访问器。
type Factory func(cfg Config) (Accessor, error)

var registry = map[Type]Factory{}

// Register 注册指定介质类型的工厂函数。
//
// 类型为空、工厂为 nil 或重复注册会 panic，用于在程序启动阶段暴露配置错误。
func Register(t Type, f Factory) {
	if t == "" || f == nil {
		panic("backend: register requires non-empty type and non-nil factory")
	}
	if _, dup := registry[t]; dup {
		panic("backend: duplicate storage type " + string(t))
	}
	registry[t] = f
}

// New 根据配置创建对应的存储介质访问器。
//
// 返回的访问器初始状态为离线（StatusOffline），调用方需要先执行 Online
// 建立访问能力后再进行文件操作。
func New(cfg Config) (Accessor, error) {
	if cfg == nil {
		return nil, fmt.Errorf("backend: nil config")
	}
	f, ok := registry[cfg.Type()]
	if !ok {
		return nil, fmt.Errorf("backend: unsupported storage type: %q", cfg.Type())
	}
	return f(cfg)
}

// Lifecycle 管理存储介质的生命周期。
type Lifecycle interface {
	// Online 建立存储介质的访问能力，并将介质置为在线状态。
	//
	// 实现应保证幂等或对重复调用有明确定义；建立失败时应将介质置为
	// 错误状态并返回具体原因。
	Online() error

	// Offline 释放存储介质的访问能力，并将介质置为离线状态。
	//
	// 如果介质由其他组件持有访问，实现不应强制释放他人的资源。
	Offline() error

	// Test 检测存储介质当前是否可访问。
	//
	// Test 不改变生命周期状态，也不负责建立连接或挂载。
	Test() error
}

// FileStore 提供存储介质上的文件操作。
type FileStore interface {
	// CreateFile 创建或打开指定路径对应的存储文件，并返回统一访问对象。
	//
	// path 为存储介质内的相对路径（对 S3 为对象 Key）。
	CreateFile(path string) (*StorageFile, error)

	// RemoveAll 删除指定路径对应的存储对象及其子对象，语义上等价于
	// 文件系统的递归删除。
	RemoveAll(path string) error
}

// OpenFile 提供打开已有存储对象进行只读访问的能力。
//
// 需要读取已有对象的介质实现该接口；调用方通过类型断言按需获取，例如：
//
//	if r, ok := acc.(OpenFile); ok {
//	    f, _ := r.OpenFile("key")
//	}
type OpenFile interface {
	// OpenFile 以只读方式打开已有对象，返回统一文件访问对象。
	OpenFile(path string) (*StorageFile, error)
}

// Accessor 是存储介质的统一访问入口。
//
// 它组合介质元信息（Type/Status）、生命周期管理（Lifecycle）与文件操作
// （FileStore）。未来新增的存储能力（如对象列举、分片上传等）通过独立接口
// 定义，并由具体介质选择性实现，调用方通过类型断言获取，不修改本接口。
type Accessor interface {
	Type() Type
	Status() Status
	Lifecycle
	FileStore
}

// storagePath 将相对路径 p 拼接到根目录 root，拒绝绝对路径与 ".." 逃逸，
// 防止存储根目录之外的文件被意外创建或删除。
func storagePath(root, p string) (string, error) {
	if p == "" {
		return root, nil
	}
	if filepath.IsAbs(p) {
		return "", fmt.Errorf("backend: absolute path not allowed: %s", p)
	}
	full := filepath.Join(root, p)
	rel, err := filepath.Rel(root, full)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("backend: path escapes storage root: %s", p)
	}
	return full, nil
}
