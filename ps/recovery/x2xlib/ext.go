package x2xlib

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

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

func checkOsType(os_ string) error {
	if os_ != define.OsWindows && os_ != define.OsLinux {
		return errors.Errorf("unsupported os `%s`", os_)
	}
	return nil
}

func checkMsSignAlgo(signAlgo string) error {
	if signAlgo == "" {
		return errors.New("signature algorithm is required")
	}
	if signAlgo != define.MsSignDual && signAlgo != define.MsSignSha256 && signAlgo != define.MsSignSha1 {
		return errors.Errorf("signature algorithm `%s` is not supported", signAlgo)
	}
	return nil
}

var supportedNtVersions = []string{
	"5.1",  // XP
	"5.2",  // Server 2003 / XP x64
	"6.0",  // Vista / Server 2008
	"6.1",  // Windows 7 / Server 2008 R2
	"6.2",  // Windows 8 / Server 2012
	"6.3",  // Windows 8.1 / Server 2012 R2
	"10.0", // Windows 10/11 / Server 2016+
}

func checkNtVersion(ntVersion string) error {
	ntVersion = strings.TrimSpace(ntVersion)
	if ntVersion == "" {
		return errors.New("NT version is required")
	}

	for _, v := range supportedNtVersions {
		if ntVersion == v || strings.HasPrefix(ntVersion, v+".") {
			return nil
		}
	}

	return errors.Errorf(
		"unsupported NT version `%s`",
		ntVersion,
	)
}

func ensureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

var unsupportedChars = regexp.MustCompile(`[^a-zA-Z0-9_.-]+`)
var multiUnderscore = regexp.MustCompile(`_+`)

// SafeName 仅保留字母、数字、下划线、中横线、点号。
func SafeName(s string) string {
	s = strings.TrimSpace(s)

	// 非法字符替换成 _
	s = unsupportedChars.ReplaceAllString(s, "_")

	// 连续 _ 合并
	s = multiUnderscore.ReplaceAllString(s, "_")

	// 去掉前后 _
	s = strings.Trim(s, "_")

	// 避免空
	if s == "" || s == "." || s == ".." {
		return "unnamed"
	}

	return s
}

// initIndexFile 初始化带索引文件目录。
func initIndexFile(path string) error {
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	if extend.IsExisted(path) {
		return nil
	}
	return os.WriteFile(
		path,
		[]byte("[]"),
		0o644,
	)
}
