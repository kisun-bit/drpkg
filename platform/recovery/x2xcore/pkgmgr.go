package x2xcore

import (
	"os"
	"path/filepath"
)

type PackageManager int

const (
	PackageManagerUnknown PackageManager = iota
	PackageManagerRPM
	PackageManagerDEB
)

func (p PackageManager) String() string {
	switch p {
	case PackageManagerRPM:
		return "rpm"
	case PackageManagerDEB:
		return "deb"
	default:
		return "unknown"
	}
}

func DetectPackageManager(sysroot string) PackageManager {
	join := func(p string) string {
		return filepath.Join(sysroot, p)
	}

	exists := func(path string) bool {
		_, err := os.Stat(path)
		return err == nil
	}

	// =========================
	// 1. 优先检测真实包数据库
	// =========================

	// Debian/Ubuntu
	dpkgMarkers := []string{
		"var/lib/dpkg/status",
		"var/lib/dpkg/available",
		"var/lib/dpkg/info",
	}

	for _, p := range dpkgMarkers {
		if exists(join(p)) {
			return PackageManagerDEB
		}
	}

	// RPM 系（兼容老版/新版）
	rpmMarkers := []string{
		"var/lib/rpm",          // 老版 rpm db
		"usr/lib/sysimage/rpm", // RHEL8+/Fedora/SUSE 新版
	}

	for _, p := range rpmMarkers {
		if exists(join(p)) {
			return PackageManagerRPM
		}
	}

	// =========================
	// 2. 检测二进制工具
	// =========================

	debBins := []string{
		"usr/bin/dpkg",
		"usr/bin/apt",
		"usr/bin/apt-get",
	}

	for _, p := range debBins {
		if exists(join(p)) {
			return PackageManagerDEB
		}
	}

	rpmBins := []string{
		"usr/bin/rpm",
		"bin/rpm",
		"usr/bin/dnf",
		"usr/bin/yum",
		"usr/bin/zypper", // SUSE
	}

	for _, p := range rpmBins {
		if exists(join(p)) {
			return PackageManagerRPM
		}
	}

	return PackageManagerUnknown
}
