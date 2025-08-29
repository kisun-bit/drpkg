package info

import (
	"os"
	"runtime"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

// Generic 通用信息
type Generic struct {
	// Hostname 主机名
	Hostname string `json:"hostname"`
	// OS 系统类型
	OS string `json:"os"`
	// OCIArch 系统架构
	OCIArch string `json:"ociArch"`
	// Cpu CPU信息
	Cpu []cpu.InfoStat `json:"cpu"`
	// Mem 内存信息
	Mem mem.VirtualMemoryStat `json:"mem"`
}

// QueryGeneric 查询通用信息
func QueryGeneric() (g Generic, err error) {
	g.OS = runtime.GOOS
	g.OCIArch = runtime.GOARCH

	if g.Hostname, err = os.Hostname(); err != nil {
		return
	}

	if g.Cpu, err = cpu.Info(); err != nil {
		return
	}

	m, e := mem.VirtualMemory()
	if e != nil {
		return g, e
	}
	g.Mem = *m

	return g, nil
}
