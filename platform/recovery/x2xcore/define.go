package x2xcore

import (
	"fmt"
	"regexp"

	"github.com/kisun-bit/drpkg/defs"
	"github.com/pkg/errors"
)

// 支持挂载的文件系统类型
var SupportedFsTypes = []string{
	// Linux 主流
	defs.FsTypeExt4,
	defs.FsTypeExt3,
	defs.FsTypeExt2,
	defs.FsTypeXFS,
	defs.FsTypeBtrfs,

	// FAT / Windows
	defs.FsTypeFAT,
	defs.FsTypeVFAT,
	defs.FsTypeMSDOS,
	defs.FsTypeNTFS,

	// 特殊/集群
	defs.FsTypeCramFS,
	defs.FsTypeGFS2,

	// Apple
	defs.FsTypeHFS,
	defs.FsTypeHFSPlus,

	// Unix-like
	defs.FsTypeZFS,
	defs.FsTypeJFS,
	defs.FsTypeMinix,
	defs.FsTypeReiserFS,
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
	ErrDeviceNotSupported  = errors.New("device is not supported")
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

var (
	FirstBootProcFilePrefix            = "drfirstboot.h0nk1"
	FirstBootProcCompletedFlagFilename = fmt.Sprintf("%s.completed", FirstBootProcFilePrefix)
	FirstBootProcPowershellScriptName  = fmt.Sprintf("%s.fb.ps", FirstBootProcFilePrefix)
	FirstBootProcBatScriptName         = fmt.Sprintf("%s.fb.bat", FirstBootProcFilePrefix)
	FirstBootProcShellScriptName       = fmt.Sprintf("%s.fb.sh", FirstBootProcFilePrefix)
	FirstBootProcNetworkConfigFileName = fmt.Sprintf("%s.fb.networkconfig.json", FirstBootProcFilePrefix)
)
