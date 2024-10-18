package ioctl

import (
	"fmt"
	"github.com/pkg/errors"
	"io/fs"
	"os"
	"path/filepath"
)

// DetectIfCfgAndGenerateManager 检测可能的网络配置路径.
// 疑似路径集合:
// 1. $TARGET_FS_ROOT/etc/sysconfig/*/ifcfg-*
// 2. $TARGET_FS_ROOT/etc/network/inter[f]aces
// 3. $TARGET_FS_ROOT/etc/network/interfaces.d/*  (注意: 需保证$TARGET_FS_ROOT/etc/network/inter[f]aces存在)
// 4. $TARGET_FS_ROOT/etc/NetworkManager/system-connections/*.nmconnection
// 5. TODO more...
func DetectIfCfgAndGenerateManager(rootDir, ifName string) (ifManager NetworkCfgManager, err error) {
	var (
		ifcfgPattern = fmt.Sprintf("%s/%s", rootDir, "etc/sysconfig/*/ifcfg-*")

		// TODO 后续支持.
		_ = fmt.Sprintf("%s/%s", rootDir, "etc/network/inter[f]aces")
		_ = fmt.Sprintf("%s/%s", rootDir, "etc/network/interfaces.d/*")
		_ = fmt.Sprintf("%s/%s", rootDir, "etc/NetworkManager/system-connections/*.nmconnection")
	)

	ifCfgMatches, err := filepath.Glob(ifcfgPattern)
	if err == nil && len(ifCfgMatches) > 0 {
		for _, ifCfgPath := range ifCfgMatches {
			if filepath.Base(ifCfgPath) != fmt.Sprintf("ifcfg-%s", ifName) {
				continue
			}
			fi, e := os.Lstat(ifCfgPath)
			if e != nil {
				return nil, errors.Wrapf(e, "ifcfg is %s", ifCfgPath)
			}
			if fi.IsDir() {
				continue
			}
			// TODO 兼容链接配置文件.
			if fi.Mode()&fs.ModeSymlink != 0 {
				continue
			}
			return NewIfCfgManager(ifCfgPath)
		}
	}

	return nil, errors.Errorf("unable to find network card(%s) configuration file", ifName)
}
