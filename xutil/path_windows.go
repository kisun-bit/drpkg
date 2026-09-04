//go:build windows

package xutil

import (
	"path/filepath"
	"strings"
)

// Windows 路径前缀常量
const (
	ExtendedPathPrefix = `\\?\`
	UNCPathPrefix      = `\\?\UNC\`
	GlobalRootPrefix   = `\\?\GLOBALROOT\`
	VolumeGUIDPrefix   = `\\?\Volume{`
)

// ToExtendedPath 将 Windows 路径转换为扩展长度格式（\\?\ 前缀），
// 支持长路径（超过 260 字符）和 VSS 快照路径。
//
// 转换规则：
//   - 已是扩展格式 → 原样返回
//   - \\?\GLOBALROOT\ 且恰好 5 个反斜杠 → 追加尾部分隔符（裸卷名需要）
//   - 以 \\ 开头 → 转换为 \\?\UNC\ 格式
//   - 其他 → 追加 \\?\ 前缀
func ToExtendedPath(name string) string {
	abspath, err := filepath.Abs(name)
	if err == nil {
		// 已是 UNC 扩展格式
		if strings.HasPrefix(abspath, UNCPathPrefix) {
			return abspath
		}
		// VSS 快照路径：\\?\GLOBALROOT\Device\HarddiskVolumeShadowCopyXX
		if strings.HasPrefix(abspath, GlobalRootPrefix) {
			if strings.Count(abspath, `\`) == 5 {
				// 裸卷名需要尾部分隔符，否则访问卷本身会失败
				return abspath + string(filepath.Separator)
			}
			return abspath
		}
		// 已是扩展格式
		if strings.HasPrefix(abspath, ExtendedPathPrefix) {
			return abspath
		}
		// 网络路径 → UNC 扩展格式
		if strings.HasPrefix(abspath, `\\`) {
			return strings.Replace(abspath, `\\`, UNCPathPrefix, 1)
		}
		// 普通路径 → 扩展格式
		return ExtendedPathPrefix + abspath
	}
	return name
}

// ExtractVolumeName 从 Windows 路径中提取卷名，正确处理各种路径格式：
//   - VSS 快照路径：\\?\GLOBALROOT\Device\HarddiskVolumeShadowCopyXX
//   - 卷 GUID 路径：\\?\Volume{...}\
//   - UNC 路径：\\?\UNC\server\share\...
//   - 扩展路径：\\?\C:\...
//   - 普通路径：C:\...
func ExtractVolumeName(path string) (string, error) {
	// VSS 快照路径
	if strings.HasPrefix(path, GlobalRootPrefix) {
		// 提取 \\?\GLOBALROOT\Device\HarddiskVolumeShadowCopyXX
		if parts := strings.SplitN(path, `\`, 7); len(parts) >= 6 {
			return strings.Join(parts[:6], `\`), nil
		}
		return filepath.VolumeName(path), nil
	}

	// 卷 GUID 路径直接获取卷名
	if !strings.HasPrefix(path, VolumeGUIDPrefix) {
		if strings.HasPrefix(path, UNCPathPrefix) {
			// \\?\UNC\ → \\
			path = `\\` + path[len(UNCPathPrefix):]
		} else if strings.HasPrefix(path, ExtendedPathPrefix) {
			// \\?\ → 去掉前缀
			path = path[len(ExtendedPathPrefix):]
		} else {
			var err error
			path, err = filepath.Abs(path)
			if err != nil {
				return "", err
			}
		}
	}
	return filepath.VolumeName(path), nil
}