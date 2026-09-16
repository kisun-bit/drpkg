package hkd

import (
	"bytes"
	"fmt"
	"io"

	"github.com/kisun-bit/drpkg/storage/backend"
)

// Create 在存储介质上创建新的 HKD 磁盘备份对象，返回可写的句柄。
//
// 磁盘被划分为 ClusterSize 大小的固定区间；index 预分配 N 项定长槽位。
// 主文件 {DiskID}.hkd 已写入，index 暂存内存，Close 时写回 {DiskID}.hki。
//
// backing 可选：传入已打开的父备份即为 overlay，未写入的 Cluster 读取时回落
// 到 backing。
func Create(acc backend.Accessor, opt Options, backing ...*HKD) (*HKD, error) {
	if acc == nil {
		return nil, fmt.Errorf("hkd: nil accessor")
	}
	if err := opt.validate(); err != nil {
		return nil, err
	}
	if opt.ClusterSize == 0 {
		return nil, fmt.Errorf("hkd: ClusterSize must be positive")
	}

	h := &HKD{
		acc:        acc,
		opt:        opt,
		base:       opt.basePath(),
		diskID:     opt.DiskID,
		writable:   true,
		volMaxSize: opt.volLimit(),
		index: NewIndex(
			opt.DiskSize,
			opt.ClusterSize,
			uint64(opt.LBASize),
			uint64(opt.PBASize),
		),
	}
	if len(backing) > 0 {
		h.backing = backing[0]
		h.backing.writable = false // backing 作为父层时只读
	}

	if err := h.writeMain(); err != nil {
		_ = h.cleanup()
		return nil, err
	}
	if err := h.openVolWrite(0); err != nil {
		_ = h.cleanup()
		return nil, err
	}
	return h, nil
}

// Open 打开存储介质上已有的 HKD 磁盘备份对象以供读取。
//
// opt 只需提供标识信息（用于定位路径）；磁盘属性与配置从主文件头与 index 读回
// 并回填。backing 用于 overlay 读取时回落。
func Open(acc backend.Accessor, opt Options, backing ...*HKD) (*HKD, error) {
	if acc == nil {
		return nil, fmt.Errorf("hkd: nil accessor")
	}
	if err := opt.validate(); err != nil {
		return nil, err
	}

	reader, ok := acc.(backend.OpenFile)
	if !ok {
		return nil, fmt.Errorf("hkd: accessor %s does not support reading", acc.Type())
	}

	h := &HKD{
		acc:    acc,
		opt:    opt,
		base:   opt.basePath(),
		diskID: opt.DiskID,
	}
	if len(backing) > 0 {
		h.backing = backing[0]
		h.backing.writable = false // backing 作为父层时只读
	}

	// 读主文件头并回填磁盘属性。
	mf, err := reader.OpenFile(h.joinBase(h.mainName()))
	if err != nil {
		return nil, fmt.Errorf("hkd: open main header: %w", err)
	}
	header, err := readHkdHeader(mf)
	_ = mf.Close()
	if err != nil {
		return nil, err
	}
	h.opt.DiskSize = header.DiskSize
	h.opt.ClusterSize = header.ClusterSize
	h.opt.LBASize = header.LBASize
	h.opt.PBASize = header.PBASize

	// 读 index。
	idxf, err := reader.OpenFile(h.joinBase(h.indexName()))
	if err != nil {
		return nil, fmt.Errorf("hkd: open index: %w", err)
	}
	idx, err := readIndex(idxf)
	_ = idxf.Close()
	if err != nil {
		return nil, err
	}
	h.index = idx
	return h, nil
}

// WriteAt 把 data 写入磁盘 offset 处（COW，类似 qcow2 覆盖写）。
//
// data 可能只覆盖某个 Cluster 的一部分，此时会读出旧数据合并形成新 Cluster；
// 也可能是完整的一个或多个连续 Cluster。生成的新 Cluster 若能在原槽放下则原地
// 覆盖，否则追加到当前 vol 末尾，旧的被废弃，由 Compact 回收。
func (h *HKD) WriteAt(offset uint64, data []byte) error {
	if h == nil || !h.writable || h.closed {
		return fmt.Errorf("hkd: backup is not writable")
	}
	if offset >= h.opt.DiskSize {
		return fmt.Errorf("hkd: offset %d exceeds disk size %d", offset, h.opt.DiskSize)
	}
	if uint64(len(data)) > h.opt.DiskSize-offset {
		return fmt.Errorf("hkd: write range [%d,%d) exceeds disk size %d",
			offset, offset+uint64(len(data)), h.opt.DiskSize)
	}

	cs := h.opt.ClusterSize
	start := offset / cs
	end := (offset + uint64(len(data)) - 1) / cs
	if end >= h.index.ClusterCount() {
		end = h.index.ClusterCount() - 1
	}

	for i := start; i <= end; i++ {
		clen := clusterLen(i, cs, h.opt.DiskSize)
		cstart := i * cs

		a := offset
		if cstart > a {
			a = cstart
		}
		b := offset + uint64(len(data))
		if cstart+clen < b {
			b = cstart + clen
		}
		if a >= b {
			continue
		}

		// 读旧原始数据（含 backing 回落）。
		old, found, err := h.readClusterRaw(i)
		if err != nil {
			return err
		}

		// 组装新 Cluster 原始数据。
		neu := make([]byte, clen)
		if found {
			copy(neu, old)
		}
		copy(neu[a-cstart:b-cstart], data[a-offset:b-offset])

		// 无变化则跳过。
		if found && bytes.Equal(old, neu) {
			continue
		}

		if err := h.writeClusterAt(i, neu); err != nil {
			return err
		}
	}
	return nil
}

// ReadAt 从磁盘 offset 处读取数据到 data（io.ReaderAt 语义）。
//
// 基于 offset 查询 Index 定位每个涉及的 Cluster，解压解密后拼接返回。
// 未写入的 Cluster 读取为全零（或从 backing 回落）。
func (h *HKD) ReadAt(offset uint64, data []byte) (int, error) {
	if h == nil {
		return 0, fmt.Errorf("hkd: nil HKD")
	}
	if offset >= h.opt.DiskSize {
		return 0, io.EOF
	}
	want := uint64(len(data))
	if offset+want > h.opt.DiskSize {
		want = h.opt.DiskSize - offset
	}
	if want == 0 {
		return 0, io.EOF
	}

	cs := h.opt.ClusterSize
	start := offset / cs
	end := (offset + want - 1) / cs

	read := uint64(0)
	for i := start; i <= end && read < want; i++ {
		clen := clusterLen(i, cs, h.opt.DiskSize)
		cstart := i * cs

		a := offset + read
		if cstart > a {
			a = cstart
		}
		b := offset + want
		if cstart+clen < b {
			b = cstart + clen
		}
		if a >= b {
			continue
		}

		raw, found, err := h.readClusterRaw(i)
		if err != nil {
			return int(read), err
		}
		if found {
			copy(data[a-offset:b-offset], raw[a-cstart:b-cstart])
		}
		// 未写入：data 保持零值（调用方缓冲区初始值）。
		read += b - a
	}

	// io.ReaderAt 约定：返回字节数小于 len(data) 时必须返回非 nil error。
	// 当 offset+len(data) 越过磁盘末尾时，即使读满了 clamp 后的 want，也应返回 EOF。
	if offset+uint64(len(data)) > h.opt.DiskSize {
		return int(read), io.EOF
	}
	return int(read), nil
}

// ClusterCount 返回 Cluster 总数 N。
func (h *HKD) ClusterCount() uint64 {
	if h == nil || h.index == nil {
		return 0
	}
	return h.index.ClusterCount()
}

// Compact 回收覆盖写产生的垃圾：把有效 Cluster 紧凑重写到新 vol，更新 Index，
// 并删除旧 vol。
func (h *HKD) Compact() error {
	if h == nil || !h.writable {
		return fmt.Errorf("hkd: backup is not writable")
	}
	if !h.opt.Compact {
		return nil
	}

	// 关闭当前 append vol。
	if h.volFile != nil {
		if err := h.volFile.Close(); err != nil {
			return err
		}
		h.volFile = nil
	}

	oldMax := h.index.maxVolID() + 1 // 旧 vol 数量

	var newVol uint32 = oldMax
	var newOff uint64
	var newVolFile *backend.StorageFile
	closeNew := func() error {
		if newVolFile != nil {
			if err := newVolFile.Close(); err != nil {
				return err
			}
			newVolFile = nil
		}
		return nil
	}

	for i := uint64(0); i < h.index.ClusterCount(); i++ {
		if !h.index.IsWritten(i) {
			continue
		}
		c, err := h.readCluster(i)
		if err != nil {
			_ = closeNew()
			return err
		}
		size := c.BinaryStructSize()

		if newVolFile == nil || (newOff > 0 && newOff+size > h.volMaxSize) {
			if err := closeNew(); err != nil {
				return err
			}
			newVol++
			f, err := h.acc.CreateFile(h.joinBase(h.volName(newVol)))
			if err != nil {
				return err
			}
			newVolFile = f
			newOff = 0
		}

		if _, err := newVolFile.Seek(int64(newOff), io.SeekStart); err != nil {
			_ = closeNew()
			return err
		}
		if err := c.Write(newVolFile); err != nil {
			_ = closeNew()
			return err
		}
		h.index.SetEntry(i, indexEntry{VolID: newVol, Offset: newOff, Size: uint32(size)})
		newOff += size
	}
	if err := closeNew(); err != nil {
		return err
	}

	// 删除旧 vol（编号 0..oldMax-1）。
	for v := uint32(0); v < oldMax; v++ {
		if err := h.acc.RemoveAll(h.joinBase(h.volName(v))); err != nil {
			return err
		}
	}

	// 重置写状态到新 vol 之后。
	h.curVol = h.index.maxVolID() + 1
	h.volOff = 0
	h.volFile = nil

	return h.writeIndex()
}

// Close 完成备份：关闭当前 vol 并写回 index。
func (h *HKD) Close() error {
	if h == nil || h.closed {
		return nil
	}
	h.closed = true

	if h.volFile != nil {
		if err := h.volFile.Close(); err != nil {
			return err
		}
		h.volFile = nil
	}
	if h.writable {
		return h.writeIndex()
	}
	return nil
}

// readClusterRaw 读取第 i 个 Cluster 的原始数据（已解压解密）。
//
// found=false 表示该 Cluster 未写入（且无 backing），此时返回 nil。
func (h *HKD) readClusterRaw(i uint64) (raw []byte, found bool, err error) {
	if h.index.IsWritten(i) {
		c, err := h.readCluster(i)
		if err != nil {
			return nil, false, err
		}
		raw, err := c.GetRawData()
		if err != nil {
			return nil, false, err
		}
		return raw, true, nil
	}
	if h.backing != nil {
		return h.backing.readClusterRaw(i)
	}
	return nil, false, nil
}

// readCluster 读取第 i 个 Cluster 的二进制结构（不解码）。
func (h *HKD) readCluster(i uint64) (*Cluster, error) {
	entry := h.index.EntryAt(i)

	// 目标 Cluster 若位于当前正在写入的分卷，则其数据仍在本地写缓冲里
	// （如 S3 的临时文件尚未上传），通过 OpenFile 读不到；改为直接从缓冲
	// 句柄读取，读完后恢复到追加位置，避免打断后续写入。
	if h.volFile != nil && entry.VolID == h.curVol {
		if _, err := h.volFile.Seek(int64(entry.Offset), io.SeekStart); err != nil {
			return nil, err
		}
		c, err := ReadCluster(h.volFile)
		if err != nil {
			return nil, fmt.Errorf("hkd: read cluster %d from vol %d offset %d: %w",
				i, entry.VolID, entry.Offset, err)
		}
		if _, err := h.volFile.Seek(int64(h.volOff), io.SeekStart); err != nil {
			return nil, err
		}
		return c, nil
	}

	reader, ok := h.acc.(backend.OpenFile)
	if !ok {
		return nil, fmt.Errorf("hkd: accessor %s does not support reading", h.acc.Type())
	}
	f, err := reader.OpenFile(h.joinBase(h.volName(entry.VolID)))
	if err != nil {
		return nil, fmt.Errorf("hkd: open vol %d: %w", entry.VolID, err)
	}
	defer f.Close()
	if _, err := f.Seek(int64(entry.Offset), io.SeekStart); err != nil {
		return nil, err
	}
	c, err := ReadCluster(f)
	if err != nil {
		return nil, fmt.Errorf("hkd: read cluster %d from vol %d offset %d: %w",
			i, entry.VolID, entry.Offset, err)
	}
	return c, nil
}

// writeClusterAt 写入第 i 个 Cluster 的原始数据，并更新 Index。
//
// 能原地覆盖（旧 Cluster 在当前 vol 且新二进制不超旧槽位）则原地覆盖，
// 否则追加到当前 vol 末尾。
func (h *HKD) writeClusterAt(i uint64, rawData []byte) error {
	cluster, err := CreateCluster(i*h.opt.ClusterSize, rawData, h.opt.Compress, h.opt.Encrypt, h.opt.Check)
	if err != nil {
		return err
	}
	size := cluster.BinaryStructSize()
	old := h.index.EntryAt(i)

	if h.opt.Compact && old.Size != 0 && old.VolID == h.curVol && h.volFile != nil && size <= uint64(old.Size) {
		if _, err := h.volFile.Seek(int64(old.Offset), io.SeekStart); err != nil {
			return err
		}
		if err := cluster.Write(h.volFile); err != nil {
			return err
		}
		if _, err := h.volFile.Seek(int64(h.volOff), io.SeekStart); err != nil {
			return err
		}
		h.index.SetEntry(i, indexEntry{VolID: old.VolID, Offset: old.Offset, Size: uint32(size)})
		return nil
	}

	if err := h.ensureVolForAppend(size); err != nil {
		return err
	}
	off := h.volOff
	if err := cluster.Write(h.volFile); err != nil {
		return err
	}
	h.volOff += size
	h.index.SetEntry(i, indexEntry{VolID: h.curVol, Offset: off, Size: uint32(size)})
	return nil
}

// ensureVolForAppend 确保当前 append vol 存在且能容纳 size 字节。
func (h *HKD) ensureVolForAppend(size uint64) error {
	if h.volFile == nil {
		return h.openVolWrite(h.curVol)
	}
	if h.volOff > 0 && h.volOff+size > h.volMaxSize {
		if err := h.volFile.Close(); err != nil {
			return err
		}
		h.volFile = nil
		return h.openVolWrite(h.curVol + 1)
	}
	return nil
}

// openVolWrite 打开第 volID 个分卷用于追加写入。
func (h *HKD) openVolWrite(volID uint32) error {
	f, err := h.acc.CreateFile(h.joinBase(h.volName(volID)))
	if err != nil {
		return fmt.Errorf("hkd: open vol %d: %w", volID, err)
	}
	h.volFile = f
	h.curVol = volID
	h.volOff = 0
	return nil
}

// writeMain 把主文件头写入 {DiskID}.hkd。
func (h *HKD) writeMain() error {
	f, err := h.acc.CreateFile(h.joinBase(h.mainName()))
	if err != nil {
		return fmt.Errorf("hkd: create main header: %w", err)
	}
	defer f.Close()
	if err := writeHkdHeader(f, h.opt.toHeader()); err != nil {
		return fmt.Errorf("hkd: write main header: %w", err)
	}
	return nil
}

// writeIndex 把完整 Index 写入 {DiskID}.hki。
func (h *HKD) writeIndex() error {
	f, err := h.acc.CreateFile(h.joinBase(h.indexName()))
	if err != nil {
		return fmt.Errorf("hkd: create index: %w", err)
	}
	defer f.Close()
	if _, err := h.index.writeTo(f); err != nil {
		return fmt.Errorf("hkd: write index: %w", err)
	}
	return nil
}

// cleanup 清理 Create 失败时已写入的文件。
func (h *HKD) cleanup() error {
	if h.volFile != nil {
		_ = h.volFile.Close()
		h.volFile = nil
	}
	for _, name := range []string{h.mainName(), h.indexName()} {
		_ = h.acc.RemoveAll(h.joinBase(name))
	}
	for volID := uint32(0); volID <= h.curVol; volID++ {
		_ = h.acc.RemoveAll(h.joinBase(h.volName(volID)))
	}
	return nil
}

// Commit 把当前层（overlay）的增量数据合并写回 backing，成功后清空当前层。
//
// backing 必须尚未 Close；commit 期间会临时恢复 backing 可写。
func (h *HKD) Commit() error {
	if h == nil || !h.writable {
		return fmt.Errorf("hkd: backup is not writable")
	}
	if h.backing == nil {
		return fmt.Errorf("hkd: no backing to commit")
	}
	if h.backing.closed {
		return fmt.Errorf("hkd: backing is closed, cannot commit")
	}

	cs := h.opt.ClusterSize
	saved := h.backing.writable
	h.backing.writable = true
	for i := uint64(0); i < h.index.ClusterCount(); i++ {
		if !h.index.IsWritten(i) {
			continue
		}
		raw, err := h.readClusterOwn(i)
		if err != nil {
			h.backing.writable = saved
			return err
		}
		if err := h.backing.WriteAt(i*cs, raw); err != nil {
			h.backing.writable = saved
			return fmt.Errorf("hkd: commit cluster %d to backing: %w", i, err)
		}
	}
	h.backing.writable = saved

	if h.volFile != nil {
		if err := h.volFile.Close(); err != nil {
			return err
		}
		h.volFile = nil
	}

	// 清空当前层：删除 vol、重置 index 与写状态。
	if err := h.removeAllVols(); err != nil {
		return err
	}
	h.index = NewIndex(h.opt.DiskSize, h.opt.ClusterSize, uint64(h.opt.LBASize), uint64(h.opt.PBASize))
	h.curVol = 0
	h.volOff = 0
	h.volFile = nil
	return h.writeIndex()
}

// Release 删除当前层在存储介质上的所有文件（main/index/vol），并使句柄失效。
func (h *HKD) Release() error {
	if h == nil {
		return fmt.Errorf("hkd: nil HKD")
	}
	if h.volFile != nil {
		_ = h.volFile.Close()
		h.volFile = nil
	}
	if err := h.removeAllVols(); err != nil {
		return err
	}
	for _, name := range []string{h.mainName(), h.indexName()} {
		if err := h.acc.RemoveAll(h.joinBase(name)); err != nil {
			return err
		}
	}
	h.closed = true
	return nil
}

// Rebase 把当前层（overlay）的 backing 重新指向 newBacking。
func (h *HKD) Rebase(newBacking *HKD) {
	h.backing = newBacking
	if newBacking != nil {
		newBacking.writable = false // 父层只读
	}
}

// readClusterOwn 读取当前层自己写入的 Cluster 原始数据（不回落到 backing）。
func (h *HKD) readClusterOwn(i uint64) ([]byte, error) {
	c, err := h.readCluster(i)
	if err != nil {
		return nil, err
	}
	return c.GetRawData()
}

// removeAllVols 删除当前层所有 vol 文件。
func (h *HKD) removeAllVols() error {
	for v := uint32(0); v <= h.index.maxVolID(); v++ {
		if err := h.acc.RemoveAll(h.joinBase(h.volName(v))); err != nil {
			return err
		}
	}
	return nil
}
