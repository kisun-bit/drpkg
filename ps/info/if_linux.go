package info

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func QueryIFExtra(ifName string) IFExtra {
	ie := IFExtra{}
	c, _ := os.ReadFile(fmt.Sprintf("/sys/class/net/%s/operstate", ifName))
	ie.Linked = strings.TrimSpace(string(c)) == "up"
	ie.Physical = IsPhysicalIF(ifName)
	return ie
}

func IsPhysicalIF(ifName string) bool {
	o, _ := os.ReadFile(filepath.Join("/sys/class/net", ifName, "device/modalias"))
	return strings.TrimSpace(string(o)) != ""
}
