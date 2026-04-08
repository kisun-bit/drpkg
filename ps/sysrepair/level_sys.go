package sysrepair

import (
	"fmt"

	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/ps/bus/pci/universal"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
)

// SystemCompatChecker 系统兼容性检测器
type SystemCompatChecker struct {
	SysContext SysContextType

	// RootDir 系统根目录
	// Windows: "C:\\"
	// Linux:   "/"
	RootDir string

	// PciList 硬件枚举
	PciList []*universal.UniPci

	// KernelCompats 各内核兼容性
	KernelCompats []KernelCompat
}

type PciClass struct {
	BaseClass  uint32
	SubClasses []uint32
}

// NewSystemCompatChecker 创建系统兼容性检测器
func NewSystemCompatChecker(rootDir string, pciList []string, baseClassExclude []uint32) (sc *SystemCompatChecker, err error) {
	logger.Debugf("NewSystemCompatChecker ++")
	defer logger.Debugf("NewSystemCompatChecker --")

	if !extend.IsExisted(rootDir) {
		return sc, errors.Errorf("root dir %s does not exist", rootDir)
	}

	sc = &SystemCompatChecker{}
	sc.SysContext = SysContextTypeFromRootDir(rootDir)
	sc.RootDir = rootDir

	for _, pciStr := range pciList {
		p, ep := universal.UniPciFromString(pciStr)
		if ep != nil {
			return nil, ep
		}

		if funk.InUInt32s(baseClassExclude, p.BaseClassId()) {
			logger.Warnf("NewSystemCompatChecker: %s: ignore %s (human: %s)", sc, pciStr, p.Human())
			continue
		}
		sc.PciList = append(sc.PciList, p)
	}

	for _, p := range sc.PciList {
		logger.Infof("NewSystemCompatChecker: %s: add %s (human: %s)", sc, p, p.Human())
	}

	return sc, nil
}

func (sc *SystemCompatChecker) String() string {
	return fmt.Sprintf("SystemCompatChecker{Context:%s, RootDir:%s}", sc.SysContext, sc.RootDir)
}

func (sc *SystemCompatChecker) Check() error {
	logger.Debugf("%s Check ++", sc)
	defer logger.Debugf("%s Check --", sc)

	//
	// 探测内核
	//

	//
	// 探测内核对硬件的兼容性
	//

	return nil
}
