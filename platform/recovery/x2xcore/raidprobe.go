package x2xcore

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
)

const (
	sectorSize = 512

	// Linux MD RAID magic.
	mdMagic uint32 = 0xa92b4efc

	// Intel IMSM metadata signature.
	imsmSignature = "Intel Raid ISM Cfg Sig."

	// Linux MD metadata 0.90.
	md90MagicOffset = 0

	// MD 1.x superblock magic.
	md1MagicOffset = 0

	// MD 1.x version is stored at offset 4.
	md1MajorOffset = 4
	md1MinorOffset = 8

	// MD 1.x RAID level offset.
	//
	// mdp_super1:
	//   magic       0
	//   major       4
	//   feature_map 8
	//   pad0        12
	//   set_uuid0   16
	//   set_uuid1   20
	//   set_uuid2   24
	//   set_uuid3   28
	//   set_name    32
	//   ctime       64
	//   dev_number  72
	//   cnt_corrected 76
	//   device_uuid 80
	//   devflags    96
	//   utime       104
	//   events      112
	//   resync_offset 120
	//   sb_csum     132
	//   dev_size    136
	//   super_offset 144
	//   recovery_offset 152
	//   data_offset 160
	//   data_size   168
	//   super1 layout ...
	//
	// The array RAID level is stored in the personality field
	// near the end of the fixed superblock.
)

// RAIDInfo describes detected RAID metadata.
type RAIDInfo struct {
	Device string

	Detected bool

	// "md" or "imsm"
	Type string

	// "0.90", "1.0", "1.1", "1.2", "imsm"
	MetadataVersion string

	// RAID level if it can be determined.
	// Examples: 0, 1, 5, 6, 10.
	RAIDLevel int

	// UUID as hexadecimal string.
	UUID string

	// Member/device number inside the array.
	DeviceNumber uint32

	// Metadata byte offset.
	MetadataOffset int64

	// Raw signature for debugging.
	Signature string
}

// RAIDProbe probes one block device or disk image.
func RAIDProbe(path string) (*RAIDInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open device %q: %w", path, err)
	}
	defer f.Close()

	size, err := f.Seek(0, os.SEEK_END)
	if err != nil {
		return nil, fmt.Errorf("get device size: %w", err)
	}

	info := &RAIDInfo{
		Device: path,
	}

	// ------------------------------------------------------------
	// 1. Probe Linux MD RAID 1.1.
	//
	// Metadata is located at the beginning of the device.
	// ------------------------------------------------------------
	if r, err := probeMD1(f, 0, "1.1"); err != nil {
		return nil, err
	} else if r != nil {
		r.Device = path
		r.MetadataVersion = "1.1"
		return r, nil
	}

	// ------------------------------------------------------------
	// 2. Probe Linux MD RAID 1.2.
	//
	// Metadata is located 4 KiB from the beginning.
	// ------------------------------------------------------------
	if r, err := probeMD1(f, 4096, "1.2"); err != nil {
		return nil, err
	} else if r != nil {
		r.Device = path
		r.MetadataVersion = "1.2"
		return r, nil
	}

	// ------------------------------------------------------------
	// 3. Probe Linux MD RAID 1.0.
	//
	// Superblock is located at the end of the device.
	// It is aligned according to MD's 64 KiB rule.
	// ------------------------------------------------------------
	if size >= 128*1024 {
		offset := md10Offset(size)

		if r, err := probeMD1(f, offset, "1.0"); err != nil {
			return nil, err
		} else if r != nil {
			r.Device = path
			r.MetadataVersion = "1.0"
			return r, nil
		}
	}

	// ------------------------------------------------------------
	// 4. Probe Linux MD RAID 0.90.
	//
	// Metadata is a 4 KiB block located 64 KiB-aligned near
	// the end of the device.
	// ------------------------------------------------------------
	if size >= 128*1024 {
		offset := md90Offset(size)

		if r, err := probeMD90(f, offset); err != nil {
			return nil, err
		} else if r != nil {
			r.Device = path
			r.MetadataVersion = "0.90"
			return r, nil
		}
	}

	// ------------------------------------------------------------
	// 5. Probe Intel IMSM / Intel RST.
	// ------------------------------------------------------------
	if r, err := probeIMSM(f, size); err != nil {
		return nil, err
	} else if r != nil {
		r.Device = path
		return r, nil
	}

	info.Detected = false
	return info, nil
}

// md10Offset returns the offset of MD metadata 1.0.
//
// mdadm uses:
//
//	(size & ~(64K - 1)) - 64K
func md10Offset(size int64) int64 {
	const align = int64(64 * 1024)

	return (size & ^(align - 1)) - align
}

// md90Offset is effectively the same end-of-disk alignment used
// by the legacy 0.90 format.
func md90Offset(size int64) int64 {
	const align = int64(64 * 1024)

	return (size & ^(align - 1)) - align
}

func probeMD1(
	f *os.File,
	offset int64,
	version string,
) (*RAIDInfo, error) {

	buf := make([]byte, 4096)

	if _, err := f.ReadAt(buf, offset); err != nil {
		return nil, nil
	}

	if len(buf) < 256 {
		return nil, nil
	}

	magic := binary.LittleEndian.Uint32(
		buf[md1MagicOffset:],
	)

	if magic != mdMagic {
		return nil, nil
	}

	major := binary.LittleEndian.Uint32(
		buf[md1MajorOffset:],
	)

	if major != 1 {
		return nil, nil
	}

	// UUID is 16 bytes starting at offset 16.
	uuid := formatUUID(buf[16:32])

	// dev_number is at offset 72.
	devNumber := binary.LittleEndian.Uint32(
		buf[72:76],
	)

	info := &RAIDInfo{
		Detected:        true,
		Type:            "md",
		MetadataVersion: version,
		UUID:            uuid,
		DeviceNumber:    devNumber,
		MetadataOffset:  offset,
		Signature:       "mdraid",
	}

	// The RAID level is stored in the personality field.
	//
	// For robust detection we scan the fixed superblock area
	// for known little-endian RAID level values.
	info.RAIDLevel = detectMD1RAIDLevel(buf)

	return info, nil
}

// detectMD1RAIDLevel tries to locate the RAID personality.
//
// mdadm's mdp_super1 contains:
//
//	level
//	layout
//	size
//
// in the later part of the fixed superblock.
//
// We intentionally keep this function conservative. If the
// value cannot be confidently recognized, RAIDLevel remains -1.
func detectMD1RAIDLevel(buf []byte) int {
	// Known RAID level values.
	known := map[uint32]int{
		0:  0,
		1:  1,
		4:  4,
		5:  5,
		6:  6,
		10: 10,
	}

	// The mdp_super1 fixed section is 256 bytes.
	//
	// The level field is normally around offset 80/90 depending
	// on structure interpretation. We check 32-bit aligned fields
	// in the fixed area and reject ambiguous cases.
	candidates := make([]int, 0, 2)

	for off := 176; off+4 <= 256; off += 4 {
		v := binary.LittleEndian.Uint32(buf[off : off+4])

		if level, ok := known[v]; ok {
			candidates = append(candidates, level)
		}
	}

	if len(candidates) == 1 {
		return candidates[0]
	}

	return -1
}

// probeMD90 probes legacy MD 0.90 metadata.
func probeMD90(
	f *os.File,
	offset int64,
) (*RAIDInfo, error) {

	buf := make([]byte, 4096)

	if _, err := f.ReadAt(buf, offset); err != nil {
		return nil, nil
	}

	// md 0.90 magic is also 0xa92b4efc.
	magic := binary.LittleEndian.Uint32(buf[0:4])

	if magic != mdMagic {
		return nil, nil
	}

	// UUID in md 0.90 starts at byte 16.
	uuid := formatUUID(buf[16:32])

	// The RAID level is stored in the mdp_super_t structure.
	//
	// The exact legacy layout differs from metadata 1.x, so
	// do not claim a RAID level unless it is recognized.
	level := -1

	// Major/minor version etc. are not interpreted here.
	// Detection of the superblock itself is sufficient.
	return &RAIDInfo{
		Detected:        true,
		Type:            "md",
		MetadataVersion: "0.90",
		RAIDLevel:       level,
		UUID:            uuid,
		MetadataOffset:  offset,
		Signature:       "mdraid",
	}, nil
}

// probeIMSM probes Intel RST / IMSM metadata.
//
// mdadm's Intel implementation defines:
//
//	MPB_SIGNATURE "Intel Raid ISM Cfg Sig. "
//
// The IMSM metadata parameter block is normally located near
// the end of the disk.
func probeIMSM(
	f *os.File,
	size int64,
) (*RAIDInfo, error) {

	if size < 128*1024 {
		return nil, nil
	}

	// IMSM reserves metadata near the end of the disk.
	//
	// mdadm defines:
	//
	//     IMSM_RESERVED_SECTORS 8192
	//
	// and uses a 2210-sector metadata area.
	//
	// Probe the 64 KiB-aligned area near the end first.
	const sector = int64(512)
	const reserved = int64(8192) * sector

	// Common IMSM anchor location:
	// end - 2 * 64K-ish metadata area.
	//
	// To make detection more tolerant across firmware variants,
	// scan the last 128 KiB in 512-byte steps.
	start := size - 128*1024

	buf := make([]byte, 512)

	for offset := start; offset < size; offset += sector {
		if _, err := f.ReadAt(buf, offset); err != nil {
			continue
		}

		sig := string(bytes.TrimRight(buf[:32], "\x00"))

		if !bytes.HasPrefix(
			buf[:32],
			[]byte(imsmSignature),
		) {
			continue
		}

		info := &RAIDInfo{
			Detected:        true,
			Type:            "imsm",
			MetadataVersion: "imsm",
			MetadataOffset:  offset,
			Signature:       sig,
			RAIDLevel:       -1,
		}

		// IMSM superblock:
		//
		// 0x00 signature
		// 0x20 checksum
		// 0x24 mpb_size
		// 0x28 family_num
		// 0x2c generation_num
		// 0x38 num_disks
		// 0x39 num_raid_devs
		//
		// Disk entries begin at 0xD8.
		//
		// RAID volume descriptors follow disk entries.
		//
		// For pure "contains RAID metadata" detection,
		// the signature is already enough.
		_ = reserved

		return info, nil
	}

	return nil, nil
}

func formatUUID(b []byte) string {
	if len(b) < 16 {
		return ""
	}

	// Linux MD UUID is treated as four little-endian uint32
	// values rather than RFC4122 UUID byte ordering.
	return fmt.Sprintf(
		"%08x:%08x:%08x:%08x",
		binary.LittleEndian.Uint32(b[0:4]),
		binary.LittleEndian.Uint32(b[4:8]),
		binary.LittleEndian.Uint32(b[8:12]),
		binary.LittleEndian.Uint32(b[12:16]),
	)
}
