package ntfs

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"unicode/utf16"

	"github.com/davecgh/go-spew/spew"
	"github.com/kisun-bit/drpkg/define"
	"github.com/kisun-bit/drpkg/disk/filesystem/bitmap"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
)

type BitmapParser struct {
	dev   string
	start int64
	size  int64
	fr    *extend.FsRegionReader

	boot       bootSector
	mftRuns    []dataRun
	bitmapRuns []dataRun
}

func NewBitmapParser(dev string, start int64, size int64) (bitmap.FsBitmapParser, error) {
	fr, e := extend.NewFsRegionReader(dev, start, size)
	if e != nil {
		return nil, e
	}
	return &BitmapParser{dev: dev, start: start, size: size, fr: fr}, nil
}

func (p *BitmapParser) String() string {
	return fmt.Sprintf("<NTFSBitmapParser(dev=%s,start=%d,size=%d)>",
		p.dev, p.start, p.size)
}

func (p *BitmapParser) Dump() (*bitmap.FsBitmap, error) {
	defer func() {
		if p.fr != nil {
			_ = p.fr.Close()
		}
	}()

	if err := p.parseBoot(); err != nil {
		return nil, err
	}
	logger.Debugf("%s.Dump() BootSector:\n%s", p, spew.Sdump(p.boot))

	if err := p.loadBitmapRuns(); err != nil {
		return nil, err
	}
	logger.Debugf("%s.Dump() BitmapRuns:\n%s", p, spew.Sdump(p.bitmapRuns))

	clusterSize := p.boot.clusterSize()

	// 计算 $Bitmap 属性数据总长度（字节）
	bitmapLen := int64(0)
	for _, run := range p.bitmapRuns {
		bitmapLen += run.lengthClusters * clusterSize
	}
	logger.Debugf("%s.Dump() BitmapLen: %v", p, bitmapLen)

	// 读取所有 run 的数据到 bitmapBytes
	bitmapBytes := make([]byte, bitmapLen)
	destOffset := int64(0) // 目标端：bitmapBytes 里的写入位置
	for _, run := range p.bitmapRuns {
		length := run.lengthClusters * clusterSize

		if run.sparse {
			// 稀疏 run 对应的簇全部视为 0（空闲），
			// Go 的 make([]byte, n) 默认已经是全 0，跳过读取即可
			destOffset += length
			continue
		}

		srcOffset := run.startCluster * clusterSize // 源端：设备内偏移
		if _, e := p.fr.ReadAt(bitmapBytes[destOffset:destOffset+length], srcOffset); e != nil {
			return nil, e
		}
		destOffset += length
	}

	// 校验：位图字节数不应小于按簇数算出来的理论大小
	totalClusters := p.boot.totalCluster()
	needBytes := (totalClusters + 7) / 8
	if int64(len(bitmapBytes)) < needBytes {
		return nil, fmt.Errorf(
			"ntfs bitmap: bitmap data too small: got %d bytes, need at least %d for %d clusters",
			len(bitmapBytes), needBytes, totalClusters,
		)
	}

	// 按位解析每个簇的占用状态
	var used int64
	for blockNo := int64(0); blockNo < totalClusters; blockNo++ {
		byteIdx := blockNo / 8
		bitIdx := uint(blockNo % 8)
		bit := (bitmapBytes[byteIdx] >> bitIdx) & 1
		if bit == 1 {
			used++
		}
	}

	//usedBytes := used * clusterSize
	//totalBytes := totalClusters * clusterSize
	//logger.Debugf("%s.Dump() blocks=%d, bs=%d, used=%dB ( %s / %s )",
	//	p, totalClusters, clusterSize, usedBytes, humanize.IBytes(uint64(usedBytes)), humanize.IBytes(uint64(totalBytes)))

	return &bitmap.FsBitmap{
		Type:       define.FsTypeNTFS,
		BitmapKind: bitmap.BitmapFromFS,
		Bitmap:     bitmapBytes,
		Bits:       totalClusters,
		BlockSize:  int(clusterSize),
	}, nil
}

func (p *BitmapParser) parseBoot() error {
	buf := make([]byte, 512)
	if _, err := p.fr.Read(buf); err != nil && err != io.EOF {
		return fmt.Errorf("ntfs: read boot sector: %v", err)
	}
	bps := uint32(binary.LittleEndian.Uint16(buf[0x0B:]))
	spc := uint32(buf[0x0D])
	if spc == 0 {
		return fmt.Errorf("ntfs: invalid BPB (bytes/sector=%d sectors/cluster=%d)", bps, spc)
	}
	// H4: a bytes-per-sector value of 0 or 1 makes sectorEnd-2 negative in
	// applyFixup; a non-power-of-two confuses sector arithmetic. Require a
	// power of two in [512, 64KiB].
	if !isPowerOfTwo(bps) || bps < minBytesPerSector {
		return fmt.Errorf("ntfs: invalid bytes-per-sector %d (must be a power of two >= %d)",
			bps, minBytesPerSector)
	}

	totalSects := binary.LittleEndian.Uint64(buf[0x28:])
	mftClus := binary.LittleEndian.Uint64(buf[0x30:])

	b := bootSector{
		bytesPerSector:    bps,
		sectorsPerCluster: spc,
		mftCluster:        mftClus,
		totalSectors:      int64(totalSects),
	}
	b.mftRecordSize = decodeClustersPerRecord(int8(buf[0x40]), b.clusterSize())
	b.indexRecordSize = decodeClustersPerRecord(int8(buf[0x44]), b.clusterSize())
	// M1: clamp record sizes to a sane ceiling; a signed-shift byte can
	// otherwise produce a ~2 GiB record size that OOMs the make() in
	// readFileRecordAt / the INDX walk.
	if b.mftRecordSize == 0 || b.mftRecordSize > maxRecordSize {
		return fmt.Errorf("ntfs: invalid MFT record size %d", b.mftRecordSize)
	}
	if b.indexRecordSize > maxRecordSize {
		return fmt.Errorf("ntfs: invalid index record size %d", b.indexRecordSize)
	}
	p.boot = b
	return nil
}

// loadMFTRuns reads MFT record 0 ($MFT) to recover its $DATA runlist so
// the reader can locate any MFT record even on a fragmented volume. $MFT
// record 0 is always at $MftClusterNumber and is self-describing.
func (p *BitmapParser) loadMFTRuns() error {
	// A hostile $MftClusterNumber (e.g. 0xFFFF...FF) would make the byte
	// offset negative or overflow; compute it with the same guards
	// mftRecordOffset uses so a corrupt boot sector yields an error rather
	// than a negative ReadAt offset (and a slice-bounds panic).
	off, ok := p.mftRecordOffset(mftRecordMFT)
	if !ok {
		return fmt.Errorf("ntfs: $MFT record offset unreachable")
	}
	rec, err := p.readFileRecordAt(off)
	if err != nil {
		return fmt.Errorf("ntfs: read $MFT record: %v", err)
	}
	for _, a := range rec.attrs {
		if a.typeCode == attrData && a.name == "" {
			if a.nonResident {
				p.mftRuns = a.runs
				return nil
			}
		}
	}
	return fmt.Errorf("ntfs: $MFT has no non-resident $DATA")
}

func (p *BitmapParser) loadBitmapRuns() error {
	// A hostile $MftClusterNumber (e.g. 0xFFFF...FF) would make the byte
	// offset negative or overflow; compute it with the same guards
	// mftRecordOffset uses so a corrupt boot sector yields an error rather
	// than a negative ReadAt offset (and a slice-bounds panic).
	off, ok := p.mftRecordOffset(mftRecordBitmap)
	if !ok {
		return fmt.Errorf("ntfs: $BITMAP record offset unreachable")
	}
	rec, err := p.readFileRecordAt(off)
	if err != nil {
		return fmt.Errorf("ntfs: read $MFT record: %v", err)
	}
	for _, a := range rec.attrs {
		if a.typeCode == attrData && a.name == "" {
			if a.nonResident {
				p.bitmapRuns = a.runs
				return nil
			}
		}
	}
	return fmt.Errorf("ntfs: $BITMAP has no non-resident $DATA")
}

// readFileRecordAt reads one FILE record at an absolute byte offset,
// applies the update-sequence-array fixup and parses its attributes.
func (p *BitmapParser) readFileRecordAt(off int64) (*fileRecord, error) {
	buf := make([]byte, p.boot.mftRecordSize)
	if _, err := p.fr.ReadAt(buf, off); err != nil && err != io.EOF {
		return nil, err
	}
	return p.parseFileRecord(buf)
}

func (p *BitmapParser) parseFileRecord(buf []byte) (*fileRecord, error) {
	if len(buf) < 0x30 {
		return nil, fmt.Errorf("ntfs: short FILE record")
	}
	if string(buf[0:4]) != "FILE" {
		return nil, fmt.Errorf("ntfs: bad FILE magic %q", buf[0:4])
	}
	usaOffset := binary.LittleEndian.Uint16(buf[0x04:])
	usaCount := binary.LittleEndian.Uint16(buf[0x06:])
	if err := applyFixup(buf, usaOffset, usaCount, p.boot.bytesPerSector); err != nil {
		return nil, fmt.Errorf("ntfs: FILE fixup: %v", err)
	}

	flags := binary.LittleEndian.Uint16(buf[0x16:])
	firstAttr := binary.LittleEndian.Uint16(buf[0x14:])

	fr := &fileRecord{flags: flags}
	off := int(firstAttr)
	for off+4 <= len(buf) {
		typeCode := binary.LittleEndian.Uint32(buf[off:])
		if typeCode == attrEnd {
			break
		}
		if off+8 > len(buf) {
			break
		}
		recLen := int(binary.LittleEndian.Uint32(buf[off+4:]))
		if recLen <= 0 || off+recLen > len(buf) {
			return nil, fmt.Errorf("ntfs: bad attribute length %d at %d", recLen, off)
		}
		a, err := p.parseAttribute(buf[off : off+recLen])
		if err != nil {
			return nil, err
		}
		fr.attrs = append(fr.attrs, a)
		off += recLen
	}
	return fr, nil
}

// parseAttribute decodes one attribute record (header + body).
func (p *BitmapParser) parseAttribute(buf []byte) (attribute, error) {
	var a attribute
	// C1: the fixed-offset reads below (buf[8], buf[10:], buf[0x10:],
	// buf[0x14:]) require a full resident attribute header. The caller only
	// guarantees recLen>0, so a short attribute would panic without this.
	if err := CheckBounds(0, 0x18, len(buf)); err != nil {
		return a, fmt.Errorf("ntfs: short attribute header: %w", err)
	}
	a.typeCode = binary.LittleEndian.Uint32(buf[0:])
	nonResident := buf[8]
	nameLen := int(buf[9])
	nameOff := int(binary.LittleEndian.Uint16(buf[10:]))
	a.flags = binary.LittleEndian.Uint16(buf[0x0C:])
	a.attrID = binary.LittleEndian.Uint16(buf[0x0E:])
	if nameLen > 0 {
		if name, err := Slice(buf, nameOff, nameLen*2); err == nil {
			a.name = decodeUTF16(name)
		}
	}
	if nonResident == 0 {
		// Resident attribute.
		contentLen := int(binary.LittleEndian.Uint32(buf[0x10:]))
		contentOff := int(binary.LittleEndian.Uint16(buf[0x14:]))
		content, err := Slice(buf, contentOff, contentLen)
		if err != nil {
			return a, fmt.Errorf("ntfs: resident attr content out of range: %w", err)
		}
		a.residentData = append([]byte(nil), content...)
		a.realSize = uint64(contentLen)
		return a, nil
	}
	// Non-resident attribute. The runlist-offset (0x20) and real-size
	// (0x30) fields live in the extended (non-resident) header, so require
	// at least 0x40 bytes before reading them.
	if err := CheckBounds(0, 0x40, len(buf)); err != nil {
		return a, fmt.Errorf("ntfs: short non-resident attribute header: %w", err)
	}
	a.nonResident = true
	a.startVCN = binary.LittleEndian.Uint64(buf[0x10:])
	a.lastVCN = binary.LittleEndian.Uint64(buf[0x18:])
	a.compUnit = binary.LittleEndian.Uint16(buf[0x22:])
	// The real-size field (0x30) is only meaningful on the first fragment
	// (startVCN 0); later attribute-list fragments repeat the allocated size
	// but resolveAttributes takes realSize from the VCN-0 fragment.
	a.realSize = binary.LittleEndian.Uint64(buf[0x30:])
	runOff := int(binary.LittleEndian.Uint16(buf[0x20:]))
	runData, err := Slice(buf, runOff, len(buf)-runOff)
	if err != nil {
		return a, fmt.Errorf("ntfs: bad runlist offset: %w", err)
	}
	runs, err := decodeRunList(runData)
	if err != nil {
		return a, err
	}
	a.runs = runs
	return a, nil
}

// mftRecordOffset maps a record number to its absolute byte offset using
// the $MFT runlist. Returns (offset, true) when the record is reachable.
func (p *BitmapParser) mftRecordOffset(recNo uint64) (int64, bool) {
	// Bytes into the $MFT $DATA stream where this record begins. recNo and
	// mftRecordSize are both bounded (record size <= 1 MiB), but compute the
	// product overflow-safely anyway.
	target, ok := mulCheck(int64(recNo), int64(p.boot.mftRecordSize))
	if !ok {
		return 0, false
	}
	cs := p.boot.clusterSize()
	// Special-case: before loadMFTRuns populates mftRuns we still need
	// record 0; it sits at mftCluster. A hostile $MftClusterNumber must not
	// produce a negative or overflowing byte offset.
	if p.mftRuns == nil {
		base, ok := mulCheck(int64(p.boot.mftCluster), cs)
		if !ok || int64(p.boot.mftCluster) < 0 {
			return 0, false
		}
		abs, ok := addCheck(base, target)
		if !ok {
			return 0, false
		}
		//abs, ok = addCheck(abs, p.start)
		//if !ok || abs < 0 {
		//	return 0, false
		//}
		return abs, true
	}
	var streamPos int64 // byte position of the start of the current run
	for _, run := range p.mftRuns {
		// M4: overflow-safe run arithmetic. A malicious $MFT runlist could
		// otherwise drive lengthClusters*cs or startCluster*cs negative and
		// misdirect a record read.
		runBytes, ok := mulCheck(run.lengthClusters, cs)
		if !ok {
			return 0, false
		}
		streamEnd, ok := addCheck(streamPos, runBytes)
		if !ok {
			return 0, false
		}
		if target < streamEnd {
			if run.sparse {
				return 0, false
			}
			within := target - streamPos
			base, ok := mulCheck(run.startCluster, cs)
			if !ok {
				return 0, false
			}
			abs, ok := addCheck(base, within)
			if !ok {
				return 0, false
			}
			//abs, ok = addCheck(abs, p.start)
			//if !ok || abs < 0 {
			//	return 0, false
			//}
			return abs, true
		}
		streamPos = streamEnd
	}
	return 0, false
}

// mulCheck returns a*b and whether the product fit in int64 without
// overflow (treating a*b as a non-negative byte count: a negative operand
// or a wrapped result reports !ok).
func mulCheck(a, b int64) (int64, bool) {
	if a < 0 || b < 0 {
		return 0, false
	}
	if a == 0 || b == 0 {
		return 0, true
	}
	p := a * b
	if p/b != a || p < 0 {
		return 0, false
	}
	return p, true
}

// addCheck returns a+b and whether the sum fit in int64 without overflow.
func addCheck(a, b int64) (int64, bool) {
	s := a + b
	if (b > 0 && s < a) || (b < 0 && s > a) {
		return 0, false
	}
	return s, true
}

// isPowerOfTwo reports whether v is a non-zero power of two.
func isPowerOfTwo(v uint32) bool { return v != 0 && v&(v-1) == 0 }

// decodeClustersPerRecord interprets the signed "clusters per record"
// byte used for both MFT records (0x40) and index records (0x44). A
// positive value is a cluster count; a negative value v means the record
// is 2^(-v) bytes (the common case when a record is smaller than a
// cluster, e.g. -10 => 1024 bytes).
func decodeClustersPerRecord(v int8, clusterSize int64) uint32 {
	if v >= 0 {
		// M1: positive cluster counts are bounded by the caller clamping
		// the result to maxRecordSize, but guard the multiply against
		// overflow so a huge clusterSize cannot wrap into a small value.
		sz := int64(v) * clusterSize
		if clusterSize != 0 && sz/clusterSize != int64(v) {
			return 0 // overflow → rejected by the caller's size check
		}
		if sz < 0 || sz > math.MaxUint32 {
			return 0
		}
		return uint32(sz)
	}
	// M2: a negative byte v means 2^(-v) bytes. Restrict the shift to
	// [9,16] (512 B .. 64 KiB records); anything outside that range (e.g.
	// -31 => 2 GiB) is rejected with a zero the caller treats as invalid.
	shift := -int(v)
	if shift < 9 || shift > 16 {
		return 0
	}
	return uint32(int64(1) << uint(shift))
}

// applyFixup performs the NTFS update-sequence-array correction. The
// USA's first word is the update sequence number; the remaining words
// are the original last two bytes of each sector. On disk, the last two
// bytes of every sector in the record hold the USN; this routine
// verifies that and restores the saved bytes.
func applyFixup(buf []byte, usaOffset, usaCount uint16, bytesPerSector uint32) error {
	if usaCount == 0 {
		return nil
	}
	uo := int(usaOffset)
	if err := CheckBounds(uo, int(usaCount)*2, len(buf)); err != nil {
		return fmt.Errorf("USA out of range: %v", err)
	}
	usn := buf[uo : uo+2]
	sectors := int(usaCount) - 1
	for i := 0; i < sectors; i++ {
		sectorEnd := (i + 1) * int(bytesPerSector)
		// H4: a bytes-per-sector of 0/1 (or any sector ending before its
		// own trailing word) makes sectorEnd-2 negative; reject it rather
		// than panic. parseBoot already enforces bps>=512, but INDX blocks
		// reach here through the same path so keep the guard local.
		if sectorEnd < 2 || sectorEnd > len(buf) {
			return fmt.Errorf("fixup sector %d beyond record", i)
		}
		tail := buf[sectorEnd-2 : sectorEnd]
		if tail[0] != usn[0] || tail[1] != usn[1] {
			return fmt.Errorf("fixup mismatch in sector %d", i)
		}
		saved := buf[uo+2+i*2 : uo+2+i*2+2]
		tail[0] = saved[0]
		tail[1] = saved[1]
	}
	return nil
}

// CheckBounds verifies that the half-open range [off, off+n) lies entirely
// within a buffer of the given length, i.e. off >= 0 && n >= 0 &&
// off+n <= length. The sum is computed in int64 so it cannot wrap on a
// 64-bit platform, defeating class (C) overflow tricks such as
// off = maxint, n = 1.
func CheckBounds(off, n, length int) error {
	if off < 0 {
		return fmt.Errorf("safeio: negative offset %d", off)
	}
	if n < 0 {
		return fmt.Errorf("safeio: negative length %d", n)
	}
	if length < 0 {
		return fmt.Errorf("safeio: negative buffer length %d", length)
	}
	// Compute off+n in int64. On a 64-bit platform int is int64, so the sum
	// itself can wrap (e.g. off=MaxInt, n=1); detect that explicitly.
	end := int64(off) + int64(n)
	if end < int64(off) || end > int64(length) {
		return fmt.Errorf("safeio: range [%d,%d+%d) exceeds buffer length %d",
			off, off, n, length)
	}
	return nil
}

// Slice returns buf[off:off+n] after a [CheckBounds] validation, so a
// malformed offset/length yields an error rather than a slice-bounds panic
// (class C). The returned slice aliases buf; callers that need an
// independent copy must copy it themselves.
func Slice(buf []byte, off, n int) ([]byte, error) {
	if err := CheckBounds(off, n, len(buf)); err != nil {
		return nil, err
	}
	return buf[off : off+n], nil
}

// decodeRunList decodes an NTFS data-run list into extents. Each run
// begins with a header byte: low nibble = byte count of the length
// field, high nibble = byte count of the (signed, relative) offset
// field. A zero header terminates the list. A run with a zero-length
// offset field is sparse (a hole).
func decodeRunList(buf []byte) ([]dataRun, error) {
	var runs []dataRun
	var prevLCN int64
	i := 0
	// A runlist cannot have more runs than it has bytes (each run is at
	// least 2 bytes: a non-zero header + one length byte); bound the walk
	// so a degenerate buffer cannot spin.
	guard := NewLoopGuard(len(buf) + 1)
	for i < len(buf) {
		if err := guard.Next(); err != nil {
			return nil, fmt.Errorf("ntfs: runlist: %w", err)
		}
		header := buf[i]
		if header == 0 {
			break
		}
		i++
		lenBytes := int(header & 0x0F)
		offBytes := int(header >> 4)
		if lenBytes == 0 || i+lenBytes+offBytes > len(buf) {
			return nil, fmt.Errorf("ntfs: malformed data run")
		}
		length := int64(readUintLE(buf[i : i+lenBytes]))
		i += lenBytes
		// H3: bound the cluster count so an 8-byte length of 0x7FFF...FF
		// cannot feed H1/H2. A negative (top-bit-set) length is also
		// rejected.
		if length < 0 || length > maxRunLengthClusters {
			return nil, fmt.Errorf("ntfs: data run length %d out of range", length)
		}

		run := dataRun{lengthClusters: length}
		if offBytes == 0 {
			run.sparse = true
			run.startCluster = -1
		} else {
			delta := readIntLE(buf[i : i+offBytes])
			i += offBytes
			prevLCN += delta
			// H3: a non-sparse run must reference a non-negative cluster;
			// reject a runlist that decodes to a negative LCN so the
			// startCluster*cs arithmetic downstream stays non-negative.
			if prevLCN < 0 {
				return nil, fmt.Errorf("ntfs: data run negative start cluster %d", prevLCN)
			}
			run.startCluster = prevLCN
		}
		runs = append(runs, run)
	}
	return runs, nil
}

// readUintLE reads up to 8 little-endian bytes as an unsigned integer.
func readUintLE(b []byte) uint64 {
	var v uint64
	for i := len(b) - 1; i >= 0; i-- {
		v = v<<8 | uint64(b[i])
	}
	return v
}

// readIntLE reads up to 8 little-endian bytes as a sign-extended signed
// integer (used for run offset deltas, which are relative and signed).
func readIntLE(b []byte) int64 {
	if len(b) == 0 {
		return 0
	}
	var v int64
	for i := len(b) - 1; i >= 0; i-- {
		v = v<<8 | int64(b[i])
	}
	// Sign-extend from the top bit of the most significant byte.
	shift := uint(64 - 8*len(b))
	v = (v << shift) >> shift
	return v
}

// decodeUTF16 decodes little-endian UTF-16 bytes to a Go string.
func decodeUTF16(b []byte) string {
	n := len(b) / 2
	u := make([]uint16, n)
	for i := 0; i < n; i++ {
		u[i] = binary.LittleEndian.Uint16(b[i*2:])
	}
	return string(utf16.Decode(u))
}

// LoopGuard bounds the number of iterations of a chain or tree walk where a
// full visited-set is overkill (e.g. a FAT cluster chain or an extent
// chain). Construct it with [NewLoopGuard] and call [LoopGuard.Next] once
// per iteration; after max successful calls the next call returns
// [ErrLoopLimit]. The zero value is not usable; use NewLoopGuard.
type LoopGuard struct {
	max int
	n   int
}

// NewLoopGuard returns a LoopGuard that permits up to max iterations. A
// non-positive max means "no iterations are allowed": the first
// [LoopGuard.Next] returns [ErrLoopLimit], which is the safe default for an
// attacker-supplied or nonsensical bound.
func NewLoopGuard(max int) *LoopGuard {
	return &LoopGuard{max: max}
}

// Next records one iteration. It returns nil for the first max calls and
// [ErrLoopLimit] thereafter, so a malformed image that forms an unbounded
// or cyclic chain terminates the walk with an error instead of spinning
// forever.
func (g *LoopGuard) Next() error {
	if g.n >= g.max {
		return fmt.Errorf("loopLimit: %d", g.max)
	}
	g.n++
	return nil
}

// Count reports how many times [LoopGuard.Next] has returned nil so far.
func (g *LoopGuard) Count() int { return g.n }

func IntPow(x int, y int) (start int) {
	start = 1
	for i := 0; i < y; i++ {
		start *= x
	}
	return start
}
