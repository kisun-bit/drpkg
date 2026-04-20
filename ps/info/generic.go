package info

import (
	"fmt"
	"os"
	"runtime"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/thoas/go-funk"
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
	Cpu CpuStat `json:"cpu"`
	// Mem 内存信息
	Mem mem.VirtualMemoryStat `json:"mem"`
}

type CpuStat struct {
	// Vendors 制造商ID
	Vendors []string `json:"vendors"`
	// Models 型号
	Models []string `json:"models"`
	// Slots 插槽数量
	// 通过CoresId+PhysicalId联合去重
	Slots int `json:"slots"`
	// PhysicalCores 物理核心数
	PhysicalCores int `json:"physicalCores"`
}

// QueryGeneric 查询通用信息
func QueryGeneric() (g Generic, err error) {
	g.OS = runtime.GOOS
	g.OCIArch = runtime.GOARCH

	if g.Hostname, err = os.Hostname(); err != nil {
		return
	}

	if g.Cpu, err = QueryCpuStat(); err != nil {
		return
	}

	m, e := mem.VirtualMemory()
	if e != nil {
		return g, e
	}
	g.Mem = *m

	return g, nil
}

func QueryCpuStat() (cs CpuStat, err error) {
	cpuList, err := cpu.Info()
	if err != nil {
		return cs, err
	}

	slotList := make([]string, 0)
	for _, c := range cpuList {
		if c.VendorID != "" && !funk.InStrings(cs.Vendors, c.VendorID) {
			cs.Vendors = append(cs.Vendors, c.VendorID)
		}
		if c.ModelName != "" && !funk.InStrings(cs.Models, c.ModelName) {
			cs.Models = append(cs.Models, c.ModelName)
		}
		slotId := fmt.Sprintf("%s-%s", c.PhysicalID, c.CoreID)
		if !funk.InStrings(slotList, slotId) {
			slotList = append(slotList, slotId)
			cs.Slots++
		}
		cs.PhysicalCores += int(c.Cores)
	}

	return cs, nil
}
