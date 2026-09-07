package ioctl

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/kisun-bit/drpkg/disk/table"
	"github.com/kisun-bit/drpkg/xutil"
	"github.com/pkg/errors"
)

// ////////////////////////////////////////////////////////////////////
// json
// /////////////////////////////////
// json
// Set：从普通 string 填充（超长则截断）
func (f *FixedName) Set(s string) {
	for i := range f {
		f[i] = 0
	}
	if len(s) > len(f) {
		copy(f[:], s[:len(f)])
	} else {
		copy(f[:], s)
	}
}

// String：去掉尾部 \x00 后返回 string
func (f FixedName) String() string {
	n := len(f)
	for n > 0 && f[n-1] == 0 {
		n--
	}
	return string(f[:n])
}

// UnmarshalJSON：支持 JSON 中的 string（也可以扩展支持 byte-array）
func (f *FixedName) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		// 如果不是 string，可以尝试解析为 []byte（可选）
		var arr []uint8
		if err2 := json.Unmarshal(data, &arr); err2 == nil {
			// 数组形式
			for i := range f {
				f[i] = 0
			}
			if len(arr) > len(f) {
				copy(f[:], arr[:len(f)])
			} else {
				copy(f[:], arr)
			}
			return nil
		}
		return err
	}
	// 以 string 方式填充（超过长度则截断）
	f.Set(s)
	return nil
}

// MarshalJSON：把 FixedName 序列化为 string（去掉尾部 0）
func (f FixedName) MarshalJSON() ([]byte, error) {
	return json.Marshal(f.String())
}

func PersistWriteFile(name string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	_ = f.Sync()
	if err1 := f.Close(); err1 != nil && err == nil {
		err = err1
	}
	return err
}

func StorageGuid(guid DiskGuid) (string, error) {
	key := ""

	switch runtime.GOOS {
	case "windows":
		key = strconv.Itoa(int(guid.DiskId))
	case "linux":
		key, _ = GetDiskPathByGuid(guid)
		if key != "" {
			key = filepath.Base(key)
		}
	}

	if key == "" {
		return "", errors.Errorf("failed to generate disk storage key")
	}

	return key, nil
}

func GetDiskPathByGuid(diskGuid DiskGuid) (string, error) {
	switch runtime.GOOS {
	case "windows":
		return fmt.Sprintf(`\\.\PHYSICALDRIVE%d`, diskGuid.DiskId), nil
	case "linux":
		return getLinuxDiskPath(diskGuid.Name.String())
	default:
		return "", errors.Errorf("unsupported os: %s", runtime.GOOS)
	}
}

type DiskPartTableData struct {
	Offset int64
	Data   []byte
}

func ReadDiskPartTableData(diskPath string) (dataList []DiskPartTableData, err error) {
	dt, err := table.GetDiskType(diskPath)
	if err != nil {
		return dataList, err
	}

	switch dt {
	case table.TableTypeMBR:
		// 主引导记录
		mbr, err := table.NewMBR(diskPath, 0, false)
		if err != nil {
			return dataList, err
		}
		defer mbr.Close()
		dataList = append(dataList, DiskPartTableData{
			Offset: 0,
			Data:   mbr.Bin,
		})
		// EBR记录
		ebrs, err := mbr.EBRList()
		if err != nil {
			return dataList, err
		}
		for _, ebr := range ebrs {
			dataList = append(dataList, DiskPartTableData{
				Offset: ebr.Offset,
				Data:   ebr.Bin,
			})
		}
		return dataList, nil
	case table.TableTypeGPT:
		// 主分区表
		gpt, err := table.NewGPT(diskPath, 0)
		if err != nil {
			return dataList, err
		}
		defer gpt.Close()
		dataList = append(dataList, DiskPartTableData{
			Offset: 0,
			Data:   gpt.Bin,
		})
		// 备份分区表
		bgpt, err := gpt.BackupGPT()
		if err != nil {
			return dataList, err
		}
		dataList = append(dataList, DiskPartTableData{
			Offset: bgpt.Offset,
			Data:   bgpt.Bin,
		})
		return dataList, nil
	default:
		return dataList, nil
	}
}

func DiskProtectRegions(pds []ProtectDisk, guid DiskGuid) (protectRegions []Segment) {
	if pds == nil {
		return
	}

	for _, pd := range pds {
		switch runtime.GOOS {
		case "windows":
			if pd.Guid.DiskId != guid.DiskId {
				continue
			}
			break
		default:
			if pd.Guid != guid {
				continue
			}
			break
		}
		return pd.ProtectRegions
	}

	return nil
}

func IsBlockProtected(protectRegions []Segment, blockStart uint64, blockSize uint64) bool {
	if blockSize == 0 {
		return false
	}

	if blockStart > ^uint64(0)-blockSize {
		return false
	}
	blockEnd := blockStart + blockSize

	for _, r := range protectRegions {
		if r.Size == 0 {
			continue
		}
		if r.Start > ^uint64(0)-r.Size {
			continue
		}
		regionEnd := r.Start + r.Size

		// 完全包含判断
		if blockStart >= r.Start && blockEnd <= regionEnd {
			return true
		}
	}

	return false
}

func getLinuxDiskPath(name string) (string, error) {
	type rule struct {
		baseDir string
		resolve func(string) (string, error)
	}

	rules := []rule{
		{"/dev/disk/by-path", resolveSymlink},
		{"/dev/disk/by-id", resolveSymlink},
		{"/dev/mapper", resolveMpath},
		{"/dev", resolveDirect},
	}

	for _, r := range rules {
		path := filepath.Join(r.baseDir, name)
		if !xutil.IsExisted(path) {
			continue
		}

		return r.resolve(path)
	}

	return "", errors.New("disk not found")
}

func resolveSymlink(path string) (string, error) {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return filepath.Join("/dev", filepath.Base(target)), nil
}

func resolveMpath(path string) (string, error) {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}

	uuidPath := filepath.Join("/sys/block", filepath.Base(target), "dm", "uuid")
	data, err := os.ReadFile(uuidPath)
	if err != nil {
		return "", err
	}

	if !strings.HasPrefix(strings.TrimSpace(string(data)), "mpath-") {
		return "", errors.Errorf("invalid mpath uuid: %s", uuidPath)
	}

	return path, nil
}

func resolveDirect(path string) (string, error) {
	return path, nil
}
