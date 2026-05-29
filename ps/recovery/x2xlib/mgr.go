package x2xlib

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kisun-bit/drpkg/define"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/pkg/errors"
)

// X2XLib 表示驱动库对象。
type X2XLib struct {
	library                string
	virtualDrvDBIndexPath  string // 虚拟化驱动库索引文件
	hardwareDrvDBIndexPath string // 普通主设硬件的驱动索引文件
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
		library:                libraryDir,
		virtualDrvDBIndexPath:  filepath.Join(libraryDir, DriverStoreVirtualDirName, DriverStoreIndexFileName),
		hardwareDrvDBIndexPath: filepath.Join(libraryDir, DriverStoreDirName, DriverStoreIndexFileName),
	}, nil
}

func (x *X2XLib) String() string {
	return fmt.Sprintf(
		"x2xlib{library=%s}",
		x.library,
	)
}

// Destroy 销毁驱动库。
func (x *X2XLib) Destroy() error {
	if err := os.RemoveAll(filepath.Dir(x.virtualDrvDBIndexPath)); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Dir(x.hardwareDrvDBIndexPath)); err != nil {
		return err
	}
	return nil
}

// Initialize 初始化驱动库目录结构。
func (x *X2XLib) Initialize() error {

	//
	// 普通驱动
	//

	if err := initIndexFile(x.virtualDrvDBIndexPath); err != nil {
		return errors.Wrapf(
			err,
			"init index file: %s",
			x.virtualDrvDBIndexPath,
		)
	}

	//
	// 虚拟化驱动
	//

	if err := initIndexFile(x.virtualDrvDBIndexPath); err != nil {
		return errors.Wrapf(
			err,
			"init index file: %s",
			x.virtualDrvDBIndexPath,
		)
	}

	return nil
}

// AddWindowsVDL 添加 Windows 虚拟化驱动。
func (x *X2XLib) AddWindowsVDL(
	name string,
	version string,
	remark string,
	virtual define.HPVirtType,
	vendor string,
	architecture string,
	signType string,
	signedByMs bool,
	minCompatibleNT string,
	maxCompatibleNT string,
	sourceDir string,
) (
	driverID string,
	driverDir string,
	err error,
) {

	defer func() {
		if err != nil {
			err = errors.Wrapf(err, "AddWindowsVDL: "+
				"name=%s, ver=%s, virtual=%s, arch=%s, sign=%s, signByMs=%v, sourceDir=%s",
				name,
				version,
				virtual,
				architecture,
				signType,
				signedByMs,
				sourceDir)
		}
	}()

	if signType == "" {
		signType = define.MsSignNone
	}

	vd, err := newWindowsVDL(
		name,
		version,
		remark,
		virtual,
		vendor,
		architecture,
		signType,
		signedByMs,
		minCompatibleNT,
		maxCompatibleNT,
	)
	if err != nil {
		return "", "", err
	}

	driverDir, err = x.addVDLWithFiles(vd, sourceDir)
	if err != nil {
		return "", "", err
	}

	return vd.Id, driverDir, nil
}

// AddLinuxVDL 添加 Linux 虚拟化驱动。
func (x *X2XLib) AddLinuxVDL(
	name string,
	version string,
	remark string,
	virtual define.HPVirtType,
	vendor string,
	architecture string,
	distro string,
	compatibleKernels []string,
	sourceDir string,
) (
	driverID string,
	driverDir string,
	err error,
) {

	defer func() {
		if err != nil {
			err = errors.Wrapf(err, "AddLinuxVDL: "+
				"name=%s, ver=%s, virtual=%s, arch=%s, distro=%s, kernels=%v, sourceDir=%s",
				name,
				version,
				virtual,
				architecture,
				distro,
				compatibleKernels,
				sourceDir)
		}
	}()

	vd, err := newLinuxVDL(
		name,
		version,
		remark,
		virtual,
		vendor,
		architecture,
		distro,
		compatibleKernels,
	)
	if err != nil {
		return "", "", err
	}

	driverDir, err = x.addVDLWithFiles(vd, sourceDir)
	if err != nil {
		return "", "", err
	}

	return vd.Id, driverDir, nil
}

// GetWindowsCompatibleVDL 获取 Windows 兼容的虚拟化驱动库。
func (x *X2XLib) GetWindowsCompatibleVDL(
	virtual define.HPVirtType,
	architecture string,
	ntVersion string,
	mustSignType string, // 可选参数
	mustSignedByMs bool, // 可选参数
) (
	driverFriendly string,
	driverDir string,
	err error,
) {
	defer func() {
		if err != nil {
			err = errors.Wrapf(err, "GetWindowsCompatibleVDL: "+
				"virtual=%s, arch=%s, nt=%s, sign=%s, signByMs=%v",
				virtual,
				architecture,
				ntVersion,
				mustSignType,
				mustSignedByMs)
		}
	}()

	vdls, err := listVDL(x.virtualDrvDBIndexPath)
	if err != nil {
		return "", "", err
	}

	for _, vd := range vdls {
		if vd.isWindowsCompatible(virtual, architecture, ntVersion, mustSignType, mustSignedByMs) {
			return vd.String(), filepath.Join(x.library, vd.fileRepoDir()), nil
		}
	}

	// TODO 按微软签名、版本号、兼容版本排序，优先使用最合适的驱动库进行注入

	return "", "", os.ErrNotExist
}

// GetLinuxCompatibleVDL 获取 Linux 兼容的虚拟化驱动库。
func (x *X2XLib) GetLinuxCompatibleVDL(
	virtual define.HPVirtType,
	architecture string,
	distro string,
	kernel string,
	mustVendor string, // 可选参数
) (
	driverFriendly string,
	driverDir string,
	err error,
) {

	defer func() {
		if err != nil {
			err = errors.Wrapf(err, "GetLinuxCompatibleVDL: "+
				"distro=%s, arch=%s, virtual=%s, kernel=%s, vendor=%s",
				distro,
				architecture,
				virtual,
				kernel,
				mustVendor)
		}
	}()

	vdls, err := listVDL(x.virtualDrvDBIndexPath)
	if err != nil {
		return "", "", err
	}

	for _, vd := range vdls {
		if vd.isLinuxCompatible(virtual, architecture, distro, kernel, mustVendor) {
			return vd.String(), filepath.Join(x.library, vd.fileRepoDir()), nil
		}
	}

	// TODO 按版本号排序（由高置低），优先使用最合适的驱动库进行注入

	return "", "", os.ErrNotExist
}

// GetVDL 获取指定的虚拟化驱动库
func (x *X2XLib) GetVDL(
	driverID string,
) (
	driverDir string,
	err error,
) {
	defer func() {
		if err != nil {
			err = errors.Wrapf(err, "GetVDL: driverID=%s", driverID)
		}
	}()

	vdls, err := listVDL(x.virtualDrvDBIndexPath)
	if err != nil {
		return "", err
	}

	for _, vd := range vdls {
		if vd.Id == driverID {
			return filepath.Join(x.library, vd.fileRepoDir()), nil
		}
	}
	return "", os.ErrNotExist
}

// DeleteVDL 删除指定的虚拟化驱动库
func (x *X2XLib) DeleteVDL(
	driverID string,
) (
	err error,
) {
	defer func() {
		if err != nil {
			err = errors.Wrapf(err, "RemoveVDL: driverID=%s", driverID)
		}
	}()

	return delVDL(x.virtualDrvDBIndexPath, driverID)
}

func (x *X2XLib) addVDLWithFiles(vd *vdl, sourceDir string) (dstDir string, err error) {
	dstDir = filepath.Join(x.library, vd.fileRepoDir())
	if err = extend.CopyDir(sourceDir, dstDir); err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dstDir)
		}
	}()

	if err = addVDL(x.virtualDrvDBIndexPath, vd); err != nil {
		return "", err
	}
	return dstDir, nil
}
