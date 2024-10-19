package util

import (
	"os"
	"regexp"
)

var (
	expand_regex = regexp.MustCompile("%([a-zA-Z_0-9]+)%")
)

func ExpandEnv(v string) string {
	// 使用正则表达式替换Windows风格的环境变量.
	v = expand_regex.ReplaceAllString(v, "$${$1}")

	// 调用os.Expand来处理字符串中的环境变量替换。os.Expand会将${VAR}替换为实际的环境变量值.
	return os.Expand(v, getenv)
}

func getenv(v string) string {
	switch v {
	case "$":
		return "$"
	}
	return os.Getenv(v)
}
