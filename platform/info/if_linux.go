package info

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"github.com/shirou/gopsutil/v3/net"
)

func QueryIFList() ([]IF, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, errors.Errorf("failed to query network info, %v", err)
	}
	is := make([]IF, 0)
	for _, i := range interfaces {
		f := IF{}
		f.IFExtra = QueryIFExtra(i.Name)
		f.InterfaceStat = i
		is = append(is, f)
	}
	return is, nil
}

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
