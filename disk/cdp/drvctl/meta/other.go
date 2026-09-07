package meta

import (
	"crypto/md5"
	"encoding/hex"
	"io"
	"os"
	"strings"

	"github.com/pkg/errors"
)

type DevMajor string

// Segment 表示卷在物理磁盘上的连续数据区间
type Segment struct {
	Disk  string `json:"disk"`  // 磁盘路径，例如"/dev/sda"或"\\.\PHYSICALDRIVE0"
	Start uint64 `json:"start"` // 起始偏移量
	Size  uint64 `json:"size"`  // 区间大小
}

func (dm DevMajor) splitMajor() (major, minor string) {
	parts := strings.SplitN(string(dm), ":", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

func (dm DevMajor) MajorNum() string {
	major, _ := dm.splitMajor()
	return major
}

func (dm DevMajor) MinNum() string {
	_, minor := dm.splitMajor()
	return minor
}

//func (dm DevMajor) IsLV() bool {
//	return dm.MajorNum() == "253"
//}

func Md5sum(r io.Reader) (string, error) {
	h := md5.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	code := h.Sum(nil)
	return hex.EncodeToString(code), nil
}

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
