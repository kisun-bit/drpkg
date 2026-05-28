package x2xlib

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/kisun-bit/drpkg/define"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
)

// X2XLib 表示驱动库对象。
type X2XLib struct {
	library string
}

// LinuxVirtualDriver 表示 Linux 虚拟化驱动信息。
type LinuxVirtualDriver struct {
	Id      string            `json:"id"`
	Virtual define.HPVirtType `json:"virtual"`
	Driver  string            `json:"driver"`
	Version string            `json:"version"`
	Kernels []string          `json:"kernels"`
}

// GetID 返回驱动 ID。
func (x LinuxVirtualDriver) GetID() string {
	return x.Id
}

// WindowsVirtualDriver 表示 Windows 虚拟化驱动信息。
type WindowsVirtualDriver struct {
	Id           string            `json:"id"`
	Virtual      define.HPVirtType `json:"virtual"`
	Driver       string            `json:"driver"`
	Version      string            `json:"version"`
	MinNtVersion string            `json:"minNtVersion"`
	MaxNtVersion string            `json:"maxNtVersion"`
}

// GetID 返回驱动 ID。
func (x WindowsVirtualDriver) GetID() string {
	return x.Id
}

// driverIndexer 用于泛型索引管理。
type driverIndexer interface {
	GetID() string
}

// NewX2XLib 创建驱动库实例。
func NewX2XLib(libraryDir string) (*X2XLib, error) {
	if !extend.IsExisted(libraryDir) {
		return nil, errors.Wrapf(
			os.ErrNotExist,
			"%s",
			libraryDir,
		)
	}

	return &X2XLib{
		library: libraryDir,
	}, nil
}

func (x *X2XLib) String() string {
	return fmt.Sprintf(
		"x2xlib{library=%s}",
		x.library,
	)
}

// Initialize 初始化驱动库目录结构。
func (x *X2XLib) Initialize() error {
	for _, osType := range SupportedOsTypes {

		osDir := filepath.Join(
			x.library,
			osType,
		)

		if err := ensureDir(osDir); err != nil {
			return errors.Wrapf(
				err,
				"create os dir: %s",
				osDir,
			)
		}

		for _, distro := range getSupportedDistros(osType) {

			distroDir := filepath.Join(
				osDir,
				distro,
			)

			if err := ensureDir(distroDir); err != nil {
				return errors.Wrapf(
					err,
					"create distro dir: %s",
					distroDir,
				)
			}

			for _, arch := range SupportedArchTypes {

				archDir := filepath.Join(
					distroDir,
					arch,
				)

				if err := ensureDir(archDir); err != nil {
					return errors.Wrapf(
						err,
						"create arch dir: %s",
						archDir,
					)
				}

				// 普通驱动目录
				driverStoreDir := filepath.Join(
					archDir,
					DriverStoreDirName,
				)

				if err := initIndexDir(driverStoreDir); err != nil {
					return errors.Wrapf(
						err,
						"init driver store: %s",
						driverStoreDir,
					)
				}

				// 虚拟化驱动目录
				virtualDir := filepath.Join(
					archDir,
					DriverStoreVirtualDirName,
				)

				if err := ensureDir(virtualDir); err != nil {
					return errors.Wrapf(
						err,
						"create virtual dir: %s",
						virtualDir,
					)
				}

				for _, virt := range SupportedVirtualizationTypes {

					virtDir := filepath.Join(
						virtualDir,
						string(virt),
					)

					if err := ensureDirWithGitKeep(
						virtDir,
					); err != nil {
						return errors.Wrapf(
							err,
							"create virt dir: %s",
							virtDir,
						)
					}
				}

				indexFile := filepath.Join(
					virtualDir,
					DriverStoreIndexFileName,
				)

				if err := os.WriteFile(
					indexFile,
					[]byte("[]"),
					0o644,
				); err != nil {
					return errors.Wrapf(
						err,
						"write index: %s",
						indexFile,
					)
				}
			}
		}
	}

	return nil
}

// initIndexDir 初始化带索引文件目录。
func initIndexDir(dir string) error {
	if err := ensureDir(dir); err != nil {
		return err
	}

	if err := ensureDirWithGitKeep(dir); err != nil {
		return err
	}

	indexFile := filepath.Join(
		dir,
		DriverStoreIndexFileName,
	)

	return os.WriteFile(
		indexFile,
		[]byte("[]"),
		0o644,
	)
}

// readIndex 读取索引文件。
func readIndex[T any](
	indexFile string,
) ([]*T, error) {

	data, err := os.ReadFile(indexFile)
	if err != nil {
		return nil, errors.Wrapf(
			err,
			"read index file: %s",
			indexFile,
		)
	}

	items := make([]*T, 0)

	if err = json.Unmarshal(
		data,
		&items,
	); err != nil {
		return nil, errors.Wrapf(
			err,
			"parse index file: %s",
			indexFile,
		)
	}

	return items, nil
}

// writeIndex 写入索引文件。
func writeIndex(
	indexFile string,
	v any,
) error {

	data, err := json.MarshalIndent(
		v,
		"",
		"    ",
	)
	if err != nil {
		return errors.Wrap(
			err,
			"marshal index",
		)
	}

	if err = os.WriteFile(
		indexFile,
		data,
		0o644,
	); err != nil {
		return errors.Wrapf(
			err,
			"write index: %s",
			indexFile,
		)
	}

	return nil
}

// addIndexItem 向索引文件追加记录。
func addIndexItem[T driverIndexer](
	indexFile string,
	item *T,
) error {

	if item == nil {
		return errors.New("item is nil")
	}

	items, err := readIndex[T](indexFile)
	if err != nil {
		return err
	}

	items = append(
		items,
		item,
	)

	return writeIndex(
		indexFile,
		items,
	)
}

// prepareDriverDir 创建目标驱动目录并复制驱动文件。
// 如果后续失败，会自动回滚目录。
func (x *X2XLib) prepareDriverDir(
	osType string,
	distro string,
	architecture string,
	virtual define.HPVirtType,
	driverName string,
	srcDriverDir string,
) (
	driverID string,
	dstDriverDir string,
	err error,
) {

	driverID, err = generateDriverId(driverName)
	if err != nil {
		return "", "", errors.Wrap(
			err,
			"generate driver id",
		)
	}

	dstDriverDir = filepath.Join(
		x.library,
		osType,
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
		return "", "", errors.Wrapf(
			err,
			"create driver dir: %s",
			dstDriverDir,
		)
	}

	if err = extend.CopyDir(
		srcDriverDir,
		dstDriverDir,
	); err != nil {
		return "", "", errors.Wrapf(
			err,
			"copy driver directory from %s to %s",
			srcDriverDir,
			dstDriverDir,
		)
	}

	return driverID, dstDriverDir, nil
}

// AddWindowsVirtualizationDriver 添加 Windows 虚拟化驱动。
func (x *X2XLib) AddWindowsVirtualizationDriver(
	architecture string,
	virtual define.HPVirtType,
	driverName string,
	driverVersion string,
	srcDriverDir string,
	minNTVersion string,
	maxNTVersion string,
) (
	driverID string,
	err error,
) {

	if err = checkArchitecture(architecture); err != nil {
		return "", errors.Wrapf(
			err,
			"invalid architecture: %s",
			architecture,
		)
	}

	if err = checkVirtualType(virtual); err != nil {
		return "", errors.Wrapf(
			err,
			"invalid virtualization type: %s",
			virtual,
		)
	}

	if err = checkDriverName(driverName); err != nil {
		return "", errors.Wrapf(
			err,
			"invalid driver name: %s",
			driverName,
		)
	}

	if err = checkDriverVersion(driverVersion); err != nil {
		return "", errors.Wrapf(
			err,
			"invalid driver version: %s",
			driverVersion,
		)
	}

	if err = checkDriverDir(srcDriverDir); err != nil {
		return "", errors.Wrapf(
			err,
			"invalid driver dir: %s",
			srcDriverDir,
		)
	}

	if err = checkNtVersion(minNTVersion); err != nil {
		return "", errors.Wrapf(
			err,
			"invalid min nt version: %s",
			minNTVersion,
		)
	}

	if err = checkNtVersion(maxNTVersion); err != nil {
		return "", errors.Wrapf(
			err,
			"invalid max nt version: %s",
			maxNTVersion,
		)
	}

	driverID, _, err = x.prepareDriverDir(
		define.OsWindows,
		define.DistroMicrosoft,
		architecture,
		virtual,
		driverName,
		srcDriverDir,
	)
	if err != nil {
		return "", err
	}

	wd := &WindowsVirtualDriver{
		Id:           driverID,
		Virtual:      virtual,
		Driver:       driverName,
		Version:      driverVersion,
		MinNtVersion: minNTVersion,
		MaxNtVersion: maxNTVersion,
	}

	indexFile, err := x.getVirtualizationDriverIndex(
		define.OsWindows,
		define.DistroMicrosoft,
		architecture,
	)
	if err != nil {
		return "", err
	}

	if err = addIndexItem(
		indexFile,
		wd,
	); err != nil {
		return "", err
	}

	return driverID, nil
}

// AddLinuxVirtualizationDriver 添加 Linux 虚拟化驱动。
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
		return "", errors.Wrapf(
			err,
			"invalid distro: %s",
			distro,
		)
	}

	if err = checkArchitecture(architecture); err != nil {
		return "", errors.Wrapf(
			err,
			"invalid architecture: %s",
			architecture,
		)
	}

	if err = checkVirtualType(virtual); err != nil {
		return "", errors.Wrapf(
			err,
			"invalid virtualization type: %s",
			virtual,
		)
	}

	if err = checkDriverName(driverName); err != nil {
		return "", errors.Wrapf(
			err,
			"invalid driver name: %s",
			driverName,
		)
	}

	if err = checkDriverVersion(driverVersion); err != nil {
		return "", errors.Wrapf(
			err,
			"invalid driver version: %s",
			driverVersion,
		)
	}

	if err = checkDriverDir(srcDriverDir); err != nil {
		return "", errors.Wrapf(
			err,
			"invalid driver dir: %s",
			srcDriverDir,
		)
	}

	compatibleKernels, err = checkAndFixStrings(
		compatibleKernels,
	)
	if err != nil {
		return "", errors.Wrap(
			err,
			"invalid compatible kernels",
		)
	}

	driverID, _, err = x.prepareDriverDir(
		define.OsLinux,
		distro,
		architecture,
		virtual,
		driverName,
		srcDriverDir,
	)
	if err != nil {
		return "", err
	}

	ld := &LinuxVirtualDriver{
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
		return "", err
	}

	if err = addIndexItem(
		indexFile,
		ld,
	); err != nil {
		return "", err
	}

	return driverID, nil
}

// GetLinuxVirtualizationDriver 获取匹配的 Linux 虚拟化驱动目录。
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
		return nil, err
	}

	if err = checkArchitecture(architecture); err != nil {
		return nil, err
	}

	if err = checkVirtualType(virtualType); err != nil {
		return nil, err
	}

	if kernel == "" {
		return nil, errors.New(
			"kernel is required",
		)
	}

	indexFile, err := x.getVirtualizationDriverIndex(
		define.OsLinux,
		distro,
		architecture,
	)
	if err != nil {
		return nil, err
	}

	lvds, err := readIndex[LinuxVirtualDriver](indexFile)
	if err != nil {
		return nil, err
	}

	for _, lvd := range lvds {

		if lvd.Virtual != virtualType {
			continue
		}

		// 修复 kernel 未过滤 bug
		if !funk.ContainsString(
			lvd.Kernels,
			kernel,
		) {
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
				"get driver package: %s",
				lvd.Id,
			)
		}

		driverDirs = append(
			driverDirs,
			driverDir,
		)
	}

	driverDirs = funk.UniqString(
		driverDirs,
	)

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

// GetWindowsVirtualizationDriver 获取匹配的 Windows 虚拟化驱动目录。
func (x *X2XLib) GetWindowsVirtualizationDriver(
	architecture string,
	virtualType define.HPVirtType,
	ntVersion string,
) (
	driverDirs []string,
	err error,
) {

	if err = checkArchitecture(architecture); err != nil {
		return nil, err
	}

	if err = checkVirtualType(virtualType); err != nil {
		return nil, err
	}

	if err = checkNtVersion(ntVersion); err != nil {
		return nil, err
	}

	indexFile, err := x.getVirtualizationDriverIndex(
		define.OsLinux,
		define.DistroMicrosoft,
		architecture,
	)
	if err != nil {
		return nil, err
	}

	wvds, err := readIndex[WindowsVirtualDriver](indexFile)
	if err != nil {
		return nil, err
	}

	curVer := extend.ParseVersion(ntVersion)
	for _, wvd := range wvds {
		if wvd.Virtual != virtualType {
			continue
		}
		minVer := extend.ParseVersion(wvd.MinNtVersion)
		maxVer := extend.ParseVersion(wvd.MaxNtVersion)

		if !(curVer.GreaterOrEqual(minVer) && curVer.LessOrEqual(maxVer)) {
			continue
		}

		// 已找到兼容的驱动
		driverDir, e := x.getVirtualizationDriverPackage(
			define.OsLinux,
			define.DistroMicrosoft,
			architecture,
			virtualType,
			wvd.Id,
		)
		if e != nil {
			return nil, errors.Wrapf(
				e,
				"get driver package: %s",
				wvd.Id,
			)
		}

		driverDirs = append(driverDirs, driverDir)
	}

	driverDirs = funk.UniqString(
		driverDirs,
	)

	if len(driverDirs) == 0 {
		return nil, errors.Errorf(
			"virtualization driver not found: distro=%s arch=%s virt=%s ntversion=%s",
			define.DistroMicrosoft,
			architecture,
			virtualType,
			ntVersion,
		)
	}

	return driverDirs, nil
}

func (x *X2XLib) RemoveVirtualizationDriver(
	osType string,
	distro string,
	architecture string,
	driverIds []string,
) (
	err error,
) {

	if len(driverIds) == 0 {
		return nil
	}

	if err = checkDistro(distro); err != nil {
		return err
	}

	if err = checkArchitecture(architecture); err != nil {
		return err
	}

	indexFile := filepath.Join(
		x.library,
		osType,
		distro,
		architecture,
		DriverStoreVirtualDirName,
		DriverStoreIndexFileName,
	)

	switch osType {
	case define.OsWindows:
		return x.removeWindowsVirtualizationDriver(indexFile, driverIds)
	case define.OsLinux:
		return x.removeLinuxVirtualizationDriver(indexFile, driverIds)
	}

	return nil
}

// getVirtualizationDriverIndex 获取虚拟化驱动索引文件路径。
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

// getVirtualizationDriverPackage 获取驱动目录。
func (x *X2XLib) getVirtualizationDriverPackage(
	osType string,
	distro string,
	architecture string,
	virtualType define.HPVirtType,
	driverID string,
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
		driverID,
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

// listLinuxVirtualizationDriver 获取 Linux 虚拟化驱动列表。
func (x *X2XLib) listLinuxVirtualizationDriver(
	indexFile string,
) (
	[]*LinuxVirtualDriver,
	error,
) {
	return readIndex[LinuxVirtualDriver](indexFile)
}

// listWindowsVirtualizationDriver 获取 Windows 虚拟化驱动列表。
func (x *X2XLib) listWindowsVirtualizationDriver(
	indexFile string,
) (
	[]*WindowsVirtualDriver,
	error,
) {
	return readIndex[WindowsVirtualDriver](indexFile)
}

// removeIndexItems 删除索引项。
func removeIndexItems[T driverIndexer](
	indexFile string,
	driverIDs []string,
) error {

	if len(driverIDs) == 0 {
		return nil
	}

	items, err := readIndex[T](
		indexFile,
	)
	if err != nil {
		return err
	}

	newItems := make(
		[]*T,
		0,
		len(items),
	)

	for _, item := range items {

		drvId := (*item).GetID()

		if funk.ContainsString(
			driverIDs,
			drvId,
		) {
			archDir := filepath.Dir(filepath.Dir(indexFile))
			_ = filepath.Walk(archDir, func(path string, info fs.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if extend.IsNilType(info) {
					return nil
				}
				if info.IsDir() && filepath.Base(path) == drvId {
					_ = os.RemoveAll(path)
					return filepath.SkipAll
				}
				return nil
			})
			continue
		}

		newItems = append(
			newItems,
			item,
		)
	}

	if len(newItems) == len(items) {
		return nil
	}

	return writeIndex(
		indexFile,
		newItems,
	)
}

// removeLinuxVirtualizationDriver 删除 Linux 虚拟化驱动。
func (x *X2XLib) removeLinuxVirtualizationDriver(
	indexFile string,
	driverIDs []string,
) error {
	return removeIndexItems[LinuxVirtualDriver](
		indexFile,
		driverIDs,
	)
}

// removeWindowsVirtualizationDriver 删除 Windows 虚拟化驱动。
func (x *X2XLib) removeWindowsVirtualizationDriver(
	indexFile string,
	driverIDs []string,
) error {
	return removeIndexItems[WindowsVirtualDriver](
		indexFile,
		driverIDs,
	)
}
