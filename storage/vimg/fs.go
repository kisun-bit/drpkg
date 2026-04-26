package vimg

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
)

const (
	clusterFrameMagic      uint32 = 0x474D4956 // "VIMG" (little-endian)
	clusterFrameVersion    uint8  = 1
	clusterFrameHeaderSize        = 24

	clusterFrameFlagCompressed uint8 = 1 << 0
	clusterFrameFlagEncrypted  uint8 = 1 << 1

	aes256KeySize = 32
)

type storagePrivateInfo struct {
	Version         int    `json:"version"`
	FilePath        string `json:"filePath"`
	BackingFilePath string `json:"backingFilePath,omitempty"`
	EncryptionKey   string `json:"encryptionKey,omitempty"` // base64-encoded 32-byte key
}

/*********************** Manager *************************/

type manager struct{}

func NewManager() Manager {
	return &manager{}
}

func (m *manager) Create(opts CreateOptions) (*VImg, error) {
	if err := validateCreateOptions(opts); err != nil {
		return nil, err
	}

	guid := "vimg_" + strings.ReplaceAll(uuid.New().String(), "-", "")
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
	if err := dataFile.Close(); err != nil {
		return nil, err
	}

	idxFile, err := os.Create(idxPath)
	if err != nil {
		return nil, err
	}
	if err := idxFile.Close(); err != nil {
		return nil, err
	}

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

	key, err := resolveEncryptionKey(opts.Encryption, opts.EncryptionKey)
	if err != nil {
		return nil, err
	}

	info := storagePrivateInfo{
		Version:  1,
		FilePath: metaPath,
	}
	if len(key) > 0 {
		info.EncryptionKey = base64.StdEncoding.EncodeToString(key)
	}
	if err := setStoragePrivateInfo(v, info); err != nil {
		return nil, err
	}

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

	metaPath, err := getMetaPath(v)
	if err != nil {
		return nil, err
	}

	backingMeta := strings.TrimSpace(opts.BackingMetaPath)
	if backingMeta == "" && strings.TrimSpace(opts.BackingGuid) != "" {
		backingMeta = guessMetaFromGuid(metaPath, opts.BackingGuid)
	}
	if backingMeta == "" {
		return nil, errors.New("backingMetaPath or backingGuid is required")
	}
	backingMeta = resolveMetaPath(metaPath, backingMeta)

	backingMetaInfo := &VImg{}
	if err := readJSON(backingMeta, backingMetaInfo); err != nil {
		return nil, fmt.Errorf("failed to open backing meta %q: %w", backingMeta, err)
	}
	if opts.BackingGuid != "" && opts.BackingGuid != backingMetaInfo.Guid {
		return nil, fmt.Errorf("backing guid mismatch: expected %s, got %s", opts.BackingGuid, backingMetaInfo.Guid)
	}
	if backingMetaInfo.ClusterSize != v.ClusterSize {
		return nil, fmt.Errorf("backing cluster size mismatch: child=%d backing=%d", v.ClusterSize, backingMetaInfo.ClusterSize)
	}

	v.BackingGuid = backingMetaInfo.Guid

	info, err := getStoragePrivateInfo(v)
	if err != nil {
		return nil, err
	}
	info.BackingFilePath = backingMeta
	if err := setStoragePrivateInfo(v, info); err != nil {
		return nil, err
	}

	return v, writeJSON(metaPath, v)
}

func (m *manager) Open(metaPath string) (*Image, error) {
	img, err := m.open(metaPath, map[string]struct{}{})
	if err != nil {
		return nil, err
	}

	var i Image = img
	return &i, nil
}

func (m *manager) open(metaPath string, opening map[string]struct{}) (*image, error) {
	absMeta := metaPath
	if p, err := filepath.Abs(metaPath); err == nil {
		absMeta = p
	}

	if _, ok := opening[absMeta]; ok {
		return nil, fmt.Errorf("detected backing cycle at %s", absMeta)
	}
	opening[absMeta] = struct{}{}
	defer delete(opening, absMeta)

	v := &VImg{}
	if err := readJSON(absMeta, v); err != nil {
		return nil, err
	}

	info, err := getStoragePrivateInfo(v)
	if err != nil {
		return nil, err
	}

	key, err := decodeStoredEncryptionKey(v, info)
	if err != nil {
		return nil, err
	}

	dataPath := replaceExt(absMeta, ".DATA")
	idxPath := replaceExt(absMeta, ".IDX")

	dataFile, err := os.OpenFile(dataPath, os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}

	idxFile, err := os.OpenFile(idxPath, os.O_RDWR, 0644)
	if err != nil {
		_ = dataFile.Close()
		return nil, err
	}

	img := &image{
		meta:          v,
		dataFile:      dataFile,
		idxFile:       idxFile,
		index:         make(map[uint64]IndexEntry),
		encryptionKey: key,
	}

	if err := img.loadIndex(); err != nil {
		_ = img.Close()
		return nil, err
	}

	if v.BackingGuid != "" {
		backingMeta := strings.TrimSpace(info.BackingFilePath)
		if backingMeta == "" {
			backingMeta = guessMetaFromGuid(absMeta, v.BackingGuid)
		}
		backingMeta = resolveMetaPath(absMeta, backingMeta)

		backingImg, err := m.open(backingMeta, opening)
		if err != nil {
			_ = img.Close()
			return nil, err
		}
		if backingImg.meta.Guid != v.BackingGuid {
			_ = backingImg.Close()
			_ = img.Close()
			return nil, fmt.Errorf("backing guid mismatch: expected %s, got %s", v.BackingGuid, backingImg.meta.Guid)
		}
		img.backing = backingImg
	}

	return img, nil
}

func (m *manager) Delete(metaPath string) error {
	if len(metaPath) < 5 {
		return errors.New("invalid meta path")
	}
	base := metaPath[:len(metaPath)-5]
	_ = os.Remove(base + ".DATA")
	_ = os.Remove(base + ".IDX")
	_ = os.Remove(base + ".META")
	return nil
}

/*********************** Image *************************/

type image struct {
	meta     *VImg
	dataFile *os.File
	idxFile  *os.File

	index map[uint64]IndexEntry
	mu    sync.RWMutex

	backing       *image
	encryptionKey []byte
}

func (img *image) Info() *VImg {
	return img.meta
}

func (img *image) Backing() (*BackingRef, error) {
	img.mu.Lock()
	defer img.mu.Unlock()

	if strings.TrimSpace(img.meta.BackingGuid) == "" {
		return nil, nil
	}

	info, err := getStoragePrivateInfo(img.meta)
	if err != nil {
		return nil, err
	}

	curMeta := info.FilePath
	backingMeta := strings.TrimSpace(info.BackingFilePath)
	if backingMeta == "" {
		if strings.TrimSpace(curMeta) == "" {
			curMeta, err = getMetaPath(img.meta)
			if err != nil {
				return nil, err
			}
		}
		backingMeta = guessMetaFromGuid(curMeta, img.meta.BackingGuid)
	} else if strings.TrimSpace(curMeta) != "" {
		backingMeta = resolveMetaPath(curMeta, backingMeta)
	}

	return &BackingRef{
		Guid:     img.meta.BackingGuid,
		MetaPath: backingMeta,
	}, nil
}

func (img *image) Close() error {
	var firstErr error
	if img.dataFile != nil {
		if err := img.dataFile.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if img.idxFile != nil {
		if err := img.idxFile.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if img.backing != nil {
		if err := img.backing.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

/*********************** Write *************************/

func (img *image) WriteAt(p []byte, off uint64) error {
	if len(p) == 0 {
		return nil
	}

	if err := img.validateRWRange(off, len(p)); err != nil {
		return err
	}

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
	if len(data) != int(img.meta.ClusterSize) {
		return fmt.Errorf("invalid cluster payload length: got %d, want %d", len(data), img.meta.ClusterSize)
	}

	stored, err := img.encodeCluster(index, data)
	if err != nil {
		return err
	}

	offset, err := img.dataFile.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}

	if _, err := img.dataFile.Write(stored); err != nil {
		return err
	}

	if _, err := img.idxFile.Seek(0, io.SeekEnd); err != nil {
		return err
	}

	entry := IndexEntry{
		ClusterIndex: index,
		OffsetInDATA: uint64(offset),
		LengthInDATA: uint32(len(stored)),
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

func (img *image) encodeCluster(index uint64, data []byte) ([]byte, error) {
	payload := append([]byte(nil), data...)
	flags := uint8(0)
	nonce := []byte(nil)

	switch img.meta.Compression {
	case CompressionNone:
	case CompressionLZ4:
		return nil, errors.New("compression LZ4 is not implemented yet")
	default:
		return nil, fmt.Errorf("unsupported compression algorithm: %d", img.meta.Compression)
	}

	switch img.meta.Encryption {
	case EncryptionNone:
	case EncryptionAES256:
		var err error
		payload, nonce, err = encryptAES256GCM(img.encryptionKey, index, payload)
		if err != nil {
			return nil, err
		}
		flags |= clusterFrameFlagEncrypted
	default:
		return nil, fmt.Errorf("unsupported encryption algorithm: %d", img.meta.Encryption)
	}

	if flags == 0 {
		return payload, nil
	}

	header := make([]byte, clusterFrameHeaderSize)
	binary.LittleEndian.PutUint32(header[0:4], clusterFrameMagic)
	header[4] = clusterFrameVersion
	header[5] = flags
	binary.LittleEndian.PutUint32(header[8:12], uint32(len(data)))
	binary.LittleEndian.PutUint32(header[12:16], crc32.ChecksumIEEE(payload))
	binary.LittleEndian.PutUint16(header[16:18], uint16(len(nonce)))
	binary.LittleEndian.PutUint32(header[20:24], uint32(len(payload)))

	stored := make([]byte, 0, len(header)+len(nonce)+len(payload))
	stored = append(stored, header...)
	stored = append(stored, nonce...)
	stored = append(stored, payload...)
	return stored, nil
}

/*********************** Read *************************/

func (img *image) ReadAt(p []byte, off uint64) error {
	if len(p) == 0 {
		return nil
	}

	if err := img.validateRWRange(off, len(p)); err != nil {
		return err
	}

	img.mu.Lock()
	defer img.mu.Unlock()

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
	if !img.clusterIndexInRange(index) {
		fillZero(buf)
		return nil
	}

	found, err := img.readLocalCluster(index, buf)
	if err != nil {
		return err
	}
	if found {
		return nil
	}

	if img.backing != nil {
		return img.backing.readClusterWithLock(index, buf)
	}

	fillZero(buf)
	return nil
}

func (img *image) readClusterWithLock(index uint64, buf []byte) error {
	img.mu.Lock()
	defer img.mu.Unlock()
	return img.readCluster(index, buf)
}

func (img *image) readLocalCluster(index uint64, buf []byte) (bool, error) {
	entry, ok := img.index[index]
	if !ok {
		// 其他进程/句柄可能已追加写入 IDX，这里在 miss 时做一次刷新。
		if err := img.loadIndexNoLock(); err != nil {
			return false, err
		}
		entry, ok = img.index[index]
		if !ok {
			return false, nil
		}
	}

	data := make([]byte, entry.LengthInDATA)
	if _, err := img.dataFile.ReadAt(data, int64(entry.OffsetInDATA)); err != nil {
		return true, err
	}

	plain, err := img.decodeCluster(index, data)
	if err != nil {
		return true, err
	}

	fillZero(buf)
	if len(plain) > len(buf) {
		return true, fmt.Errorf("decoded cluster size too large: got %d, buf=%d", len(plain), len(buf))
	}
	copy(buf, plain)
	return true, nil
}

func (img *image) decodeCluster(index uint64, stored []byte) ([]byte, error) {
	if len(stored) < clusterFrameHeaderSize {
		return append([]byte(nil), stored...), nil
	}

	magic := binary.LittleEndian.Uint32(stored[0:4])
	if magic != clusterFrameMagic {
		return append([]byte(nil), stored...), nil
	}

	version := stored[4]
	if version != clusterFrameVersion {
		return nil, fmt.Errorf("unsupported cluster frame version: %d", version)
	}

	flags := stored[5]
	rawSize := binary.LittleEndian.Uint32(stored[8:12])
	dataCRC := binary.LittleEndian.Uint32(stored[12:16])
	nonceLen := binary.LittleEndian.Uint16(stored[16:18])
	payloadLen := binary.LittleEndian.Uint32(stored[20:24])

	requiredLen := clusterFrameHeaderSize + int(nonceLen) + int(payloadLen)
	if len(stored) != requiredLen {
		return nil, fmt.Errorf("invalid cluster frame length: got %d, expected %d", len(stored), requiredLen)
	}

	nonceOff := clusterFrameHeaderSize
	payloadOff := nonceOff + int(nonceLen)

	nonce := stored[nonceOff:payloadOff]
	payload := stored[payloadOff:]

	if crc32.ChecksumIEEE(payload) != dataCRC {
		return nil, errors.New("cluster payload crc32 mismatch")
	}

	plain := append([]byte(nil), payload...)

	if flags&clusterFrameFlagEncrypted != 0 {
		var err error
		plain, err = decryptAES256GCM(img.encryptionKey, index, nonce, plain)
		if err != nil {
			return nil, err
		}
	}

	if flags&clusterFrameFlagCompressed != 0 {
		return nil, errors.New("compression LZ4 is not implemented yet")
	}

	if len(plain) != int(rawSize) {
		return nil, fmt.Errorf("decoded raw size mismatch: got %d, expected %d", len(plain), rawSize)
	}
	return plain, nil
}

/*********************** Map *************************/

func (img *image) Map(off, length uint64) ([]MapSegment, error) {
	if length == 0 {
		return []MapSegment{}, nil
	}
	if off >= img.meta.VirtualSize {
		return nil, fmt.Errorf("offset out of range: off=%d size=%d", off, img.meta.VirtualSize)
	}
	end := off + length
	if end < off || end > img.meta.VirtualSize {
		return nil, fmt.Errorf("range out of bounds: off=%d len=%d size=%d", off, length, img.meta.VirtualSize)
	}

	img.mu.Lock()
	defer img.mu.Unlock()

	if err := img.loadIndexNoLock(); err != nil {
		return nil, err
	}

	clusterSize := uint64(img.meta.ClusterSize)
	topGuid := img.meta.Guid
	segments := make([]MapSegment, 0, 8)

	for pos := off; pos < end; {
		clusterIndex := pos / clusterSize
		clusterStart := clusterIndex * clusterSize
		clusterEnd := clusterStart + clusterSize
		if clusterEnd > img.meta.VirtualSize {
			clusterEnd = img.meta.VirtualSize
		}
		if clusterEnd > end {
			clusterEnd = end
		}

		source, ownerGuid, err := img.resolveMapSourceNoLock(clusterIndex, topGuid)
		if err != nil {
			return nil, err
		}

		seg := MapSegment{
			Offset:    pos,
			Length:    clusterEnd - pos,
			Source:    source,
			OwnerGuid: ownerGuid,
		}

		if n := len(segments); n > 0 &&
			segments[n-1].Offset+segments[n-1].Length == seg.Offset &&
			segments[n-1].Source == seg.Source &&
			segments[n-1].OwnerGuid == seg.OwnerGuid {
			segments[n-1].Length += seg.Length
		} else {
			segments = append(segments, seg)
		}

		pos = clusterEnd
	}

	return segments, nil
}

func (img *image) resolveMapSourceNoLock(clusterIndex uint64, topGuid string) (MapSource, string, error) {
	if !img.clusterIndexInRange(clusterIndex) {
		return MapSourceZero, "", nil
	}

	if _, ok := img.index[clusterIndex]; !ok {
		// 避免多句柄下 map 看到过期索引。
		if err := img.loadIndexNoLock(); err != nil {
			return MapSourceZero, "", err
		}
	}
	if _, ok := img.index[clusterIndex]; ok {
		if img.meta.Guid == topGuid {
			return MapSourceData, img.meta.Guid, nil
		}
		return MapSourceBacking, img.meta.Guid, nil
	}

	if img.backing == nil {
		return MapSourceZero, "", nil
	}
	return img.backing.resolveMapSourceWithLock(clusterIndex, topGuid)
}

func (img *image) resolveMapSourceWithLock(clusterIndex uint64, topGuid string) (MapSource, string, error) {
	img.mu.Lock()
	defer img.mu.Unlock()
	return img.resolveMapSourceNoLock(clusterIndex, topGuid)
}

/*********************** Commit *************************/

func (img *image) Commit() error {
	if img.backing == nil {
		return errors.New("no backing")
	}

	if img.meta.ClusterSize != img.backing.meta.ClusterSize {
		return errors.New("cluster size mismatch with backing")
	}

	img.mu.Lock()
	defer img.mu.Unlock()

	img.backing.mu.Lock()
	defer img.backing.mu.Unlock()

	for idx := range img.index {
		buf := make([]byte, img.meta.ClusterSize)

		found, err := img.readLocalCluster(idx, buf)
		if err != nil {
			return err
		}
		if !found {
			continue
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
	if newBacking.meta.Guid == img.meta.Guid {
		_ = newBacking.Close()
		return errors.New("cannot rebase image to itself")
	}
	if newBacking.hasGuidInChain(img.meta.Guid) {
		_ = newBacking.Close()
		return errors.New("rebase would create backing cycle")
	}
	if newBacking.meta.ClusterSize != img.meta.ClusterSize {
		_ = newBacking.Close()
		return errors.New("cluster size mismatch with new backing")
	}
	img.mu.Lock()
	defer img.mu.Unlock()

	clusterSize := uint64(img.meta.ClusterSize)
	totalClusters := img.meta.VirtualSize / clusterSize
	if img.meta.VirtualSize%clusterSize != 0 {
		totalClusters++
	}

	for idx := uint64(0); idx < totalClusters; idx++ {
		if _, ok := img.index[idx]; ok {
			// 本地已覆盖，不受 backing 变化影响。
			continue
		}

		oldBuf := make([]byte, img.meta.ClusterSize)
		newBuf := make([]byte, img.meta.ClusterSize)

		if img.backing != nil {
			if err := img.backing.readClusterWithLock(idx, oldBuf); err != nil {
				_ = newBacking.Close()
				return err
			}
		}
		if err := newBacking.readClusterWithLock(idx, newBuf); err != nil {
			_ = newBacking.Close()
			return err
		}

		if !byteSliceEqual(oldBuf, newBuf) {
			if err := img.writeCluster(idx, oldBuf); err != nil {
				_ = newBacking.Close()
				return err
			}
		}
	}

	oldBacking := img.backing
	img.backing = newBacking
	img.meta.BackingGuid = newBacking.meta.Guid

	info, err := getStoragePrivateInfo(img.meta)
	if err != nil {
		_ = newBacking.Close()
		return err
	}
	info.BackingFilePath = newBackingMeta
	if err := setStoragePrivateInfo(img.meta, info); err != nil {
		_ = newBacking.Close()
		return err
	}

	metaPath, err := getMetaPath(img.meta)
	if err != nil {
		_ = newBacking.Close()
		return err
	}
	if err := writeJSON(metaPath, img.meta); err != nil {
		_ = newBacking.Close()
		return err
	}

	if oldBacking != nil {
		_ = oldBacking.Close()
	}
	return nil
}

func (img *image) hasGuidInChain(guid string) bool {
	visited := map[string]struct{}{}
	cur := img
	for cur != nil {
		if cur.meta != nil && cur.meta.Guid == guid {
			return true
		}
		if cur.meta != nil {
			if _, ok := visited[cur.meta.Guid]; ok {
				return true
			}
			visited[cur.meta.Guid] = struct{}{}
		}
		cur = cur.backing
	}
	return false
}

func (img *image) clusterIndexInRange(index uint64) bool {
	clusterSize := uint64(img.meta.ClusterSize)
	if clusterSize == 0 {
		return false
	}
	if index > ^uint64(0)/clusterSize {
		return false
	}
	clusterStart := index * clusterSize
	return clusterStart < img.meta.VirtualSize
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

	entrySize := int64(binary.Size(IndexEntry{}))
	for offset+entrySize <= size {
		var e IndexEntry
		if err := readStruct(img.idxFile, &e, offset); err != nil {
			return err
		}

		img.index[e.ClusterIndex] = e
		offset += entrySize
	}

	// 忽略末尾残缺索引，避免崩溃恢复场景直接打开失败。
	return nil
}

/*********************** Utils *************************/

func validateCreateOptions(opts CreateOptions) error {
	if strings.TrimSpace(opts.Dir) == "" {
		return errors.New("dir is required")
	}
	if opts.VirtualSize == 0 {
		return errors.New("virtual size must be non-zero")
	}
	if opts.ClusterSize == 0 || opts.ClusterSize%512 != 0 {
		return errors.New("cluster size must be non-zero and 512-byte aligned")
	}
	switch opts.Compression {
	case CompressionNone:
	case CompressionLZ4:
		return errors.New("compression LZ4 is not implemented yet")
	default:
		return fmt.Errorf("unsupported compression algorithm: %d", opts.Compression)
	}
	switch opts.Encryption {
	case EncryptionNone, EncryptionAES256:
	default:
		return fmt.Errorf("unsupported encryption algorithm: %d", opts.Encryption)
	}
	return nil
}

func resolveEncryptionKey(enc Encryption, userInput string) ([]byte, error) {
	switch enc {
	case EncryptionNone:
		return nil, nil
	case EncryptionAES256:
		key, err := parseEncryptionKey(userInput)
		if err != nil {
			return nil, err
		}
		if len(key) == 0 {
			key = make([]byte, aes256KeySize)
			if _, err := rand.Read(key); err != nil {
				return nil, err
			}
		}
		return key, nil
	default:
		return nil, fmt.Errorf("unsupported encryption algorithm: %d", enc)
	}
}

func parseEncryptionKey(in string) ([]byte, error) {
	s := strings.TrimSpace(in)
	if s == "" {
		return nil, nil
	}

	// 64-char hex
	if len(s) == 64 {
		if b, err := hex.DecodeString(s); err == nil && len(b) == aes256KeySize {
			return b, nil
		}
	}

	// base64 encoded 32 bytes
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) == aes256KeySize {
		return b, nil
	}

	// raw 32-byte literal
	if len(s) == aes256KeySize {
		return []byte(s), nil
	}

	return nil, errors.New("invalid EncryptionKey: expected 32-byte raw key, 64-char hex, or base64-encoded 32-byte key")
}

func decodeStoredEncryptionKey(v *VImg, info storagePrivateInfo) ([]byte, error) {
	switch v.Encryption {
	case EncryptionNone:
		return nil, nil
	case EncryptionAES256:
		if strings.TrimSpace(info.EncryptionKey) == "" {
			return nil, errors.New("encryption key is missing in StoragePrivateInfo")
		}
		key, err := base64.StdEncoding.DecodeString(info.EncryptionKey)
		if err != nil {
			return nil, fmt.Errorf("invalid encryption key encoding: %w", err)
		}
		if len(key) != aes256KeySize {
			return nil, fmt.Errorf("invalid encryption key size: got %d, want %d", len(key), aes256KeySize)
		}
		return key, nil
	default:
		return nil, fmt.Errorf("unsupported encryption algorithm: %d", v.Encryption)
	}
}

func encryptAES256GCM(key []byte, clusterIndex uint64, plain []byte) ([]byte, []byte, error) {
	if len(key) != aes256KeySize {
		return nil, nil, fmt.Errorf("invalid AES-256 key size: %d", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}

	aad := make([]byte, 8)
	binary.LittleEndian.PutUint64(aad, clusterIndex)

	ciphertext := aead.Seal(nil, nonce, plain, aad)
	return ciphertext, nonce, nil
}

func decryptAES256GCM(key []byte, clusterIndex uint64, nonce []byte, ciphertext []byte) ([]byte, error) {
	if len(key) != aes256KeySize {
		return nil, fmt.Errorf("invalid AES-256 key size: %d", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(nonce) != aead.NonceSize() {
		return nil, fmt.Errorf("invalid nonce size: got %d, want %d", len(nonce), aead.NonceSize())
	}

	aad := make([]byte, 8)
	binary.LittleEndian.PutUint64(aad, clusterIndex)

	plain, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, err
	}
	return plain, nil
}

func (img *image) validateRWRange(off uint64, length int) error {
	if length < 0 {
		return errors.New("invalid read/write length")
	}
	if img.meta.ClusterSize == 0 {
		return errors.New("cluster size is zero")
	}
	if length == 0 {
		if off > img.meta.VirtualSize {
			return fmt.Errorf("offset out of range: off=%d size=%d", off, img.meta.VirtualSize)
		}
		return nil
	}
	if off >= img.meta.VirtualSize {
		return fmt.Errorf("offset out of range: off=%d size=%d", off, img.meta.VirtualSize)
	}
	end := off + uint64(length)
	if end < off || end > img.meta.VirtualSize {
		return fmt.Errorf("range out of bounds: off=%d len=%d size=%d", off, length, img.meta.VirtualSize)
	}
	return nil
}

func byteSliceEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func fillZero(buf []byte) {
	for i := range buf {
		buf[i] = 0
	}
}

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

	return binary.Read(bytes.NewReader(buf), binary.LittleEndian, v)
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
	info, err := getStoragePrivateInfo(v)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(info.FilePath) == "" {
		return "", errors.New("filePath not found in StoragePrivateInfo")
	}
	return info.FilePath, nil
}

func getStoragePrivateInfo(v *VImg) (storagePrivateInfo, error) {
	if v == nil {
		return storagePrivateInfo{}, errors.New("vimg is nil")
	}

	info := storagePrivateInfo{}
	if err := json.Unmarshal([]byte(v.StoragePrivateInfo), &info); err != nil {
		return storagePrivateInfo{}, fmt.Errorf("failed to unmarshal StoragePrivateInfo: %w", err)
	}
	if info.Version == 0 {
		info.Version = 1
	}
	return info, nil
}

func setStoragePrivateInfo(v *VImg, info storagePrivateInfo) error {
	if v == nil {
		return errors.New("vimg is nil")
	}
	if info.Version == 0 {
		info.Version = 1
	}
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	v.StoragePrivateInfo = string(data)
	return nil
}

func guessMetaFromGuid(curMeta, guid string) string {
	dir := filepath.Dir(curMeta)
	return filepath.Join(dir, guid+".META")
}

func resolveMetaPath(curMetaPath, target string) string {
	if filepath.IsAbs(target) {
		return target
	}
	return filepath.Join(filepath.Dir(curMetaPath), target)
}
