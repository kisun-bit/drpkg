package hkd

import (
	"encoding/binary"
	"fmt"
	"io"
	"strings"

	"github.com/kisun-bit/drpkg/storage/backend"
)

// Options 描述创建 HKD 磁盘备份所需的标识信息与磁盘属性。
//
// 标识用于生成备份对象的存储路径与唯一标识，且不能包含空格或特殊字符，
// 只能由数字、字母和下划线组成。
type Options struct {
	// DisasterSystemID 灾备系统 ID，8 位随机字符串。
	DisasterSystemID string
	// UserID 用户 ID，8 位数字，不足部分左侧补 0。
	UserID string
	// StorageMediaID 存储介质 ID，UUID 格式。
	StorageMediaID string
	// PolicyID 策略 ID，UUID 格式。
	PolicyID string
	// TaskID 任务 ID，UUID 格式。
	TaskID string
	// HostID 源主机 ID。
	HostID string
	// DiskID 源磁盘 ID。
	DiskID string

	// DiskSize 磁盘总大小，单位字节。
	DiskSize uint64
	// LBASize 逻辑扇区大小，单位字节。
	LBASize uint32
	// PBASize 物理扇区大小，单位字节。
	PBASize uint32
	// ClusterSize Cluster 大小，即磁盘分块粒度，单位字节。
	ClusterSize uint64
	// VolMaxSize 单个 vol 分卷最大容量，0 表示使用默认值（1 TiB）。
	VolMaxSize uint64

	// Check 启用 Cluster 一致性校验。
	Check bool
	// Compress 启用数据压缩。
	Compress bool
	// Encrypt 启用数据加密，需先通过 SetEncryptionKey 设置密钥。
	Encrypt bool
	// Compact 启用覆盖写原地回收与 Compact 垃圾整理；false 时覆盖写始终追加
	// 到 vol 文件末尾，仅在达到分卷阈值时另开 vol。
	Compact bool
}

// HKD 表示一个磁盘备份对象，用于描述和管理一个完整的磁盘备份。
//
// 一个 HKD 磁盘备份由标识生成的路径下的三类文件组成：
//   - {DiskID}.hkd        主对象，保存磁盘属性与配置；
//   - {DiskID}.hki        index 对象，保存元信息与 Cluster 索引；
//   - {DiskID}.%08d.hkc   若干 vol 分卷对象，保存 Cluster 数据。
type HKD struct {
	acc    backend.Accessor
	opt    Options
	base   string
	diskID string

	index   *Index
	curVol  uint32
	volOff  uint64
	volFile *backend.StorageFile
	closed  bool

	// writable 表示当前句柄用于写入（Create 建立）；Open 建立的句柄只读。
	writable bool

	// volMaxSize 是单个分卷的容量上限。
	volMaxSize uint64

	// backing 是 overlay 的父备份；未写入的 Cluster 读取时回落到 backing。
	backing *HKD
}

// hkdHeader 是主文件 .hkd 的固定头，只描述磁盘自身特性，不含备份业务字段。
type hkdHeader struct {
	Magic       [4]byte // 文件标识，固定为 "HKD1"
	DiskSize    uint64  // 磁盘总大小
	ClusterSize uint64  // Cluster 大小（磁盘分块粒度）
	LBASize     uint32  // 逻辑扇区大小
	PBASize     uint32  // 物理扇区大小
	Flags       uint32  // 0x01=加密 0x02=压缩 0x04=校验
}

const hkdHeaderSize = 4 + 8 + 8 + 4 + 4 + 4

var hkdMagic = [4]byte{'H', 'K', 'D', '1'}

const (
	flagEncrypt  uint32 = 1 << 0 // 0x01
	flagCompress uint32 = 1 << 1 // 0x02
	flagCheck    uint32 = 1 << 2 // 0x04
)

// basePath 返回由标识生成的存储目录前缀（不含 DiskID）。
func (o Options) basePath() string {
	return strings.Join([]string{
		o.DisasterSystemID, o.UserID, o.StorageMediaID, o.PolicyID, o.TaskID, o.HostID,
	}, "/")
}

func (h *HKD) indexName() string { return h.diskID + ".hki" }

func (h *HKD) mainName() string { return h.diskID + ".hkd" }

func (h *HKD) volName(volID uint32) string {
	return fmt.Sprintf("%s.%s", h.diskID, volFileName(volID))
}

// joinBase 把文件名拼接到备份根目录前缀。
func (h *HKD) joinBase(name string) string {
	if h.base == "" {
		return name
	}
	return h.base + "/" + name
}

// validate 校验标识与磁盘属性。
func (o Options) validate() error {
	if len(o.DisasterSystemID) != 8 {
		return fmt.Errorf("hkd: DisasterSystemID must be 8 characters, got %d", len(o.DisasterSystemID))
	}
	if len(o.UserID) != 8 {
		return fmt.Errorf("hkd: UserID must be 8 characters, got %d", len(o.UserID))
	}
	for _, s := range []struct {
		name  string
		value string
		max   int
	}{
		{"StorageMediaID", o.StorageMediaID, 36},
		{"PolicyID", o.PolicyID, 36},
		{"TaskID", o.TaskID, 36},
		{"HostID", o.HostID, 64},
		{"DiskID", o.DiskID, 64},
	} {
		if s.value == "" {
			return fmt.Errorf("hkd: %s is required", s.name)
		}
		if len(s.value) > s.max {
			return fmt.Errorf("hkd: %s too long: %d > %d", s.name, len(s.value), s.max)
		}
	}
	if o.DiskID == "" {
		return fmt.Errorf("hkd: DiskID is required")
	}
	return nil
}

// flags 根据配置生成 flags 位。
func (o Options) flags() uint32 {
	var f uint32
	if o.Encrypt {
		f |= flagEncrypt
	}
	if o.Compress {
		f |= flagCompress
	}
	if o.Check {
		f |= flagCheck
	}
	return f
}

// volLimit 返回单个分卷的容量上限。
func (o Options) volLimit() uint64 {
	if o.VolMaxSize == 0 {
		return defaultVolMaxSize
	}
	return o.VolMaxSize
}

// toHeader 把 Options 的磁盘属性与配置生成为主文件头。
func (o Options) toHeader() hkdHeader {
	return hkdHeader{
		Magic:       hkdMagic,
		DiskSize:    o.DiskSize,
		ClusterSize: o.ClusterSize,
		LBASize:     o.LBASize,
		PBASize:     o.PBASize,
		Flags:       o.flags(),
	}
}

// readHkdHeader 从 r 读取并解析主文件头。
func readHkdHeader(r io.Reader) (hkdHeader, error) {
	buf := make([]byte, hkdHeaderSize)
	if _, err := io.ReadFull(r, buf); err != nil {
		return hkdHeader{}, err
	}
	var h hkdHeader
	copy(h.Magic[:], buf[0:4])
	h.DiskSize = binary.BigEndian.Uint64(buf[4:12])
	h.ClusterSize = binary.BigEndian.Uint64(buf[12:20])
	h.LBASize = binary.BigEndian.Uint32(buf[20:24])
	h.PBASize = binary.BigEndian.Uint32(buf[24:28])
	h.Flags = binary.BigEndian.Uint32(buf[28:32])

	if h.Magic != hkdMagic {
		return hkdHeader{}, fmt.Errorf("hkd: invalid main header magic: %x", h.Magic)
	}
	return h, nil
}

// writeHkdHeader 把主文件头序列化写入 w。
func writeHkdHeader(w io.Writer, h hkdHeader) error {
	buf := make([]byte, hkdHeaderSize)
	copy(buf[0:4], h.Magic[:])
	binary.BigEndian.PutUint64(buf[4:12], h.DiskSize)
	binary.BigEndian.PutUint64(buf[12:20], h.ClusterSize)
	binary.BigEndian.PutUint32(buf[20:24], h.LBASize)
	binary.BigEndian.PutUint32(buf[24:28], h.PBASize)
	binary.BigEndian.PutUint32(buf[28:32], h.Flags)
	_, err := w.Write(buf)
	return err
}
