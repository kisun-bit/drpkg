package x2xcore

import (
	"regexp"

	"github.com/kisun-bit/drpkg/define"
	"github.com/pkg/errors"
)

// 支持挂载的文件系统类型
var SupportedFsTypes = []string{
	// Linux 主流
	define.FsTypeExt4,
	define.FsTypeExt3,
	define.FsTypeExt2,
	define.FsTypeXFS,
	define.FsTypeBtrfs,

	// FAT / Windows
	define.FsTypeFAT,
	define.FsTypeVFAT,
	define.FsTypeMSDOS,
	define.FsTypeNTFS,

	// 特殊/集群
	define.FsTypeCramFS,
	define.FsTypeGFS2,

	// Apple
	define.FsTypeHFS,
	define.FsTypeHFSPlus,

	// Unix-like
	define.FsTypeZFS,
	define.FsTypeJFS,
	define.FsTypeMinix,
	define.FsTypeReiserFS,
}

// 默认的离线系统挂载点
var (
	rootDir = "/mnt/sysroot"
)

// 正则匹配相关
var (
	reBlkidType = regexp.MustCompile(`TYPE="([^"]+)"`)
	reBlkidUuid = regexp.MustCompile(`UUID="([^"]+)"`)
)

// 错误相关
var (
	ErrorRootEnvNotMounted = errors.New("root environment is not mounted")
)

type NetworkBackend string

const (
	BackendUnknown    NetworkBackend = "unknown"
	BackendIfcfg      NetworkBackend = "rhel-ifcfg"        // RHEL ifcfg
	BackendInterfaces NetworkBackend = "debian-interfaces" // Debian interfaces
	BackendNetplan    NetworkBackend = "ubuntu-netplan"    // Ubuntu netplan
	BackendWicked     NetworkBackend = "suse-wicked"       // SUSE wicked
	BackendNMKeyfile  NetworkBackend = "network-manager"   // NetworkManager
)
