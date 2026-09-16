package backend

import "fmt"

// CIFSConfig 描述 CIFS/SMB 网络文件存储的访问信息。
type CIFSConfig struct {
	// Server CIFS 服务器地址，如 "192.168.1.100"。
	Server string
	// Share CIFS 共享名称，如 "backup"。
	Share string
	// SubPath 共享目录下的存储路径，如 "data/backup001"。
	SubPath string
	// Username 访问用户名。
	Username string
	// Password 访问密码。
	Password string
	// Domain Windows 域或工作组，可选。
	Domain string
	// Port SMB 服务端口，0 表示默认端口 445。
	Port int
	// SMBVersion SMB 协议版本，可选。
	SMBVersion string
}

// Type 返回 CIFS 介质类型。
func (CIFSConfig) Type() Type { return StorageTypeCIFS }

// cifsAccessor 实现 CIFS/SMB 网络文件存储的访问能力。
type cifsAccessor struct {
	netFs
	cfg CIFSConfig
}

func init() {
	Register(StorageTypeCIFS, newCIFS)
}

func newCIFS(cfg Config) (Accessor, error) {
	c, ok := cfg.(CIFSConfig)
	if !ok {
		return nil, fmt.Errorf("backend: cifs requires CIFSConfig, got %T", cfg)
	}
	return &cifsAccessor{netFs: netFs{fsState: newFSState()}, cfg: c}, nil
}

func (a *cifsAccessor) Type() Type { return StorageTypeCIFS }

// Online 建立 CIFS 挂载（平台相关）。
func (a *cifsAccessor) Online() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return cifsOnline(a)
}

// Offline 卸载由本访问器建立的 CIFS 挂载（平台相关）。
func (a *cifsAccessor) Offline() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.status == StatusOffline {
		return nil
	}
	return cifsOffline(a)
}
