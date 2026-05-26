package x2xlib

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

// GetLinuxPackage 获取Linux指定的安装包
func (x2xLib *X2XLib) GetLinuxPackage(distro, arch, kernel, packageName string) (packages []string, err error) {
	packageAlias := filepath.Join(x2xLib.library, "linux", distro, arch, kernel, "package.alias")
	packageAliasContent, err := os.ReadFile(packageAlias)
	if err != nil {
		return nil, errors.Wrapf(err, "parse %s", packageAlias)
	}

	s := bufio.NewScanner(bytes.NewReader(packageAliasContent))
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		lineItems := strings.Split(line, ":")
		if len(lineItems) != 2 {
			continue
		}
		curPackageName, curPackageFiles := strings.TrimSpace(lineItems[0]), strings.TrimSpace(lineItems[1])
		if curPackageName != packageName {
			continue
		}
		for _, f := range strings.Split(curPackageFiles, ",") {
			f = strings.TrimSpace(f)
			if f == "" {
				continue
			}
			f = filepath.Join(filepath.Dir(packageAlias), "packages", f)
			packages = append(packages, f)
		}
		break
	}

	if len(packages) == 0 {
		return nil, errors.Errorf("package %s not found", packageName)
	}

	return packages, nil
}
