package extend

import (
	"path/filepath"
	"syscall"
)

func FindMountPoint(path string) (string, error) {
	absPath, err := filepath.Abs(path) // 获取绝对路径
	if err != nil {
		return "", err
	}

	for {
		var stat syscall.Stat_t
		err = syscall.Stat(absPath, &stat)
		if err != nil {
			return "", err
		}

		// 获取父目录的绝对路径
		parentPath := filepath.Dir(absPath)

		// 获取父目录的stat信息
		var parentStat syscall.Stat_t
		err = syscall.Stat(parentPath, &parentStat)
		if err != nil {
			return "", err
		}

		// 如果当前路径与父目录的设备ID不同，则当前路径是挂载点
		if stat.Dev != parentStat.Dev {
			return absPath, nil
		}

		// 如果已经到达根目录，直接返回根目录
		if absPath == parentPath {
			return absPath, nil
		}

		// 逐级向上检查
		absPath = parentPath
	}
}
