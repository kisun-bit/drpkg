package meta

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/kisun-bit/drpkg/command"
	"github.com/kisun-bit/drpkg/disk/cdp/drvctl/ioctl"
	"github.com/kisun-bit/drpkg/disk/table"
	"github.com/kisun-bit/drpkg/logger"
	"github.com/kisun-bit/drpkg/platform/info"
	"github.com/kisun-bit/drpkg/xutil"
	"github.com/pkg/errors"
)

// AllocateMetadataLinux 在Linux上分配并初始化CDP元数据文件。
//
// 参数：
//   - filePath: 元数据文件的绝对路径，如 /opt/cdp.bmp
//   - drvParams: 驱动启动参数，函数会填充其中的元数据磁盘GUID和位图区域信息
//   - maxProtectedDiskCount: 最大受保护磁盘数量（16/32/64/128）
//   - maxProtectedSizeTBPerDisk: 每磁盘最大受保护大小（TB），如 64/128/512
//   - enablePreStageCopy: 是否启用预处理阶段拷贝模式
func AllocateMetadataLinux(
	filePath string,
	drvParams *ioctl.DRVReqStart,
	maxProtectedDiskCount, maxProtectedSizeTBPerDisk int,
	enablePreStageCopy bool,
) error {
	if filePath == "" {
		return errors.New("metadata file path is empty")
	}
	if drvParams == nil {
		return errors.New("driver parameters is nil")
	}
	logger.Debugf("AllocateMetadataLinux: filePath=%v", filePath)

	psinfo, err := info.QueryPsInfo()
	if err != nil {
		return err
	}

	maxDiskCount := uint32(maxProtectedDiskCount)
	maxDiskSize := uint64(maxProtectedSizeTBPerDisk) * (1 << 40)

	h, err := GenerateHeader(maxDiskCount, maxDiskSize)
	if err != nil {
		return err
	}
	logger.Debugf("AllocateMetadataLinux: header=%v", xutil.Pretty(h))

	metadataSize := int64(h.MaxDiskCount)*int64(h.BitsPerDiskBitmap/8) + DefaultFirstDiskBitmapStart
	logger.Debugf("AllocateMetadataLinux: metadataSize=%v", metadataSize)

	if xutil.IsExisted(filePath) {
		_, _, _ = command.Execute("chattr -i "+filePath, command.WithDebug())
		e := os.RemoveAll(filePath)
		logger.Debugf("AllocateMetadataLinux: Remove %s: %v", filePath, e)
	}

	logger.Debugf("AllocateMetadataLinux: Create filePath=%v", filePath)
	metadataObj, err := Create(filePath, maxDiskCount, maxDiskSize, enablePreStageCopy)
	if err != nil {
		return err
	}

	_, _, _ = command.Execute("chattr +i "+filePath, command.WithDebug())

	es := metadataObj.Extents()
	bitmapBytesPerDisk := metadataObj.Header().BitsPerDiskBitmap / 8

	if len(es) == 0 {
		return errors.Errorf("extents not found in %s", filePath)
	}

	logger.Debugf("AllocateMetadataLinux: Extents: %s", xutil.Pretty(es))

	if err = fillBitmapRegionByFileExtents(uint64(bitmapBytesPerDisk), psinfo, es, drvParams); err != nil {
		return err
	}

	return nil
}

// AllocateMetadataWindows 在Windows上分配并初始化CDP元数据区域。
//
// 参数：
//   - volumeLtr: 卷盘符，如 "C"（不含冒号）
//   - drvParams: 驱动启动参数，函数会填充其中的元数据磁盘GUID、私有段和位图区域
//   - maxProtectedDiskCount: 最大受保护磁盘数量（16/32/64/128）
//   - maxProtectedSizeTBPerDisk: 每磁盘最大受保护大小（TB），如 64/128/512
//   - reversedSizeInMB: 缩容时预留空间大小（MB），防止元数据写不进去
//   - supportedIscsi: 是否支持iSCSI磁盘
func AllocateMetadataWindows(
	volumeLtr string,
	drvParams *ioctl.DRVReqStart,
	maxProtectedDiskCount, maxProtectedSizeTBPerDisk int,
	reversedSizeInMB int,
	supportedIscsi bool,
) error {
	if volumeLtr == "" {
		return errors.New("metadata volume letter is empty")
	}
	if drvParams == nil {
		return errors.New("driver parameters is nil")
	}
	logger.Debugf("AllocateMetadataWindows: volumeLtr=%v", volumeLtr)

	psinfo, err := info.QueryPsInfo()
	if err != nil {
		return err
	}

	maxDiskCount := uint32(maxProtectedDiskCount)
	maxDiskSize := uint64(maxProtectedSizeTBPerDisk) * (1 << 40)

	h, err := GenerateHeader(maxDiskCount, maxDiskSize)
	if err != nil {
		return err
	}
	logger.Debugf("AllocateMetadataWindows: header=%v", xutil.Pretty(h))

	metadataSize := int64(h.MaxDiskCount)*int64(h.BitsPerDiskBitmap/8) + DefaultFirstDiskBitmapStart
	logger.Debugf("AllocateMetadataWindows: metadataSize=%v", metadataSize)

	ok, rInfo, err := getMetadataRegion(psinfo, volumeLtr, metadataSize, reversedSizeInMB, maxProtectedDiskCount, maxProtectedSizeTBPerDisk, supportedIscsi)
	if err != nil {
		return err
	}
	logger.Debugf("AllocateMetadataWindows: ok=%v rInfo=%v", ok, xutil.Pretty(rInfo))

	if !ok {
		logger.Debugf("AllocateMetadataWindows: Start shrink volume %s", volumeLtr)
		if e1 := shrinkVolume(psinfo, volumeLtr, metadataSize, maxProtectedDiskCount, maxProtectedSizeTBPerDisk, reversedSizeInMB); e1 != nil {
			logger.Warnf("AllocateMetadataWindows: [1/2] Shrinking. error: %v", e1)

			//
			// 自测发现：若已缩容的区域（65M）被用户扩容到了原卷，会导致重启主机后，原有任务出现元数据错误的问题，然后进入到任务重试处理
			// 但是在重试过程中，以（65M）继续缩容，会导致缩容失败的问题（报错：非法参数），但是当我以（66M）去缩容时，又是可以的。
			// 因此，缩容我们进行两次尝试，第二次尝试时，将缩容空间大小增大1M。
			//

			metadataSizeV2 := metadataSize + (1 << 20)
			logger.Warnf("AllocateMetadataWindows: Added metadataSize: %v", metadataSizeV2)
			if e2 := shrinkVolume(psinfo, volumeLtr, metadataSizeV2, maxProtectedDiskCount, maxProtectedSizeTBPerDisk, reversedSizeInMB); e2 != nil {
				logger.Warnf("AllocateMetadataWindows: [2/2] Shrinking. error: %v", e2)
				return e2
			}
		}
		psinfo, err = info.QueryPsInfo()
		if err != nil {
			return err
		}
		ok, rInfo, err = getMetadataRegion(psinfo, volumeLtr, metadataSize, reversedSizeInMB, maxProtectedDiskCount, maxProtectedSizeTBPerDisk, supportedIscsi)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("metadata zone allocation failed")
		}
	}
	logger.Debugf("AllocateMetadataWindows: region=\n%s", xutil.Pretty(rInfo))

	_, err = CreateLinear(msDiskPath(rInfo.diskId), uint64(rInfo.start), maxDiskCount, maxDiskSize)
	if err != nil {
		return errors.Wrapf(err, "Create linear metadata")
	}

	drvParams.MetadataDiskGuid.DiskId = uint32(rInfo.diskId)
	drvParams.PrivateSegments.Start = uint64(rInfo.start)

	// Windows 平台会对 PrivateSegments 中的区域进行保护，所以必须传递整个元数据区域的大小
	drvParams.PrivateSegments.Size = uint64(metadataSize)

	for i := uint32(0); i < drvParams.ProtectDisksLen; i++ {
		curDiskId := drvParams.ProtectDisks[i].Guid.DiskId
		drvParams.ProtectDisks[i].DiskBitmapLen = 1
		curSize := int64(h.BitsPerDiskBitmap / 8)
		curStart := DefaultFirstDiskBitmapStart + int64(curDiskId)*curSize
		drvParams.ProtectDisks[i].DiskBitmap = append(
			drvParams.ProtectDisks[i].DiskBitmap,
			ioctl.Segment{
				Start: drvParams.PrivateSegments.Start + uint64(curStart),
				Size:  uint64(curSize),
			})
	}
	logger.Debugf("AllocateMetadataWindows: drvParams=\n%s", xutil.Pretty(drvParams))

	return nil
}

// ========== private types ==========

type volumeInfo struct {
	Name        string       `json:"name"`
	VolumeID    string       `json:"volume_id"`
	DiskRegions []diskRegion `json:"disk_regions"`
}

type region struct {
	Start  uint64 `json:"start"`
	Length uint64 `json:"length"`
}

type diskRegion struct {
	Disk   info.Disk `json:"disk"`
	Region region    `json:"region"`
}

// metadataRegions 元数据存储区域信息
// 此区域用于存储CDP运行过程中的本地缓存信息
type metadataRegions struct {
	RawDisk         []diskRegion `json:"raw_disk"`
	UnusedDiskSpace []diskRegion `json:"unused_disk_space"`
	EmptyPart       []diskRegion `json:"empty_part"`
}

type metadataRegionInfo struct {
	diskId     int
	start      int64
	sectorSize int
}

// ========== private functions ==========

func msDiskPath(diskId int) string {
	return fmt.Sprintf(`\\.\PHYSICALDRIVE%d`, diskId)
}

func fillBitmapRegionByFileExtents(
	bitmapBytesPerDisk uint64,
	psinfo *info.PsInfo,
	fileEs []FileDiskExtentSegment,
	drvParam *ioctl.DRVReqStart,
) error {
	if len(fileEs) == 0 {
		return errors.Errorf("empty extents")
	}
	if drvParam == nil {
		return errors.Errorf("driver parameter is nil")
	}

	if psinfo != nil {
		diskPath := fileEs[0].Disk

		setMetadata := func(name string) error {
			if len(name) > ioctl.MaxnameLen {
				return errors.Errorf("name of %s is too long", diskPath)
			}
			copy(drvParam.MetadataDiskGuid.Name[:], name)

			mj, mi, err := getMajorOfDisk(diskPath)
			if err != nil {
				return errors.Wrap(err, "getMajorOfDisk")
			}
			drvParam.MetadataDiskGuid.Major = mj
			drvParam.MetadataDiskGuid.Minor = mi
			return nil
		}

		found := false
		for _, disk := range psinfo.Public.Disks {
			if disk.Device != diskPath {
				continue
			}
			if err := setMetadata(disk.PathId); err != nil {
				return err
			}
			found = true
			break
		}

		if !found {
			for _, disk := range psinfo.Private.Linux.Raid {
				if disk.Device != diskPath {
					continue
				}
				if err := setMetadata(disk.Name); err != nil {
					return err
				}
				found = true
				break
			}
		}

		if !found {
			for _, disk := range psinfo.Private.Linux.Multipath {
				if disk.Device != diskPath {
					continue
				}
				if err := setMetadata(disk.Name); err != nil {
					return err
				}
				found = true
				break
			}
		}

		if !found {
			return errors.Errorf("metadata of %s not found", diskPath)
		}
	}

	drvParam.PrivateSegments.Start = uint64(fileEs[0].Start)
	drvParam.PrivateSegments.Size = uint64(fileEs[0].Size)
	if drvParam.PrivateSegments.Size > 4096 {
		drvParam.PrivateSegments.Size = 4096
	}
	if drvParam.PrivateSegments.Size == 0 {
		return errors.Errorf("invalid extents[0] for private-segment")
	}

	if err := assertOneDiskForFileExtents(fileEs); err != nil {
		return err
	}

	logger.Debugf("fillBitmapRegionByFileExtents: drvParam: \n%v", xutil.Pretty(drvParam))
	logger.Debugf("fillBitmapRegionByFileExtents: fileEs: \n%v", xutil.Pretty(fileEs))

	allocated := buildInitialAllocated(fileEs, drvParam)
	logger.Debugf("fillBitmapRegionByFileExtents: allocated: \n%s", xutil.Pretty(allocated))

	allocatedCopy := make([]ioctl.Segment, len(allocated))
	copy(allocatedCopy, allocated)

	waitAllocateMap := make(map[int][]ioctl.Segment)
	for pIdx, p := range drvParam.ProtectDisks {
		if p.ProtectRegionsLen == 0 {
			continue
		}
		if p.DiskBitmapLen != 0 {
			continue
		}
		waitAllocateMap[pIdx] = []ioctl.Segment{}
	}

	logger.Debugf("fillBitmapRegionByFileExtents: waitAllocateMap: \n%s", xutil.Pretty(waitAllocateMap))

	for pIdx := range waitAllocateMap {
		logger.Debugf("fillBitmapRegionByFileExtents: Start allocated for ProtectDisks[%v]", pIdx)
		logger.Debugf("fillBitmapRegionByFileExtents: allocated[before]: \n%s", xutil.Pretty(allocated))

		freeExtents := subtractExtents(fileEs, allocated)

		logger.Debugf("fillBitmapRegionByFileExtents: allocated[filter]: \n%s", xutil.Pretty(allocated))
		logger.Debugf("fillBitmapRegionByFileExtents: freeExtents: \n%s", xutil.Pretty(freeExtents))

		segs, err := allocateFromFreeExtents(freeExtents, bitmapBytesPerDisk)
		logger.Debugf("fillBitmapRegionByFileExtents: segs: \n%s", xutil.Pretty(segs))

		if err != nil {
			return errors.Errorf(
				"not enough space for bitmap of %s",
				drvParam.ProtectDisks[pIdx].Guid,
			)
		}

		waitAllocateMap[pIdx] = segs
		for _, s := range segs {
			allocated = append(allocated, s)
		}
	}

	for pIdx := range waitAllocateMap {
		total := uint64(0)
		for _, seg := range waitAllocateMap[pIdx] {
			total += seg.Size
		}
		if total != bitmapBytesPerDisk {
			return errors.Errorf("bitmap size invalid")
		}
	}

	for _, segs := range waitAllocateMap {
		for _, nowAllocatedSeg := range segs {
			aStart := nowAllocatedSeg.Start
			aEnd := aStart + nowAllocatedSeg.Size
			for _, historyAllocatedSeg := range allocatedCopy {
				bStart := historyAllocatedSeg.Start
				bEnd := bStart + historyAllocatedSeg.Size
				if aStart < bEnd && aEnd > bStart {
					return errors.Errorf(
						"Conflict: region [%d, %d) overlaps with historical region [%d, %d)",
						aStart, aEnd, bStart, bEnd,
					)
				}
			}
		}
	}

	for pIdx := range waitAllocateMap {
		drvParam.ProtectDisks[pIdx].DiskBitmap = waitAllocateMap[pIdx]
		drvParam.ProtectDisks[pIdx].DiskBitmapLen = uint32(len(waitAllocateMap[pIdx]))
	}

	logger.Debugf("fillBitmapRegionByFileExtents: drvParam: \n%v", xutil.Pretty(drvParam))
	return nil
}

func buildInitialAllocated(
	fileEs []FileDiskExtentSegment,
	drvParam *ioctl.DRVReqStart,
) []ioctl.Segment {
	allocated := make([]ioctl.Segment, 0)
	headerLeft := int64(HeaderDiskSize)

	for _, fileE := range fileEs {
		if headerLeft <= 0 {
			break
		}
		curSize := fileE.Size
		if curSize > headerLeft {
			curSize = headerLeft
		}
		allocated = append(allocated, ioctl.Segment{
			Start: uint64(fileE.Start),
			Size:  uint64(curSize),
		})
		headerLeft -= curSize
	}

	for _, p := range drvParam.ProtectDisks {
		if p.ProtectRegionsLen != 0 && p.DiskBitmapLen != 0 {
			for _, seg := range p.DiskBitmap {
				allocated = append(allocated, seg)
			}
		}
	}

	return allocated
}

func subtractExtents(
	fileEs []FileDiskExtentSegment,
	allocated []ioctl.Segment,
) []ioctl.Segment {
	sort.Slice(allocated, func(i, j int) bool {
		return allocated[i].Start < allocated[j].Start
	})

	free := make([]ioctl.Segment, 0)

	for _, fe := range fileEs {
		curStart := uint64(fe.Start)
		end := uint64(fe.Start + fe.Size)

		for _, a := range allocated {
			aStart := a.Start
			aEnd := a.Start + a.Size

			if aEnd <= curStart {
				continue
			}
			if aStart >= end {
				break
			}
			if aStart > curStart {
				free = append(free, ioctl.Segment{
					Start: curStart,
					Size:  aStart - curStart,
				})
			}
			if aEnd > curStart {
				curStart = aEnd
			}
			if curStart >= end {
				break
			}
		}

		if curStart < end {
			free = append(free, ioctl.Segment{
				Start: curStart,
				Size:  end - curStart,
			})
		}
	}

	return free
}

func allocateFromFreeExtents(
	free []ioctl.Segment,
	size uint64,
) ([]ioctl.Segment, error) {
	left := size
	result := make([]ioctl.Segment, 0)

	for _, f := range free {
		if left == 0 {
			break
		}
		use := f.Size
		if use > left {
			use = left
		}
		result = append(result, ioctl.Segment{
			Start: f.Start,
			Size:  use,
		})
		left -= use
	}

	if left != 0 {
		return nil, errors.Errorf("not enough free space")
	}

	return result, nil
}

func assertOneDiskForFileExtents(
	fileEs []FileDiskExtentSegment,
) error {
	if len(fileEs) == 0 {
		return nil
	}
	disk := fileEs[0].Disk
	for _, e := range fileEs {
		if e.Disk != disk {
			return errors.Errorf("file extents span multiple disks")
		}
	}
	return nil
}

func getMajorOfDisk(diskPath string) (major, min uint32, err error) {
	devNameTable, err := DeviceMajorTable(diskPath)
	if err != nil {
		return 0, 0, err
	}
	for m, dev := range devNameTable {
		if dev != diskPath {
			continue
		}
		majorInt, _ := strconv.Atoi(m.MajorNum())
		minInt, _ := strconv.Atoi(m.MinNum())
		return uint32(majorInt), uint32(minInt), nil
	}
	return 0, 0, errors.Errorf("major of %s not found", diskPath)
}

func getMetadataRegion(
	psinfo *info.PsInfo,
	volume string,
	size int64,
	reversedSizeInMB int,
	maxProtectedDiskCount, maxProtectedSizeTBPerDisk int,
	supportedIscsi bool,
) (yes bool, info metadataRegionInfo, err error) {
	md, err := listMetadataRegions(psinfo, maxProtectedDiskCount, maxProtectedSizeTBPerDisk, supportedIscsi)
	if err != nil {
		return false, info, err
	}
	logger.Debugf("getMetadataRegion: md=\n%s", xutil.Pretty(md))

	volDiskId, volStart, volEnd, sectorSize, ok := queryDiskIDAndOffsetForVolume(psinfo, volume)
	if !ok {
		return false, info, errors.Errorf("offset information of %v is not found", volume)
	}
	logger.Debugf("getMetadataRegion: volDiskId=%d, volStart=%d, volEnd=%d, sectorSize=%d",
		volDiskId, volStart, volEnd, sectorSize)

	reversedSize := uint64(reversedSizeInMB) * (1024 * 1024)

	for _, r := range md.UnusedDiskSpace {
		if r.Region.Start == uint64(volEnd) && r.Region.Length-reversedSize >= uint64(size) {
			info.diskId = volDiskId
			info.start = int64(r.Region.Start + r.Region.Length - reversedSize - uint64(size))
			info.sectorSize = int(sectorSize)
			return true, info, nil
		}
	}
	return false, info, nil
}

// shrinkVolume 缩容卷
func shrinkVolume(
	psinfo *info.PsInfo,
	vol string,
	size int64,
	maxProtectedDiskCount, maxProtectedSizeTBPerDisk int,
	reversedSizeInMB int,
) error {
	findVol := false
	vs, err := ListVolumesForMetadata(psinfo, maxProtectedDiskCount, maxProtectedSizeTBPerDisk)
	if err != nil {
		return err
	}
	for _, v := range vs.Volumes {
		if strings.ToUpper(v.Name) == strings.ToUpper(vol) {
			findVol = true
			break
		}
	}
	if !findVol {
		return errors.Errorf("volume %s does not meet all the conditions for metadata storage", vol)
	}

	tmpDs, err := os.CreateTemp(xutil.ExecDir(), "shrink")
	if err != nil {
		return err
	}
	tmpDsPath := tmpDs.Name()
	defer func() {
		_ = os.Remove(tmpDsPath)
	}()
	defer tmpDs.Close()

	sizeMB := (size + (1048576 - 1)) / (1 << 20)
	if reversedSizeInMB > 0 {
		logger.Warnf("shrinkVolume() Add %vMB to %vMB", reversedSizeInMB, sizeMB)
		sizeMB += int64(reversedSizeInMB)
	}
	tmpDsContent := fmt.Sprintf("SELECT VOLUME %s\r\nSHRINK DESIRED=%v", vol, sizeMB)
	if _, err = tmpDs.WriteString(tmpDsContent); err != nil {
		return err
	}
	_, o, e := command.Execute("chcp 437 & diskpart /s " + tmpDsPath)
	logger.Debugf("shrinkVolume() cmd\nscript:%s\noutput:%s\nerror:%v", tmpDsContent, o, e)

	if e != nil {
		return errors.Wrapf(e, "output: %v", o)
	}
	return nil
}

// listMetadataRegions 列举适合元数据存储的所有区域信息（普通磁盘的分区间隙）
func listMetadataRegions(
	psinfo *info.PsInfo,
	maxProtectedDiskCount, maxProtectedSizeTBPerDisk int,
	supportedIscsi bool,
) (regions metadataRegions, err error) {
	if psinfo == nil {
		psinfo, err = info.QueryPsInfo()
		if err != nil {
			return regions, err
		}
	}

	maxDiskCount := uint32(maxProtectedDiskCount)
	maxDiskSize := uint64(maxProtectedSizeTBPerDisk) * (1 << 40)

	h, err := GenerateHeader(maxDiskCount, maxDiskSize)
	if err != nil {
		return regions, err
	}

	metadataSize := int64(h.MaxDiskCount)*int64(h.BitsPerDiskBitmap/8) + DefaultFirstDiskBitmapStart
	logger.Debugf("listMetadataRegions: metadataSize=%v", metadataSize)

	for _, disk := range psinfo.Public.Disks {
		if disk.Table.Type == table.TableTypeRaw ||
			disk.IsMsDynamic ||
			disk.IsReadOnly ||
			!disk.IsOnline {
			continue
		}

		if !supportedIscsi {
			if strings.Contains(disk.PathId, "-iscsi-") || strings.EqualFold(disk.Bus, "iscsi") {
				continue
			}
		}

		startSector := int64(1)
		if disk.Table.Type == table.TableTypeGPT {
			startSector = 34
		}

		endSector := disk.Size / int64(disk.LogicalSectorSize)
		if disk.Table.Type == table.TableTypeGPT {
			endSector -= 33
		}

		sort.Slice(disk.Table.Partitions, func(i, j int) bool {
			return disk.Table.Partitions[i].Start < disk.Table.Partitions[j].Start
		})

		for _, part := range disk.Table.Partitions {
			isExtendPart := false
			for _, et := range table.MBRExtendPartTypes {
				if et == part.Type {
					isExtendPart = true
				}
			}

			partStartSector := part.Start / int64(disk.LogicalSectorSize)
			partTotalSector := part.Size / int64(disk.LogicalSectorSize)
			partEndSector := partStartSector + partTotalSector

			if isExtendPart {
				continue
			}

			if partStartSector > startSector {
				appendRegions(&regions.UnusedDiskSpace, disk, startSector, partStartSector-startSector, metadataSize)
			}
			startSector = partEndSector
		}

		if endSector > startSector {
			appendRegions(&regions.UnusedDiskSpace, disk, startSector, endSector-startSector, metadataSize)
		}
	}

	return regions, nil
}

func appendRegions(regions *[]diskRegion, disk info.Disk, startSector, sectors, minMetaSize int64) {
	if startSector < 0 || sectors <= 0 {
		return
	}
	if sectors*int64(disk.LogicalSectorSize) < minMetaSize {
		return
	}
	*regions = append(*regions, diskRegion{
		Disk: disk,
		Region: region{
			Start:  uint64(startSector * int64(disk.LogicalSectorSize)),
			Length: uint64(sectors * int64(disk.LogicalSectorSize)),
		},
	})
}

func queryDiskIDAndOffsetForVolume(psinfo *info.PsInfo, volumeLtr string) (disk int, start, end int64, sectorSize int64, ok bool) {
	logger.Debugf("queryDiskIDAndOffsetForVolume: volume=%s", volumeLtr)

	diskSegment := xutil.Segment{}
	for _, v := range psinfo.Public.Volumes {
		if strings.TrimSuffix(v.MountPoint, ":") == volumeLtr && len(v.Segments) == 1 {
			diskSegment = v.Segments[0]
			break
		}
	}
	if diskSegment.Device == "" {
		logger.Errorf("queryDiskIDAndOffsetForVolume: segment not matched")
		return -1, -1, -1, -1, false
	}

	for _, d := range psinfo.Public.Disks {
		if d.Device != diskSegment.Device {
			continue
		}
		for _, p := range d.Table.Partitions {
			if uint64(p.Start) == diskSegment.Start {
				diskId, e := xutil.WindowsDiskIDFromPath(diskSegment.Device)
				if e != nil {
					logger.Errorf("queryDiskIDAndOffsetForVolume: WindowsDiskIDFromPath for %s: %v", d.Device, e)
					return -1, -1, -1, -1, false
				}
				return int(diskId), int64(diskSegment.Start), int64(diskSegment.Start + diskSegment.Size), int64(d.LogicalSectorSize), true
			}
		}
	}

	logger.Errorf("queryDiskIDAndOffsetForVolume: Partition not matched")
	return -1, -1, -1, -1, false
}
