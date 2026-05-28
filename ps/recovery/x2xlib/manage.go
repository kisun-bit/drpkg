package x2xlib

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kisun-bit/drpkg/define"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
)

type X2XLib struct {
	library string
}

type LinuxVirtualDriver struct {
	Id      string            `json:"id"`
	Virtual define.HPVirtType `json:"virtual"`
	Driver  string            `json:"driver"`
	Version string            `json:"version"`
	Kernels []string          `json:"kernels"`
}

func NewX2XLib(libraryDir string) (*X2XLib, error) {
	if !extend.IsExisted(libraryDir) {
		return nil, errors.Wrapf(os.ErrNotExist, "%s", libraryDir)
	}

	x2xLib := &X2XLib{
		library: libraryDir,
	}

	return x2xLib, nil
}

func (x *X2XLib) String() string {
	return fmt.Sprintf("x2XLib{library: %s}", x.library)
}

// Initialize 初始化驱动库
func (x *X2XLib) Initialize() error {
	for _, osType := range SupportedOsTypes {
		osDir := filepath.Join(x.library, osType)

		if err := ensureDir(osDir); err != nil {
			return errors.Wrapf(err, "create os dir: %s", osDir)
		}

		for _, distro := range getSupportedDistros(osType) {
			distroDir := filepath.Join(osDir, distro)

			if err := ensureDir(distroDir); err != nil {
				return errors.Wrapf(
					err, distroDir,
				)
			}

			for _, arch := range SupportedArchTypes {
				archDir := filepath.Join(distroDir, arch)

				if err := ensureDir(archDir); err != nil {
					return errors.Wrapf(
						err, archDir,
					)
				}

				//
				// 普通驱动
				//

				driverStoreDir := filepath.Join(archDir, DriverStoreDirName)
				if err := ensureDir(driverStoreDir); err != nil {
					return errors.Wrapf(
						err, driverStoreDir)
				}

				if err := ensureDirWithGitKeep(driverStoreDir); err != nil {
					return errors.Wrapf(
						err, driverStoreDir,
					)
				}
				driverIndexFile := filepath.Join(driverStoreDir, DriverStoreIndexFileName)
				if err := os.WriteFile(driverIndexFile, []byte("[]"), 0o644); err != nil {
					return errors.Wrapf(err, driverIndexFile)
				}

				//
				// 虚拟化驱动
				//

				virtualDriverDir := filepath.Join(archDir, DriverStoreVirtualDirName)
				if err := ensureDir(virtualDriverDir); err != nil {
					return errors.Wrapf(
						err, virtualDriverDir)
				}

				for _, virt := range SupportedVirtualizationTypes {
					virtDir := filepath.Join(
						virtualDriverDir,
						string(virt),
					)

					if err := ensureDirWithGitKeep(virtDir); err != nil {
						return errors.Wrapf(
							err, virtDir,
						)
					}
				}

				virtualIndexFile := filepath.Join(virtualDriverDir, DriverStoreIndexFileName)
				if err := os.WriteFile(virtualIndexFile, []byte("[]"), 0o644); err != nil {
					return errors.Wrapf(err, virtualIndexFile)
				}
			}
		}
	}

	return nil
}

//func (x *X2XLib) AddWindowsPCIDriver(
//	architecture string, // amd64/386/arm64/loong64
//	deviceClass uint32, // storage / network
//	driverName string, // 如 lsi_scsi、pvscsi、netkvm
//	driverDir string, // 驱动文件目录
//	minWindowsNTVersion string, // 最低兼容NT版本
//	maxWindowsNTVersion string, // 最高兼容NT版本
//	hardwareIDs []string, // 支持的硬件ID（PCI\VEN_xxx）
//) (driverID string, err error) {
//	return "", nil
//}

// AddLinuxVirtualizationDriver 添加 Linux 虚拟化驱动到驱动库。
// 驱动目录会被复制到：
// {library}/linux/{distro}/{arch}/virtual/{virtType}/{driverId}
//
// 参数：
//   - distro: Linux 发行版（如 centos7 / ubuntu2204）
//   - architecture: CPU 架构（如 amd64 / arm64）
//   - virtual: 虚拟化类型（kvm / vmware / xen / hyper-v）
//   - driverName: 驱动名称
//   - driverVersion: 驱动版本
//   - srcDriverDir: 源驱动目录
//   - compatibleKernels: 兼容的内核版本列表
//
// 返回：
//   - driverID: 生成的驱动唯一 ID
func (x *X2XLib) AddLinuxVirtualizationDriver(
	distro string,
	architecture string,
	virtual define.HPVirtType,
	driverName string,
	driverVersion string,
	srcDriverDir string,
	compatibleKernels []string,
) (
	driverID string,
	err error,
) {

	if err = checkDistro(distro); err != nil {
		return "", errors.Wrapf(err, "invalid distro: %s", distro)
	}
	if err = checkArchitecture(architecture); err != nil {
		return "", errors.Wrapf(err, "invalid architecture: %s", architecture)
	}
	if err = checkVirtualType(virtual); err != nil {
		return "", errors.Wrapf(err, "invalid virtualization type: %s", virtual)
	}
	if err = checkDriverName(driverName); err != nil {
		return "", errors.Wrapf(err, "invalid driver name: %s", driverName)
	}
	if err = checkDriverVersion(driverVersion); err != nil {
		return "", errors.Wrapf(err, "invalid driver version: %s", driverVersion)
	}
	if err = checkDriverDir(srcDriverDir); err != nil {
		return "", errors.Wrapf(err, "invalid driver directory: %s", srcDriverDir)
	}

	compatibleKernels, err = checkAndFixStrings(compatibleKernels)
	if err != nil {
		return "", errors.Wrap(err, "invalid compatible kernels")
	}

	driverID, err = generateDriverId(driverName)
	if err != nil {
		return "", errors.Wrap(err, "generate driver id")
	}

	dstDriverDir := filepath.Join(
		x.library,
		define.OsLinux,
		distro,
		architecture,
		DriverStoreVirtualDirName,
		string(virtual),
		driverID,
	)

	defer func() {
		if err != nil {
			_ = os.RemoveAll(dstDriverDir)
		}
	}()

	if err = ensureDir(dstDriverDir); err != nil {
		return "", errors.Wrapf(err, "create driver directory: %s", dstDriverDir)
	}

	if err = extend.CopyDir(srcDriverDir, dstDriverDir); err != nil {
		return "", errors.Wrapf(
			err,
			"copy driver directory from %s to %s",
			srcDriverDir,
			dstDriverDir,
		)
	}

	ld := LinuxVirtualDriver{
		Id:      driverID,
		Virtual: virtual,
		Driver:  driverName,
		Version: driverVersion,
		Kernels: compatibleKernels,
	}

	indexFile, err := x.getVirtualizationDriverIndex(
		define.OsLinux,
		distro,
		architecture,
	)
	if err != nil {
		return "", errors.Wrap(err, "get virtualization driver index")
	}

	if err = x.addLinuxVirtualizationDriver(indexFile, &ld); err != nil {
		return "", errors.Wrap(err, "add virtualization driver to index")
	}

	return driverID, nil
}

// GetLinuxVirtualizationDriver 获取匹配的 Linux 虚拟化驱动目录。
// 根据发行版、架构、虚拟化类型及内核版本查找驱动。
//
// 返回值为驱动目录列表（去重后）。
func (x *X2XLib) GetLinuxVirtualizationDriver(
	distro string,
	architecture string,
	virtualType define.HPVirtType,
	kernel string,
) (
	driverDirs []string,
	err error,
) {

	if err = checkDistro(distro); err != nil {
		return nil, errors.Wrapf(err, "invalid distro: %s", distro)
	}
	if err = checkArchitecture(architecture); err != nil {
		return nil, errors.Wrapf(err, "invalid architecture: %s", architecture)
	}
	if err = checkVirtualType(virtualType); err != nil {
		return nil, errors.Wrapf(err, "invalid virtualization type: %s", virtualType)
	}
	if kernel == "" {
		return nil, errors.New("kernel is required")
	}

	indexFile, err := x.getVirtualizationDriverIndex(
		define.OsLinux,
		distro,
		architecture,
	)
	if err != nil {
		return nil, errors.Wrap(err, "get virtualization driver index")
	}

	lvds, err := x.listLinuxVirtualizationDriver(indexFile)
	if err != nil {
		return nil, errors.Wrap(err, "list virtualization drivers")
	}

	for _, lvd := range lvds {
		if lvd.Virtual != virtualType {
			continue
		}

		driverDir, e := x.getVirtualizationDriverPackage(
			define.OsLinux,
			distro,
			architecture,
			virtualType,
			lvd.Id,
		)
		if e != nil {
			return nil, errors.Wrapf(
				e,
				"get driver package: driverId=%s",
				lvd.Id,
			)
		}

		driverDirs = append(driverDirs, driverDir)
	}

	driverDirs = funk.UniqString(driverDirs)

	if len(driverDirs) == 0 {
		return nil, errors.Errorf(
			"virtualization driver not found: distro=%s arch=%s virt=%s kernel=%s",
			distro,
			architecture,
			virtualType,
			kernel,
		)
	}

	return driverDirs, nil
}

// getVirtualizationDriverIndex 获取虚拟化驱动索引文件路径。
// 索引文件不存在时返回 os.ErrNotExist。
func (x *X2XLib) getVirtualizationDriverIndex(
	osType string,
	distro string,
	architecture string,
) (
	indexFile string,
	err error,
) {

	indexFile = filepath.Join(
		x.library,
		osType,
		distro,
		architecture,
		DriverStoreVirtualDirName,
		DriverStoreIndexFileName,
	)

	if !extend.IsExisted(indexFile) {
		return "", errors.Wrapf(
			os.ErrNotExist,
			"virtualization driver index not found: %s",
			indexFile,
		)
	}

	return indexFile, nil
}

// getVirtualizationDriverPackage 获取指定虚拟化驱动目录。
// 驱动目录不存在时返回 os.ErrNotExist。
func (x *X2XLib) getVirtualizationDriverPackage(
	osType string,
	distro string,
	architecture string,
	virtualType define.HPVirtType,
	driverId string,
) (
	driverDir string,
	err error,
) {

	driverDir = filepath.Join(
		x.library,
		osType,
		distro,
		architecture,
		DriverStoreVirtualDirName,
		string(virtualType),
		driverId,
	)

	if !extend.IsExisted(driverDir) {
		return "", errors.Wrapf(
			os.ErrNotExist,
			"virtualization driver package not found: %s",
			driverDir,
		)
	}

	return driverDir, nil
}

// listLinuxVirtualizationDriver 从索引文件读取并解析虚拟化驱动列表。
func (x *X2XLib) listLinuxVirtualizationDriver(
	indexFile string,
) (
	lvds []*LinuxVirtualDriver,
	err error,
) {

	data, err := os.ReadFile(indexFile)
	if err != nil {
		return nil, errors.Wrapf(err, "read index file: %s", indexFile)
	}

	lvds = make([]*LinuxVirtualDriver, 0)

	if err = json.Unmarshal(data, &lvds); err != nil {
		return nil, errors.Wrapf(
			err,
			"parse virtualization driver index: %s",
			indexFile,
		)
	}

	return lvds, nil
}

// addLinuxVirtualizationDriver 向索引文件追加虚拟化驱动信息。
func (x *X2XLib) addLinuxVirtualizationDriver(
	indexFile string,
	ld *LinuxVirtualDriver,
) (
	err error,
) {

	if ld == nil {
		return errors.New("linux virtual driver is nil")
	}

	lvds, err := x.listLinuxVirtualizationDriver(indexFile)
	if err != nil {
		return err
	}

	lvds = append(lvds, ld)

	data, err := json.MarshalIndent(lvds, "", "    ")
	if err != nil {
		return errors.Wrap(err, "marshal virtualization driver index")
	}

	if err = os.WriteFile(indexFile, data, 0644); err != nil {
		return errors.Wrapf(
			err,
			"write virtualization driver index: %s",
			indexFile,
		)
	}

	return nil
}

// removeLinuxVirtualizationDriver 从索引文件中移除指定虚拟化驱动。
func (x *X2XLib) removeLinuxVirtualizationDriver(
	indexFile string,
	driverIds []string,
) (
	err error,
) {

	if len(driverIds) == 0 {
		return nil
	}

	lvds, err := x.listLinuxVirtualizationDriver(indexFile)
	if err != nil {
		return err
	}

	lvdsNew := make([]*LinuxVirtualDriver, 0)

	for _, lvd := range lvds {
		ignore := false

		for _, driverId := range driverIds {
			if lvd.Id == driverId {
				ignore = true
				break
			}
		}

		if ignore {
			continue
		}

		lvdsNew = append(lvdsNew, lvd)
	}

	if len(lvdsNew) == len(lvds) {
		return nil
	}

	data, err := json.MarshalIndent(lvdsNew, "", "    ")
	if err != nil {
		return errors.Wrap(err, "marshal virtualization driver index")
	}

	if err = os.WriteFile(indexFile, data, 0644); err != nil {
		return errors.Wrapf(
			err,
			"write virtualization driver index: %s",
			indexFile,
		)
	}

	return nil
}
