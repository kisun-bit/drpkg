package util

import (
	"strconv"
	"strings"
)

// CompareVersions 版本比较. 若 ver1 > ver2 则返回 true.
func CompareVersions(ver1, ver2 string) bool {
	v1Parts := strings.Split(ver1, ".")
	v2Parts := strings.Split(ver2, ".")

	for i := 0; i < MaxInt(len(v1Parts), len(v2Parts)); i++ {
		var v1, v2 int
		if i < len(v1Parts) {
			v1, _ = strconv.Atoi(v1Parts[i])
		}
		if i < len(v2Parts) {
			v2, _ = strconv.Atoi(v2Parts[i])
		}

		if v1 < v2 {
			return false
		} else if v1 > v2 {
			return true
		}
	}
	return false
}
