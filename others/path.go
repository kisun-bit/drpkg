package others

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// ExecDir 返回程序运行的目录
func ExecDir() string {
	dir := getCurrentDirByExecutable()
	tmpDir, _ := filepath.EvalSymlinks(os.TempDir())
	if strings.Contains(dir, tmpDir) {
		return getCurrentDirByCaller()
	}
	return dir
}

const windowsDiskObjPrefix = `\\.\PHYSICALDRIVE`

func IsWindowsDisk(path string) bool {
	return strings.HasPrefix(strings.ToUpper(path), windowsDiskObjPrefix)
}

// WindowsDiskPathFromID 基于Windows磁盘ID去生成磁盘路径
func WindowsDiskPathFromID(id uint32) string {
	return fmt.Sprintf("%s%v", windowsDiskObjPrefix, id)
}

// WindowsDiskIDFromPath 解析Windows磁盘的ID
func WindowsDiskIDFromPath(path string) (id uint32, err error) {
	diskPath := strings.ToUpper(path)
	if !IsWindowsDisk(diskPath) {
		return 0, fmt.Errorf("invalid windows disk path: %s", diskPath)
	}
	diskIdStr := strings.TrimPrefix(strings.ToUpper(diskPath), windowsDiskObjPrefix)
	diskId64, err := strconv.ParseUint(diskIdStr, 10, 64)
	if err != nil {
		return 0, err
	}
	return uint32(diskId64), nil
}

// IsExisted 判断此路径是否存在
func IsExisted(name string) bool {
	fd, err := os.Open(name)
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	_ = fd.Close()
	return true
}

// IsDir 判断此路径是否是目录
func IsDir(path string) bool {
	if stat, err := os.Stat(path); err == nil && stat.IsDir() {
		return true
	}
	return false
}

// CopyFile 拷贝文件
func CopyFile(src, dst string) (int64, error) {
	stat, err := os.Stat(src)
	if err != nil {
		return 0, err
	}

	if !stat.Mode().IsRegular() {
		return 0, fmt.Errorf("%s is not a regular file", src)
	}

	source, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = source.Close()
	}()

	destination, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	func() {
		_ = destination.Close()
	}()

	return io.Copy(destination, source)
}

func getCurrentDirByExecutable() string {
	exePath, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	res, _ := filepath.EvalSymlinks(filepath.Dir(exePath))
	return res
}

func getCurrentDirByCaller() string {
	var abPath string
	_, filename, _, ok := runtime.Caller(0)
	if ok {
		abPath = path.Dir(filename)
	}
	return abPath
}
