package x2xlib

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/kisun-bit/drpkg/define"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
)

func checkDistro(distro string) error {
	if distro == "" {
		return errors.New("distro is required")
	}
	if !funk.InStrings(SupportedDistroTypes, distro) {
		return errors.Errorf("unsupported distro(`%s`)", distro)
	}
	return nil
}

func checkArchitecture(architecture string) error {
	if architecture == "" {
		return errors.New("architecture is required")
	}
	if !funk.InStrings(SupportedArchTypes, architecture) {
		return errors.Errorf("unsupported architecture(`%s`)", architecture)
	}
	return nil
}

func checkVirtualType(virt define.HPVirtType) error {
	if virt == "" {
		return errors.New("virtual type is required")
	}
	for _, h := range SupportedVirtualizationTypes {
		if virt == h {
			return nil
		}
	}
	return errors.Errorf("unsupported virtual type `%s`", virt)
}

func checkDriverName(driverName string) error {
	if driverName == "" {
		return errors.New("driver name is required")
	}
	return nil
}

func checkDriverVersion(version string) error {
	if version == "" {
		return errors.New("driver version is required")
	}
	return nil
}

func checkDriverDir(driverDir string) error {
	if driverDir == "" {
		return errors.New("driver directory is required")
	}
	if !extend.IsDir(driverDir) {
		return errors.Errorf("`%s` is not a directory", driverDir)
	}
	return nil
}

func checkAndFixStrings(strArr []string) ([]string, error) {
	if len(strArr) == 0 {
		return nil, errors.New("strings is required")
	}
	ret := make([]string, 0)
	for _, str := range strArr {
		if str = strings.TrimSpace(str); str != "" {
			ret = append(ret, str)
			continue
		}
	}
	if len(ret) == 0 {
		return nil, errors.New("strings is required")
	}
	return ret, nil
}

func getSupportedDistros(osType string) []string {
	switch osType {
	case define.OsWindows:
		return []string{
			define.DistroMicrosoft,
		}

	case define.OsLinux:
		distros := make(
			[]string,
			0,
			len(SupportedDistroTypes),
		)

		for _, distro := range SupportedDistroTypes {
			if distro == define.DistroMicrosoft {
				continue
			}

			distros = append(distros, distro)
		}

		return distros
	}

	return nil
}

func ensureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

func ensureDirWithGitKeep(path string) error {
	if err := ensureDir(path); err != nil {
		return err
	}

	gitKeep := filepath.Join(path, ".gitkeep")

	f, err := os.Create(gitKeep)
	if err != nil {
		return err
	}

	return f.Close()
}

func generateDriverId(driverName string) (string, error) {
	if err := checkDriverName(driverName); err != nil {
		return "", err
	}
	u, e := uuid.NewUUID()
	if e != nil {
		return "", e
	}
	return fmt.Sprintf(
		"%s.%s",
		driverName,
		strings.ReplaceAll(u.String(), "-", "")), nil
}
