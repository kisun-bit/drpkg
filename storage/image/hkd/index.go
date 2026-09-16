package hkd

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Index 描述 HKD 磁盘备份的元信息与 Cluster 寻址信息。
//
// 磁盘被划分为 ClusterSize 大小的固定区间，每个区间称为一个 Cluster，编号
// 从 0 到 N-1（N = ceil(DiskSize/ClusterSize)）。Cluster 编号即代表其覆盖的
// 磁盘区域：第 i 个 Cluster 覆盖磁盘 [i*ClusterSize, (i+1)*ClusterSize)。
//
// Index 由固定 512 字节 Header 与紧跟其后的定长索引区组成。索引区按 Cluster
// 编号顺序紧密排列，每项定长 16 字节，故第 i 个 Cluster 的索引项位于文件
// 偏移 indexHeaderSize + i*indexEntrySize，偏移可精确推导。
type Index struct {
	header  indexHeader
	entries []indexEntry
}

// indexHeader 是 .hki 文件的固定头，序列化后总计 512 字节。
type indexHeader struct {
	Magic        [4]byte // 文件标识，固定为 "HKI1"
	DiskSize     uint64  // 源磁盘总大小，单位字节
	ClusterSize  uint64  // Cluster 大小（磁盘分块粒度），单位字节
	ClusterCount uint64  // Cluster 总数量 N = ceil(DiskSize/ClusterSize)
	LBASize      uint32  // 源磁盘逻辑扇区大小，单位字节
	PBASize      uint32  // 源磁盘物理扇区大小，单位字节
}

// indexEntry 描述一个 Cluster 在 vol 分卷中的存储位置。
//
// Size 为 0 表示该 Cluster 尚未写入（视为空，读取时回落到 backing 或全零）。
type indexEntry struct {
	VolID  uint32 // Cluster 所属 vol 分卷编号
	Offset uint64 // Cluster 在对应 vol 分卷中的起始字节偏移
	Size   uint32 // Cluster 在 vol 中占用的字节数（0 = 未写入）
}

const (
	indexHeaderSize = 512 // Header 区固定大小
	indexEntrySize  = 16  // VolID(4) + Offset(8) + Size(4)
)

var indexMagic = [4]byte{'H', 'K', 'I', '1'}

// NewIndex 构造磁盘划分为固定 clusterSize 的 Index，索引区预分配 N 项定长槽位。
func NewIndex(diskSize, clusterSize, lbaSize, pbaSize uint64) *Index {
	n := (diskSize + clusterSize - 1) / clusterSize
	return &Index{
		header: indexHeader{
			Magic:        indexMagic,
			DiskSize:     diskSize,
			ClusterSize:  clusterSize,
			ClusterCount: n,
			LBASize:      uint32(lbaSize),
			PBASize:      uint32(pbaSize),
		},
		entries: make([]indexEntry, n),
	}
}

// Header 返回 Index 的只读 Header 副本。
func (idx *Index) Header() indexHeader { return idx.header }

// ClusterCount 返回 Cluster 总数量 N。
func (idx *Index) ClusterCount() uint64 { return idx.header.ClusterCount }

// DiskSize 返回源磁盘总大小。
func (idx *Index) DiskSize() uint64 { return idx.header.DiskSize }

// ClusterSize 返回 Cluster 大小。
func (idx *Index) ClusterSize() uint64 { return idx.header.ClusterSize }

// EntryAt 返回第 i 个 Cluster 的索引项。
func (idx *Index) EntryAt(i uint64) indexEntry { return idx.entries[i] }

// SetEntry 更新第 i 个 Cluster 的索引项。
func (idx *Index) SetEntry(i uint64, e indexEntry) { idx.entries[i] = e }

// IsWritten 报告第 i 个 Cluster 是否已写入。
func (idx *Index) IsWritten(i uint64) bool { return idx.entries[i].Size != 0 }

// maxVolID 返回当前已使用到的最大 vol 编号（vol 数量 = maxVolID+1）。
func (idx *Index) maxVolID() uint32 {
	var max uint32
	for _, e := range idx.entries {
		if e.VolID > max {
			max = e.VolID
		}
	}
	return max
}

// readIndexHeader 从 r 读取并解析 Index 头。
func readIndexHeader(r io.Reader) (indexHeader, error) {
	buf := make([]byte, indexHeaderSize)
	if _, err := io.ReadFull(r, buf); err != nil {
		return indexHeader{}, err
	}
	var h indexHeader
	copy(h.Magic[:], buf[0:4])
	h.DiskSize = binary.BigEndian.Uint64(buf[4:12])
	h.ClusterSize = binary.BigEndian.Uint64(buf[12:20])
	h.ClusterCount = binary.BigEndian.Uint64(buf[20:28])
	h.LBASize = binary.BigEndian.Uint32(buf[28:32])
	h.PBASize = binary.BigEndian.Uint32(buf[32:36])
	if h.Magic != indexMagic {
		return indexHeader{}, fmt.Errorf("invalid index magic: %x", h.Magic)
	}
	return h, nil
}

// writeIndexHeader 把 Index 头序列化为 512 字节并写入 w。
func writeIndexHeader(w io.Writer, h indexHeader) error {
	buf := make([]byte, indexHeaderSize)
	copy(buf[0:4], h.Magic[:])
	binary.BigEndian.PutUint64(buf[4:12], h.DiskSize)
	binary.BigEndian.PutUint64(buf[12:20], h.ClusterSize)
	binary.BigEndian.PutUint64(buf[20:28], h.ClusterCount)
	binary.BigEndian.PutUint32(buf[28:32], h.LBASize)
	binary.BigEndian.PutUint32(buf[32:36], h.PBASize)
	_, err := w.Write(buf)
	return err
}

// readIndex 从 r 读取完整 Index（Header + 定长索引区）。
func readIndex(r io.Reader) (*Index, error) {
	h, err := readIndexHeader(r)
	if err != nil {
		return nil, err
	}
	entries := make([]indexEntry, h.ClusterCount)
	entryBuf := make([]byte, indexEntrySize)
	for i := uint64(0); i < h.ClusterCount; i++ {
		if _, err := io.ReadFull(r, entryBuf); err != nil {
			return nil, fmt.Errorf("read index entry %d: %w", i, err)
		}
		entries[i] = indexEntry{
			VolID:  binary.BigEndian.Uint32(entryBuf[0:4]),
			Offset: binary.BigEndian.Uint64(entryBuf[4:12]),
			Size:   binary.BigEndian.Uint32(entryBuf[12:16]),
		}
	}
	return &Index{header: h, entries: entries}, nil
}

// writeTo 把 Index 序列化写入 w，返回写入总字节数。
func (idx *Index) writeTo(w io.Writer) (int64, error) {
	if err := writeIndexHeader(w, idx.header); err != nil {
		return 0, err
	}
	written := int64(indexHeaderSize)
	entryBuf := make([]byte, indexEntrySize)
	for _, e := range idx.entries {
		binary.BigEndian.PutUint32(entryBuf[0:4], e.VolID)
		binary.BigEndian.PutUint64(entryBuf[4:12], e.Offset)
		binary.BigEndian.PutUint32(entryBuf[12:16], e.Size)
		if _, err := w.Write(entryBuf); err != nil {
			return written, err
		}
		written += indexEntrySize
	}
	return written, nil
}

// clusterLen 返回第 i 个 Cluster 覆盖的磁盘字节数（最后一个可能不满）。
func clusterLen(i, clusterSize, diskSize uint64) uint64 {
	start := i * clusterSize
	if start >= diskSize {
		return 0
	}
	end := start + clusterSize
	if end > diskSize {
		end = diskSize
	}
	return end - start
}
