package vimg

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
)

/*********************** Manager *************************/

type manager struct{}

func NewManager() Manager {
	return &manager{}
}

func (m *manager) Create(opts CreateOptions) (*VImg, error) {
	guid := "vimg_" + uuid.New().String()
	base := filepath.Join(opts.Dir, guid)

	metaPath := base + ".META"
	dataPath := base + ".DATA"
	idxPath := base + ".IDX"

	if err := os.MkdirAll(opts.Dir, 0755); err != nil {
		return nil, err
	}

	dataFile, err := os.Create(dataPath)
	if err != nil {
		return nil, err
	}
	dataFile.Close()

	idxFile, err := os.Create(idxPath)
	if err != nil {
		return nil, err
	}
	idxFile.Close()

	v := &VImg{
		Guid:        guid,
		VirtualSize: opts.VirtualSize,
		ClusterSize: opts.ClusterSize,
		Layout:      LayoutFile,
		State:       StateCreated,
		Compression: opts.Compression,
		Encryption:  opts.Encryption,
		StorageType: StorageTypeFilesystem,
	}

	info := map[string]any{
		"version":  1,
		"filePath": metaPath,
	}
	infoJSON, _ := json.Marshal(info)
	v.StoragePrivateInfo = string(infoJSON)

	if err := writeJSON(metaPath, v); err != nil {
		return nil, err
	}

	return v, nil
}

func (m *manager) CreateFromBacking(opts CreateFromBackingOptions) (*VImg, error) {
	v, err := m.Create(opts.CreateOptions)
	if err != nil {
		return nil, err
	}

	v.BackingGuid = opts.BackingGuid
	metaPath, err := getMetaPath(v)
	if err != nil {
		return nil, err
	}
	return v, writeJSON(metaPath, v)
}

func (m *manager) Open(metaPath string) (*Image, error) {
	v := &VImg{}
	if err := readJSON(metaPath, v); err != nil {
		return nil, err
	}

	dataPath := replaceExt(metaPath, ".DATA")
	idxPath := replaceExt(metaPath, ".IDX")

	dataFile, err := os.OpenFile(dataPath, os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}

	idxFile, err := os.OpenFile(idxPath, os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}

	img := &image{
		meta:     v,
		dataFile: dataFile,
		idxFile:  idxFile,
		index:    make(map[uint64]IndexEntry),
	}

	if err := img.loadIndex(); err != nil {
		return nil, err
	}

	if v.BackingGuid != "" {
		backingMeta := guessMetaFromGuid(metaPath, v.BackingGuid)
		backingImg, err := m.Open(backingMeta)
		if err != nil {
			return nil, err
		}

		if bi, ok := (*backingImg).(*image); ok {
			img.backing = bi
		} else {
			return nil, errors.New("invalid backing image type")
		}
	}

	var i Image = img
	return &i, nil
}

func (m *manager) Delete(metaPath string) error {
	if len(metaPath) < 5 {
		return errors.New("invalid meta path")
	}
	base := metaPath[:len(metaPath)-5]
	os.Remove(base + ".DATA")
	os.Remove(base + ".IDX")
	os.Remove(base + ".META")
	return nil
}

/*********************** Image *************************/

type image struct {
	meta     *VImg
	dataFile *os.File
	idxFile  *os.File

	index map[uint64]IndexEntry
	mu    sync.RWMutex

	backing *image
}

func (img *image) Info() *VImg {
	return img.meta
}

func (img *image) Close() error {
	img.dataFile.Close()
	img.idxFile.Close()
	if img.backing != nil {
		img.backing.Close()
	}
	return nil
}

/*********************** Write *************************/

func (img *image) WriteAt(p []byte, off uint64) error {
	img.mu.Lock()
	defer img.mu.Unlock()

	clusterSize := uint64(img.meta.ClusterSize)

	for len(p) > 0 {
		idx := off / clusterSize
		inner := off % clusterSize

		writeLen := min(uint64(len(p)), clusterSize-inner)

		buf := make([]byte, clusterSize)

		if err := img.readCluster(idx, buf); err != nil {
			return err
		}

		copy(buf[inner:], p[:writeLen])

		if err := img.writeCluster(idx, buf); err != nil {
			return err
		}

		p = p[writeLen:]
		off += writeLen
	}

	return nil
}

func (img *image) writeCluster(index uint64, data []byte) error {
	offset, err := img.dataFile.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}

	if _, err := img.dataFile.Write(data); err != nil {
		return err
	}

	entry := IndexEntry{
		ClusterIndex: index,
		OffsetInDATA: uint64(offset),
		LengthInDATA: uint32(len(data)),
	}

	if err := writeStruct(img.idxFile, &entry); err != nil {
		return err
	}

	if err := img.dataFile.Sync(); err != nil {
		return err
	}
	if err := img.idxFile.Sync(); err != nil {
		return err
	}

	img.index[index] = entry
	return nil
}

/*********************** Read *************************/

func (img *image) ReadAt(p []byte, off uint64) error {
	img.mu.RLock()
	defer img.mu.RUnlock()

	clusterSize := uint64(img.meta.ClusterSize)

	for len(p) > 0 {
		idx := off / clusterSize
		inner := off % clusterSize

		readLen := min(uint64(len(p)), clusterSize-inner)

		buf := make([]byte, clusterSize)

		if err := img.readCluster(idx, buf); err != nil {
			return err
		}

		copy(p[:readLen], buf[inner:inner+readLen])

		p = p[readLen:]
		off += readLen
	}

	return nil
}

func (img *image) readCluster(index uint64, buf []byte) error {
	if entry, ok := img.index[index]; ok {
		data := make([]byte, entry.LengthInDATA)

		if _, err := img.dataFile.ReadAt(data, int64(entry.OffsetInDATA)); err != nil {
			return err
		}

		copy(buf, data)
		return nil
	}

	if img.backing != nil {
		return img.backing.readCluster(index, buf)
	}

	for i := range buf {
		buf[i] = 0
	}
	return nil
}

/*********************** Commit *************************/

func (img *image) Commit() error {
	if img.backing == nil {
		return errors.New("no backing")
	}

	img.mu.RLock()
	defer img.mu.RUnlock()

	img.backing.mu.Lock()
	defer img.backing.mu.Unlock()

	for idx := range img.index {
		buf := make([]byte, img.meta.ClusterSize)

		if err := img.readCluster(idx, buf); err != nil {
			return err
		}

		if err := img.backing.writeCluster(idx, buf); err != nil {
			return err
		}
	}

	// 重新加载父镜像索引，确保其内存中的 index map 是最新的
	return img.backing.loadIndexNoLock()
}

/*********************** Rebase *************************/

func (img *image) Rebase(newBackingMeta string) error {
	newImg, err := NewManager().Open(newBackingMeta)
	if err != nil {
		return err
	}

	newBacking, ok := (*newImg).(*image)
	if !ok {
		return errors.New("invalid new backing image type")
	}

	img.mu.Lock()
	defer img.mu.Unlock()

	for idx := range img.index {
		buf := make([]byte, img.meta.ClusterSize)

		if err := img.readCluster(idx, buf); err != nil {
			return err
		}

		if err := img.writeCluster(idx, buf); err != nil {
			return err
		}
	}

	img.backing = newBacking
	img.meta.BackingGuid = newBacking.meta.Guid

	metaPath, err := getMetaPath(img.meta)
	if err != nil {
		return err
	}
	return writeJSON(metaPath, img.meta)
}

/*********************** Index *************************/

func (img *image) loadIndex() error {
	img.mu.Lock()
	defer img.mu.Unlock()
	return img.loadIndexNoLock()
}

func (img *image) loadIndexNoLock() error {
	img.index = make(map[uint64]IndexEntry)

	stat, err := img.idxFile.Stat()
	if err != nil {
		return err
	}

	size := stat.Size()
	var offset int64

	for offset < size {
		var e IndexEntry

		if err := readStruct(img.idxFile, &e, offset); err != nil {
			return err
		}

		img.index[e.ClusterIndex] = e
		offset += int64(binary.Size(e))
	}

	return nil
}

/*********************** Utils *************************/

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func writeStruct(w io.Writer, v any) error {
	return binary.Write(w, binary.LittleEndian, v)
}

func readStruct(r io.ReaderAt, v any, off int64) error {
	buf := make([]byte, binary.Size(v))

	if _, err := r.ReadAt(buf, off); err != nil {
		return err
	}

	return binary.Read(bytesReader(buf), binary.LittleEndian, v)
}

func bytesReader(b []byte) *byteReader {
	return &byteReader{b: b}
}

type byteReader struct {
	b []byte
	i int
}

func (r *byteReader) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}

func min(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

func replaceExt(p, ext string) string {
	if len(p) < 5 {
		return p + ext
	}
	return p[:len(p)-5] + ext
}

func getMetaPath(v *VImg) (string, error) {
	if v == nil {
		return "", errors.New("vimg is nil")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(v.StoragePrivateInfo), &m); err != nil {
		return "", fmt.Errorf("failed to unmarshal StoragePrivateInfo: %w", err)
	}
	path, ok := m["filePath"].(string)
	if !ok {
		return "", errors.New("filePath not found in StoragePrivateInfo or not a string")
	}
	return path, nil
}

func guessMetaFromGuid(curMeta, guid string) string {
	dir := filepath.Dir(curMeta)
	return filepath.Join(dir, guid+".META")
}
