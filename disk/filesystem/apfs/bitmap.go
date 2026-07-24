// apfs_bitmap.go
package apfs

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/kisun-bit/drpkg/define"
	"github.com/kisun-bit/drpkg/disk/filesystem/bitmap"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
)

const (
	objPhysSize   = 32
	cpmMapEntSize = 32
	ciSize        = 32
	cibHeaderSize = 40 // ObjPhys(32) + CibIndex(4) + CibChunkInfoCount(4)
)

type BitmapParser struct {
	dev   string
	start int64
	size  int64
	fr    *extend.FsRegionReader
}

func NewBitmapParser(dev string, start int64, size int64) (bitmap.FsBitmapParser, error) {
	fr, e := extend.NewFsRegionReader(dev, start, size)
	if e != nil {
		return nil, e
	}
	return &BitmapParser{dev: dev, start: start, size: size, fr: fr}, nil
}

func (p *BitmapParser) String() string {
	return fmt.Sprintf("<APFSBitmapParser(dev=%s,start=%d,size=%d)>",
		p.dev, p.start, p.size)
}

// readAt reads exactly len(buf) bytes at absolute offset off (relative to
// the region's start, matching pread semantics in the C code).
func (p *BitmapParser) readAt(off int64, buf []byte) error {
	n, err := p.fr.ReadAt(buf, off)
	if err != nil {
		return fmt.Errorf("read error at offset %d (%d/%d bytes): %w", off, n, len(buf), err)
	}
	if n != len(buf) {
		return fmt.Errorf("short read at offset %d: got %d, expected %d", off, n, len(buf))
	}
	return nil
}

func decode(buf []byte, v interface{}) error {
	return binary.Read(bytes.NewReader(buf), binary.LittleEndian, v)
}

// readSuperblock reads and validates the nx_superblock_t at block 0.
func (p *BitmapParser) readSuperblock() (*NxSuperblock, error) {
	buf := make([]byte, minBlockSize)
	if err := p.readAt(0, buf); err != nil {
		return nil, fmt.Errorf("read error for superblock: %w", err)
	}

	var nxsb NxSuperblock
	if err := decode(buf, &nxsb); err != nil {
		return nil, fmt.Errorf("failed to decode nx_superblock: %w", err)
	}

	if nxsb.NxMagic != apfsMagic {
		return nil, fmt.Errorf("APFS MAGIC 0x%X mismatch (expected 0x%X)", nxsb.NxMagic, uint32(apfsMagic))
	}
	if nxsb.NxBlockSize != minBlockSize {
		return nil, fmt.Errorf("invalid or unsupported APFS block size: %d (expected %d)", nxsb.NxBlockSize, minBlockSize)
	}
	if nxsb.NxBlockCount == 0 || nxsb.NxBlockCount > maxTotalBlocks {
		return nil, fmt.Errorf("maliciously large or zero APFS block count: %d (max allowed: %d)", nxsb.NxBlockCount, maxTotalBlocks)
	}

	logger.Debugf("%s: APFS MAGIC 0x%X", p.dev, nxsb.NxMagic)
	return &nxsb, nil
}

// getSpacemanBuf walks the checkpoint descriptor area to find the latest
// checkpoint_map_phys_t, then locates and reads the spaceman object it
// points to. Mirrors get_spaceman_buf() in the C source.
func (p *BitmapParser) getSpacemanBuf(nxsb *NxSuperblock) ([]byte, error) {
	blockSize := int64(nxsb.NxBlockSize)
	base := nxsb.NxXpDescBase
	blocks := nxsb.NxXpDescBlocks

	if blockSize != minBlockSize {
		return nil, fmt.Errorf("invalid APFS block size in getSpacemanBuf: %d", blockSize)
	}
	if base < 0 || uint64(blocks) > uint64(math.MaxUint32)>>1 {
		return nil, fmt.Errorf("checkpoint descriptor area not supported (base or blocks too large)")
	}
	if blocks == 0 || blocks > maxXPDescBlocks {
		return nil, fmt.Errorf("maliciously large or zero xp_desc_blocks: %d (max allowed: %d)", blocks, maxXPDescBlocks)
	}

	typeCheckpointMap := uint32(objPhysical | objTypeCheckpointMap)
	typeSpaceman := uint32(objEphemeral | objTypeSpaceman)

	objBuf := make([]byte, blockSize)
	var cpmBuf []byte
	var cpmHeader CheckpointMapPhysHeader
	foundNxsb := false
	haveCpm := false

	for i := uint32(0); i < blocks; i++ {
		b := base + int64(i)
		if b > math.MaxInt64/blockSize {
			return nil, fmt.Errorf("potential overflow in pread offset calculation (base: %d, block_size: %d)", b, blockSize)
		}
		if err := p.readAt(b*blockSize, objBuf); err != nil {
			return nil, fmt.Errorf("pread error reading obj: %w", err)
		}

		var obj ObjPhys
		if err := decode(objBuf[:objPhysSize], &obj); err != nil {
			return nil, fmt.Errorf("failed to decode obj_phys: %w", err)
		}

		if obj.Type == nxsb.NxO.Type {
			// Sanity check: nxsb has a copy of the latest nx_superblock
			// if the filesystem was unmounted cleanly.
			if obj.Xid < nxsb.NxO.Xid {
				continue
			}
			if obj.Xid > nxsb.NxO.Xid {
				logger.Debugf("%s: newer nx_superblock found in descriptor", p.dev)
			}
			foundNxsb = true
		}

		if obj.Type == typeCheckpointMap {
			var hdr CheckpointMapPhysHeader
			if err := decode(objBuf[:cibHeaderSize-8], &hdr); err != nil { // ObjPhys(32)+flags(4)+count(4)=40; reuse cibHeaderSize const
				return nil, fmt.Errorf("failed to decode checkpoint_map_phys header: %w", err)
			}
			if !haveCpm || hdr.CpmO.Xid > cpmHeader.CpmO.Xid {
				cpmBuf = append([]byte(nil), objBuf...)
				cpmHeader = hdr
				haveCpm = true
			}
		}
	}

	if !foundNxsb {
		return nil, fmt.Errorf("nx_superblock not found in descriptor")
	}
	if !haveCpm {
		return nil, fmt.Errorf("checkpoint_map_phys not found in descriptor")
	}
	if cpmHeader.CpmFlags&checkpointMapLast == 0 {
		return nil, fmt.Errorf("multiple checkpoint_map_phys not supported")
	}

	maxEntries := uint32(blockSize-cibHeaderSize) / cpmMapEntSize
	if cpmHeader.CpmCount == 0 || cpmHeader.CpmCount > maxEntries {
		return nil, fmt.Errorf("maliciously large or zero cpm_count: %d", cpmHeader.CpmCount)
	}

	var spacemanBuf []byte
	for i := uint32(0); i < cpmHeader.CpmCount; i++ {
		off := cibHeaderSize + int(i)*cpmMapEntSize
		var mapping CheckpointMapping
		if err := decode(cpmBuf[off:off+cpmMapEntSize], &mapping); err != nil {
			return nil, fmt.Errorf("failed to decode checkpoint_mapping: %w", err)
		}

		if mapping.CpmOid == nxsb.NxSpacemanOid && mapping.CpmType == typeSpaceman {
			if mapping.CpmSize == 0 || mapping.CpmSize > maxSpacemanSize {
				return nil, fmt.Errorf("maliciously large or zero spaceman size: %d (max allowed: %d)", mapping.CpmSize, maxSpacemanSize)
			}
			if mapping.CpmPaddr < 0 || int64(mapping.CpmPaddr) > math.MaxInt64/blockSize {
				return nil, fmt.Errorf("potential overflow in pread offset for spaceman (paddr: %d, block_size: %d)", mapping.CpmPaddr, blockSize)
			}
			buf := make([]byte, mapping.CpmSize)
			if err := p.readAt(int64(mapping.CpmPaddr)*blockSize, buf); err != nil {
				return nil, fmt.Errorf("pread error reading spaceman: %w", err)
			}
			spacemanBuf = buf
			break
		}
	}

	if spacemanBuf == nil {
		return nil, fmt.Errorf("spaceman not found")
	}
	return spacemanBuf, nil
}

// buildBitmap walks the spaceman's chunk-info structures and clears bits
// for free blocks in an initially all-used bitmap. Mirrors read_bitmap().
func (p *BitmapParser) buildBitmap(nxsb *NxSuperblock, spacemanBuf []byte) (*bitmap.FsBitmap, error) {
	var sm SpacemanPhysHeader
	if err := decode(spacemanBuf, &sm); err != nil {
		return nil, fmt.Errorf("failed to decode spaceman_phys: %w", err)
	}

	if sm.SmBlockSize == 0 || sm.SmBlockSize != nxsb.NxBlockSize {
		return nil, fmt.Errorf("spaceman block size %d inconsistent with superblock %d", sm.SmBlockSize, nxsb.NxBlockSize)
	}

	blockSize := int64(nxsb.NxBlockSize)
	blocksPerChunk := uint64(sm.SmBlocksPerChunk)
	totalBlocks := nxsb.NxBlockCount

	if blocksPerChunk == 0 || blocksPerChunk > totalBlocks {
		return nil, fmt.Errorf("maliciously large or zero blocks_per_chunk: %d", blocksPerChunk)
	}

	fsb := bitmap.NewFsBitmap(define.FstypeApfs, bitmap.BitmapFromFS, int64(totalBlocks), int(blockSize))
	fsb.SetAll() // start "all used"; matches pc_init_bitmap(bitmap, 0xFF, ...)

	cibBuf := make([]byte, blockSize)
	bitmapEntryBuf := make([]byte, blockSize)

	for sd := 0; sd < sdCount; sd++ {
		dev := sm.SmDev[sd]
		smOffset := int64(dev.SmAddrOffset)

		var cntCount uint64
		if dev.SmCabCount > 0 {
			cntCount = uint64(dev.SmCabCount)
		} else {
			cntCount = uint64(dev.SmCibCount)
		}
		if cntCount > maxChunkInfoCount {
			return nil, fmt.Errorf("maliciously large or zero cnt_count for sd %d: %d (max allowed: %d)", sd, cntCount, maxChunkInfoCount)
		}

		logger.Debugf("%s: sd %d, sm_offset %x, cnt_count %d", p.dev, sd, smOffset, cntCount)

		if smOffset == 0 || cntCount == 0 {
			continue
		}

		for cnt := uint64(0); cnt < cntCount; cnt++ {
			addrOff := smOffset + int64(8*cnt)
			if addrOff+8 > int64(len(spacemanBuf)) {
				return nil, fmt.Errorf("chunk address entry %d out of range of spaceman buffer", cnt)
			}
			addrData := binary.LittleEndian.Uint64(spacemanBuf[addrOff : addrOff+8])

			if addrData == 0 || int64(addrData) > math.MaxInt64/blockSize {
				return nil, fmt.Errorf("malicious physical address for chunk info block: %d", addrData)
			}

			if err := p.readAt(int64(addrData)*blockSize, cibBuf); err != nil {
				return nil, fmt.Errorf("pread error reading chunk info block: %w", err)
			}

			var cibHdr ChunkInfoBlockHeader
			if err := decode(cibBuf[:cibHeaderSize], &cibHdr); err != nil {
				return nil, fmt.Errorf("failed to decode chunk_info_block header: %w", err)
			}

			if cibHdr.CibChunkInfoCount == 0 || cibHdr.CibChunkInfoCount > maxChunkPerCIB {
				return nil, fmt.Errorf("maliciously large or zero cib_chunk_info_count: %d (max allowed: %d)", cibHdr.CibChunkInfoCount, maxChunkPerCIB)
			}

			if cibHdr.CibO.Type == objTypeSpacemanCIB {
				for chunk := uint32(0); chunk < cibHdr.CibChunkInfoCount; chunk++ {
					off := cibHeaderSize + int(chunk)*ciSize
					if off+ciSize > int(blockSize) {
						return nil, fmt.Errorf("overflow/OOB access for chunk %d in chunk info block", chunk)
					}
					var ci ChunkInfo
					if err := decode(cibBuf[off:off+ciSize], &ci); err != nil {
						return nil, fmt.Errorf("failed to decode chunk_info: %w", err)
					}

					logger.Debugf("%s: xid=%x offset=%x bitTot=%x bitAvl=%x block=%x",
						p.dev, ci.CiXid, ci.CiAddr, ci.CiBlockCount, ci.CiFreeCount, ci.CiBitmapAddr)

					if ci.CiBitmapAddr == 0 && uint64(ci.CiFreeCount) == blocksPerChunk {
						// entire chunk free
						if ci.CiBlockCount == 0 || uint64(ci.CiBlockCount) > totalBlocks {
							return nil, fmt.Errorf("maliciously large or zero ci_block_count: %d", ci.CiBlockCount)
						}
						for block := uint64(0); block < uint64(ci.CiBlockCount); block++ {
							if ci.CiAddr > math.MaxUint64-block {
								return nil, fmt.Errorf("overflow in block+ci_addr calculation")
							}
							fsb.Clear(block + ci.CiAddr)
						}
						continue
					}

					if ci.CiBitmapAddr != 0 && ci.CiBitmapAddr > math.MaxInt64/blockSize {
						return nil, fmt.Errorf("malicious physical address for bitmap entry: %d", ci.CiBitmapAddr)
					}
					if ci.CiBlockCount == 0 || uint64(ci.CiBlockCount) > totalBlocks {
						return nil, fmt.Errorf("maliciously large or zero ci_block_count (bitmap loop): %d", ci.CiBlockCount)
					}

					if err := p.readAt(ci.CiBitmapAddr*blockSize, bitmapEntryBuf); err != nil {
						return nil, fmt.Errorf("pread error reading bitmap entry: %w", err)
					}

					for block := uint64(0); block < uint64(ci.CiBlockCount); block++ {
						if block/8 >= uint64(blockSize) {
							return nil, fmt.Errorf("block index %d out of bounds for bitmap_entry_buf (size %d)", block, blockSize)
						}
						if ci.CiAddr > math.MaxUint64-block {
							return nil, fmt.Errorf("overflow in block+ci_addr calculation")
						}
						byteIdx := block / 8
						bitOff := block % 8
						if bitmapEntryBuf[byteIdx]&(1<<bitOff) == 0 {
							fsb.Clear(block + ci.CiAddr)
						}
					}
				}
			} else {
				// Non-CIB object type: fall back to marking the whole
				// nominal chunk range free, mirroring the C code's
				// "else" branch.
				if sm.SmBlocksPerChunk == 0 || uint64(sm.SmBlocksPerChunk) > maxTotalBlocks {
					return nil, fmt.Errorf("maliciously large or zero sm_blocks_per_chunk: %d", sm.SmBlocksPerChunk)
				}
				if sm.SmChunksPerCib == 0 || sm.SmChunksPerCib > maxChunkPerCIB {
					return nil, fmt.Errorf("maliciously large or zero sm_chunks_per_cib: %d", sm.SmChunksPerCib)
				}

				bpc := uint64(sm.SmBlocksPerChunk)
				cpc := uint64(sm.SmChunksPerCib)

				for chunk := uint64(0); chunk < bpc; chunk++ {
					for block := uint64(0); block < cpc; block++ {
						if bpc > math.MaxUint64/cpc || bpc*cpc > math.MaxUint64/cnt || (cnt != 0 && block > math.MaxUint64-(bpc*cpc*cnt)) {
							return nil, fmt.Errorf("overflow in bitmap_block calculation")
						}
						bitmapBlock := block + bpc*cpc*cnt
						if bitmapBlock > totalBlocks {
							return nil, fmt.Errorf("bitmap_block %d exceeds totalblock %d", bitmapBlock, totalBlocks)
						}
						fsb.Clear(bitmapBlock)
					}
				}
			}
		}
	}

	return fsb, nil
}

// Dump reads the APFS superblock and spaceman structures from the device
// and reconstructs a used/free block bitmap.
func (p *BitmapParser) Dump() (bmap *bitmap.FsBitmap, err error) {
	defer func() {
		if p.fr != nil {
			_ = p.fr.Close()
		}
	}()

	nxsb, err := p.readSuperblock()
	if err != nil {
		return nil, err
	}

	spacemanBuf, err := p.getSpacemanBuf(nxsb)
	if err != nil {
		return nil, err
	}

	fsb, err := p.buildBitmap(nxsb, spacemanBuf)
	if err != nil {
		return nil, err
	}

	logger.Debugf("%s: dumped bitmap: %d bits, block size %d", p.dev, fsb.Bits, fsb.BlockSize)

	return fsb, nil
}
