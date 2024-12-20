package util

import (
	"log"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

func GetConfigFileAbsPath(path string) string {
	path = filepath.FromSlash(path)
	if !filepath.IsAbs(path) {
		execPath := GetExecutableAbPath()
		path = filepath.FromSlash(filepath.Join(execPath, path))
	}
	return path
}

func GetExecutableAbPath() string {
	dir := getCurrentAbPathByExecutable()
	tmpDir, _ := filepath.EvalSymlinks(os.TempDir())
	if strings.Contains(dir, tmpDir) {
		return getCurrentAbPathByCaller()
	}
	return dir
}

// getCurrentAbPathByExecutable 获取当前执行文件绝对路径
func getCurrentAbPathByExecutable() string {
	exePath, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	res, _ := filepath.EvalSymlinks(filepath.Dir(exePath))
	return res
}

// getCurrentAbPathByCaller 获取当前执行文件绝对路径（go run）
func getCurrentAbPathByCaller() string {
	var abPath string
	_, filename, _, ok := runtime.Caller(0)
	if ok {
		abPath = path.Dir(filename)
	}
	return abPath
}
