//go:build !windows

package xutil

// ToExtendedPath 非 Windows 平台直接返回原路径。
func ToExtendedPath(name string) string {
	return name
}

// ExtractVolumeName 非 Windows 平台返回空字符串。
func ExtractVolumeName(path string) (string, error) {
	return "", nil
}