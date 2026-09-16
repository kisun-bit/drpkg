package backend

import "fmt"

// NFSConfig 描述 NFS 网络文件存储的访问信息。
type NFSConfig struct {
	// Server NFS 服务器地址，如 "192.168.1.100"。
	Server string
	// ExportPath NFS 导出路径，如 "/backup"。
	ExportPath string
	// SubPath 导出目录下的存储路径，如 "data/backup001"。
	SubPath string
	// Port NFS 服务端口，0 表示默认端口 2049。
	Port int
	// Version NFS 协议版本，如 "3"、"4"。
	Version string
	// MountOptions 挂载参数，如读写模式、超时、重传次数等。
	MountOptions string
}

// Type 返回 NFS 介质类型。
func (NFSConfig) Type() Type { return StorageTypeNFS }

// nfsAccessor 实现 NFS 网络文件存储的访问能力。
type nfsAccessor struct {
	netFs
	cfg NFSConfig
}

func init() {
	Register(StorageTypeNFS, newNFS)
}

func newNFS(cfg Config) (Accessor, error) {
	c, ok := cfg.(NFSConfig)
	if !ok {
		return nil, fmt.Errorf("backend: nfs requires NFSConfig, got %T", cfg)
	}
	return &nfsAccessor{netFs: netFs{fsState: newFSState()}, cfg: c}, nil
}

func (a *nfsAccessor) Type() Type { return StorageTypeNFS }

// Online 建立 NFS 挂载（平台相关）。
func (a *nfsAccessor) Online() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return nfsOnline(a)
}

// Offline 卸载由本访问器建立的 NFS 挂载（平台相关）。
func (a *nfsAccessor) Offline() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.status == StatusOffline {
		return nil
	}
	return nfsOffline(a)
}
