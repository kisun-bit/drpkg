package info

import (
	"os"
	"path/filepath"
	"strings"
)

func QueryIFExtra(ifName string) (info IFExtra, ok bool) {
	info.Physical = IsPhysicalIF(ifName)
	return info, true
}

func IsPhysicalIF(ifName string) bool {
	o, _ := os.ReadFile(filepath.Join("/sys/class/net", ifName, "device/modalias"))
	return strings.TrimSpace(string(o)) != ""
}
