package extend

import (
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/pkg/errors"
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

const (
	windowsDiskObjPrefix         = `\\.\PHYSICALDRIVE`
	windowsVolumeShadowObjPrefix = `\\?\GLOBALROOT\DEVICE\HARDDISKVOLUMESHADOWCOPY`
	windowsVolumeObjPrefix       = `\\?\VOLUME`
)

func IsWindowsDisk(path string) bool {
	return strings.HasPrefix(strings.ToUpper(path), windowsDiskObjPrefix)
}

func IsWindowsVolumeShadow(path string) bool {
	return strings.HasPrefix(strings.ToUpper(path), windowsVolumeShadowObjPrefix)
}

func IsWindowsVolume(path string) bool {
	upperPath := strings.ToUpper(path)
	return strings.HasSuffix(path, `:\`) || strings.HasPrefix(upperPath, windowsVolumeObjPrefix)
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

// IsEmptyDir 判断目录是否为空
func IsEmptyDir(dir string) bool {
	f, err := os.Open(dir)
	if err != nil {
		return true
	}
	defer f.Close()
	ds, err := f.ReadDir(1)
	if err != nil {
		return true
	}
	return len(ds) == 0
}

// IsLinkTargetExisted 链接目标文件是否存在
func IsLinkTargetExisted(searchDir, name string, recursive bool) bool {
	var found bool
	var walkFunc filepath.WalkFunc = func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}
			if filepath.Base(target) == name {
				found = true
				return filepath.SkipDir
			}
		}
		if !recursive && info.IsDir() && path != searchDir {
			return filepath.SkipDir
		}
		return nil
	}
	err := filepath.Walk(searchDir, walkFunc)
	if err != nil {
		return false
	}
	return found
}

func FilenameIfExisted(path string) string {
	if IsExisted(path) {
		return filepath.Base(path)
	}
	return ""
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

func GlobReadFiles(globPath string) string {
	files, _ := filepath.Glob(globPath)
	var output []string
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		output = append(output, string(data))
	}
	return strings.TrimSpace(strings.Join(output, "\n"))
}

func FileSize(path string) (size uint64, err error) {
	return GetFileSize(path)
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
