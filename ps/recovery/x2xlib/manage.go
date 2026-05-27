package x2xlib

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kisun-bit/drpkg/extend"
	"github.com/pkg/errors"
)

type X2XLib struct {
	library string
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

func (x2xLib *X2XLib) String() string {
	return fmt.Sprintf("x2XLib{library: %s}", x2xLib.library)
}

// GetLinuxVirtualPackage 获取Linux指定虚拟化相关的安装包
func (x2xLib *X2XLib) GetLinuxVirtualPackage(distro, arch, kernel, virtType string) (dir string, err error) {
	dir = filepath.Join(x2xLib.library, "linux", distro, arch, kernel, "virtual", virtType)
	if !extend.IsExisted(dir) {
		return "", errors.Wrapf(os.ErrNotExist, "%s", dir)
	}
	if !extend.IsDir(dir) {
		return "", errors.Errorf("%s is not a directory", dir)
	}
	return dir, nil
}
