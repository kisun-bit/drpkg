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
	if _, e := os.Stat(name); e == nil {
		return true
	}
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

func FindSymlinkByDeviceName(base string, deviceName string) (name string, ok bool) {
	if !IsExisted(base) {
		return "", false
	}

	links, err := os.ReadDir(base)
	if err != nil {
		return "", false
	}
	for _, link := range links {
		linkName := link.Name()
		linkPath := filepath.Join(base, linkName)
		linkTarget, err := filepath.EvalSymlinks(linkPath)
		if err != nil {
			return "", false
		}
		if linkTarget == filepath.Join("/dev", filepath.Base(deviceName)) {
			return linkName, true
		}
	}
	return "", false
}

type dirCache struct {
	files map[string]struct{}
	dirs  map[string]struct{}
}

func readDirCache(dir string) (*dirCache, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	c := &dirCache{
		files: make(map[string]struct{}, len(entries)),
		dirs:  make(map[string]struct{}, len(entries)),
	}

	for _, e := range entries {
		if e.IsDir() {
			c.dirs[e.Name()] = struct{}{}
		} else {
			c.files[e.Name()] = struct{}{}
		}
	}
	return c, nil
}

func existsEntry(root, subPath string, wantDir bool) bool {
	full := filepath.Join(root, subPath)

	info, err := os.Stat(full)
	if err != nil {
		return false
	}

	if wantDir {
		return info.IsDir()
	}
	return !info.IsDir()
}

// 任意前缀文件（只匹配当前目录）
func ContainAnySubPrefixFiles(dir string, prefixList ...string) bool {
	if len(prefixList) == 0 {
		return false
	}

	c, err := readDirCache(dir)
	if err != nil {
		return false
	}

	for name := range c.files {
		for _, prefix := range prefixList {
			if strings.HasPrefix(name, prefix) {
				return true
			}
		}
	}
	return false
}

// 任意目录
func ContainAnySubDirs(dir string, subDirs ...string) bool {
	c, err := readDirCache(dir)
	if err != nil {
		return false
	}

	for _, name := range subDirs {
		// 支持 Boot/EFI 这种
		if strings.Contains(name, "/") {
			if existsEntry(dir, name, true) {
				return true
			}
			continue
		}

		if _, ok := c.dirs[name]; ok {
			return true
		}
	}
	return false
}

// 全部目录
func ContainAllSubDirs(dir string, subDirs ...string) bool {
	c, err := readDirCache(dir)
	if err != nil {
		return false
	}

	for _, name := range subDirs {
		if strings.Contains(name, "/") {
			if !existsEntry(dir, name, true) {
				return false
			}
			continue
		}

		if _, ok := c.dirs[name]; !ok {
			return false
		}
	}
	return true
}

// 全部文件
func ContainAllSubFiles(dir string, subFiles ...string) bool {
	c, err := readDirCache(dir)
	if err != nil {
		return false
	}

	for _, name := range subFiles {
		if strings.Contains(name, "/") {
			if !existsEntry(dir, name, false) {
				return false
			}
			continue
		}

		if _, ok := c.files[name]; !ok {
			return false
		}
	}
	return true
}

// 全部条目（文件或目录）
func ContainAllSubEntries(dir string, subEntries ...string) bool {
	c, err := readDirCache(dir)
	if err != nil {
		return false
	}

	for _, name := range subEntries {
		if strings.Contains(name, "/") {
			// 不确定类型 → 直接 stat
			full := filepath.Join(dir, name)
			if _, err := os.Stat(full); err != nil {
				return false
			}
			continue
		}

		if _, ok := c.files[name]; ok {
			continue
		}
		if _, ok := c.dirs[name]; ok {
			continue
		}

		return false
	}
	return true
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

func NormalizeWindowsRoot(dir string) string {
	// 处理 "C:" → "C:\"
	if len(dir) == 2 && dir[1] == ':' {
		return dir + "\\"
	}

	//// 如果不是以 "\" 结尾，则补上
	//if !strings.HasSuffix(dir, "\\") {
	//	return dir + "\\"
	//}

	return dir
}

func EffectiveForBoot(dir string) bool {
	return IsRootDir(dir) || IsBootDir(dir) || IsEfiDir(dir)
}

func IsWindowsRoot(dir string) bool {
	if !ContainAllSubDirs(dir, "Windows") {
		return false
	}
	registryPath := filepath.Join(dir, "Windows", "System32", "config", "SYSTEM")
	return IsExisted(registryPath)
}

func IsLinuxRoot(dir string) bool {
	// 必须目录（放宽）
	if !ContainAllSubDirs(dir, "etc", "usr") {
		return false
	}

	// 至少存在一个关键文件
	passwdPath := filepath.Join(dir, "etc", "passwd")
	if !IsExisted(passwdPath) {
		return false
	}

	// systemd 或 init 存在一个
	initPath := filepath.Join(dir, "sbin", "init")
	if IsExisted(initPath) {
		return true
	}
	sysmdPath := filepath.Join(dir, "lib", "systemd", "systemd")
	if IsExisted(sysmdPath) {
		return true
	}

	return false
}

func IsEfiBoot(dir string) bool {
	if !ContainAllSubDirs(dir, "EFI") {
		return false
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) == 0 {
		return false
	}

	return true
}

func IsLinuxBoot(dir string) bool {
	if ContainAnySubDirs(dir, "grub", "grub2") {
		return true
	}

	if ContainAnySubPrefixFiles(dir, "vmlinuz", "initrd", "initramfs") {
		return true
	}

	return false
}

func IsWindowsBoot(dir string) bool {
	if !ContainAllSubFiles(dir, "bootmgr") {
		return false
	}

	bcdPath := filepath.Join(dir, "Boot", "BCD")
	if IsExisted(bcdPath) {
		return true
	}
	return false
}

func IsRootDir(dir string) bool {
	switch runtime.GOOS {
	case "windows":
		dir = NormalizeWindowsRoot(dir)
		return IsWindowsRoot(dir)
	case "linux":
		return IsLinuxRoot(dir)
	default:
		return false
	}
}

func IsEfiDir(dir string) bool {
	if runtime.GOOS == "windows" {
		dir = NormalizeWindowsRoot(dir)
	}
	return IsEfiBoot(dir)
}

func IsBootDir(dir string) bool {
	switch runtime.GOOS {
	case "windows":
		dir = NormalizeWindowsRoot(dir)
		return IsWindowsBoot(dir)

	case "linux":
		return IsLinuxBoot(dir)

	default:
		return false
	}
}
