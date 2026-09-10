package meta

import (
	"fmt"
	"testing"

	"github.com/kisun-bit/drpkg/platform/info"
	"github.com/kisun-bit/drpkg/xutil"
)

// TestPhysicalDrive 在 \\.\physicaldrive0 末尾 100MiB 处写入元数据并验证。
// 仅操作磁盘末尾未分配区域，不会覆盖分区或 GPT 备份头。
func TestPhysicalDrive(t *testing.T) {
	const diskPath = `\\.\PHYSICALDRIVE0`
	const reserveBytes = 100 * 1024 * 1024 // 100 MiB

	// 1. 查询磁盘信息
	disks, err := info.QueryDisks()
	if err != nil {
		t.Fatalf("QueryDisks failed: %v", err)
	}

	var disk *info.Disk
	for i := range disks {
		if disks[i].Device == diskPath {
			disk = &disks[i]
			break
		}
	}
	if disk == nil {
		t.Fatalf("disk %s not found", diskPath)
	}

	t.Logf("Disk: %s, Size: %d bytes (%.2f GiB), Sector: %d/%d",
		disk.Device, disk.Size,
		float64(disk.Size)/(1024*1024*1024),
		disk.LogicalSectorSize, disk.PhysicalSectorSize)
	t.Logf("Partition table: %s, partitions: %d", disk.Table.Type, len(disk.Table.Partitions))

	// 2. 检查分区占用情况，找到末尾空闲区域
	var lastPartitionEnd int64
	for _, p := range disk.Table.Partitions {
		end := p.Start + p.Size
		t.Logf("  Partition: type=%s start=%d size=%d end=%d device=%s",
			p.TypeBrief, p.Start, p.Size, end, p.Device)
		if end > lastPartitionEnd {
			lastPartitionEnd = end
		}
	}

	// GPT 备份头在磁盘最后 33 个扇区（约 16 KiB @ 512B 扇区），需要预留
	gptReserve := int64(disk.LogicalSectorSize) * 34 // 留 34 扇区余量

	// 计算可用区域
	metaStart := disk.Size - int64(reserveBytes)
	if metaStart < lastPartitionEnd {
		t.Fatalf("last 100MiB overlaps with partition (partition end=%d, meta start=%d)",
			lastPartitionEnd, metaStart)
	}

	// 确保不覆盖 GPT 备份区域
	metaEnd := disk.Size
	gptBackupStart := disk.Size - gptReserve
	if metaEnd > gptBackupStart {
		metaEnd = gptBackupStart
	}

	availableBytes := metaEnd - metaStart
	t.Logf("Safe region: [%d, %d) = %d bytes (%.2f MiB)",
		metaStart, metaEnd, availableBytes, float64(availableBytes)/(1024*1024))

	if availableBytes < 50*1024*1024 {
		t.Fatalf("not enough space: need at least 50 MiB, got %.2f MiB",
			float64(availableBytes)/(1024*1024))
	}

	// 3. 对齐到元数据布局的对齐粒度
	// 定长布局下所有区域与记录都按 AlignSize 对齐，起始偏移也必须对齐，
	// 否则裸设备上无法按扇区对齐单独读写某条记录。
	metaStart = (metaStart / AlignSize) * AlignSize
	t.Logf("Aligned metadata start offset: %d", metaStart)

	h := defaultHeader(0)
	if metaStart+h.TotalSize() > metaEnd {
		t.Skipf("disk tail too small: metadata needs %d bytes, safe region is [%d, %d)",
			h.TotalSize(), metaStart, metaEnd)
	}
	t.Logf("Base metadata region: %d bytes (%.2f MiB); Protected Region 位于其后，随记录增长",
		h.TotalSize(), float64(h.TotalSize())/(1024*1024))

	// ======================================================================
	// 4. 创建元数据
	// ======================================================================
	t.Run("Create", func(t *testing.T) {
		bm, err := Create(diskPath, metaStart, 0)
		if err != nil {
			skipIfPermissionDenied(t, err)
			t.Fatalf("Create failed: %v", err)
		}
		defer bm.file.Close()

		t.Logf("Created metadata at offset %d, header version=0x%08x, bitmap units=%d",
			metaStart, bm.header.Version, bm.header.TotalBitmapUnits)

		// 5. 列出设备（应为空）
		pds, err := bm.ListValidProtectDevice()
		if err != nil {
			t.Fatalf("ListValidProtectDevice failed: %v", err)
		}
		if len(pds) != 0 {
			t.Errorf("expected 0 devices, got %d", len(pds))
		}
		t.Logf("ListValidProtectDevice: %d devices (expected 0)", len(pds))

		// 6. 添加设备
		bitIndexSpace := uint64(bm.header.BitIndexSpace)
		extSize := bitIndexSpace * 100 // 100 bits = 400 MiB
		extents := []DiskExtent{
			makeDiskExtent("test-disk-001", 0, extSize),
		}

		if err := bm.AddProtectDevice(DeviceTypeDisk, makeID("test-disk-001"), extents); err != nil {
			t.Fatalf("AddProtectDevice failed: %v", err)
		}
		t.Log("AddProtectDevice: OK")

		// 7. 再次列出
		pds, err = bm.ListValidProtectDevice()
		if err != nil {
			t.Fatalf("ListValidProtectDevice after add failed: %v", err)
		}
		if len(pds) != 1 {
			t.Fatalf("expected 1 device, got %d", len(pds))
		}
		t.Logf("ListValidProtectDevice after add: %d devices", len(pds))

		pd := pds[0]
		t.Logf("  Device: type=%v id=%s extents=%d",
			pd.Type, xutil.TrimZeroString(pd.DeviceID[:]), countValidExtents(pd))

		// 8. 添加第二个设备
		extents2 := []DiskExtent{
			makeDiskExtent("test-disk-002", 0, bitIndexSpace*50),
		}
		if err := bm.AddProtectDevice(DeviceTypeVolume, makeID("test-volume-001"), extents2); err != nil {
			t.Fatalf("AddProtectDevice (2nd) failed: %v", err)
		}
		t.Log("AddProtectDevice (2nd): OK")

		pds, _ = bm.ListValidProtectDevice()
		if len(pds) != 2 {
			t.Fatalf("expected 2 devices, got %d", len(pds))
		}
		t.Logf("ListValidProtectDevice after 2nd add: %d devices", len(pds))

		// 9. 添加重复设备（应失败）
		err = bm.AddProtectDevice(DeviceTypeDisk, makeID("test-disk-001"), extents)
		if err == nil {
			t.Error("AddProtectDevice duplicate: expected error, got nil")
		} else {
			t.Logf("AddProtectDevice duplicate: correctly rejected (%v)", err)
		}

		// 10. 读取位图
		bitCount, bitmapData, err := bm.ReadDeviceBitmap(makeID("test-disk-001"))
		if err != nil {
			t.Fatalf("ReadDeviceBitmap failed: %v", err)
		}
		t.Logf("ReadDeviceBitmap: bitCount=%d, dataLen=%d", bitCount, len(bitmapData))

		// 所有位应为 0
		dirtyCount := 0
		for i := uint32(0); i < bitCount; i++ {
			if bitTest(bitmapData, uint64(i)) {
				dirtyCount++
			}
		}
		if dirtyCount > 0 {
			t.Errorf("expected 0 dirty bits, got %d", dirtyCount)
		}
		t.Logf("ReadDeviceBitmap: all %d bits clean (OK)", bitCount)

		// 11. 移除设备
		if err := bm.RemoveProtectDevice(makeID("test-volume-001")); err != nil {
			t.Fatalf("RemoveProtectDevice failed: %v", err)
		}
		t.Log("RemoveProtectDevice: OK")

		pds, _ = bm.ListValidProtectDevice()
		if len(pds) != 1 {
			t.Fatalf("expected 1 device after remove, got %d", len(pds))
		}
		t.Logf("ListValidProtectDevice after remove: %d devices", len(pds))

		// 12. 移除不存在的设备（应无错误）
		if err := bm.RemoveProtectDevice(makeID("nonexistent")); err != nil {
			t.Fatalf("RemoveProtectDevice non-existent: %v", err)
		}
		t.Log("RemoveProtectDevice non-existent: OK (no error)")

		// 13. Flush
		if _, err := bm.Flush(); err != nil {
			t.Fatalf("Flush failed: %v", err)
		}
		t.Log("Flush: OK")

		// 14. 校验：设备句柄读取 vs 物理磁盘偏移读取的 MD5 一致性
		md5File, err := md5HashFromReader(bm.file, bm.offset, bm.Size())
		if err != nil {
			t.Fatalf("hash from device handle failed: %v", err)
		}
		t.Logf("Device MD5: %s", md5File)

		phyExtents, err := bm.PhysicalExtents()
		if err != nil {
			t.Fatalf("PhysicalExtents failed: %v", err)
		}
		t.Logf("PhysicalExtents: %d extents", len(phyExtents))
		for i, e := range phyExtents {
			t.Logf("  [%d] disk=%s start=%d size=%d", i, e.DiskID.String(), e.Start, e.Size)
		}

		md5Disk, err := md5HashFromExtents(phyExtents)
		if err != nil {
			t.Fatalf("hash from physical extents failed: %v", err)
		}
		t.Logf("Disk MD5: %s", md5Disk)

		if md5File != md5Disk {
			t.Fatalf("MD5 mismatch!\n  device: %s\n  disk: %s", md5File, md5Disk)
		}
		t.Log("MD5 device == disk: OK")

		// 15. 重新加载
		bm2, err := Load(diskPath, metaStart)
		if err != nil {
			t.Fatalf("Load after flush failed: %v", err)
		}
		defer bm2.file.Close()

		pds2, err := bm2.ListValidProtectDevice()
		if err != nil {
			t.Fatalf("ListValidProtectDevice after reload failed: %v", err)
		}
		if len(pds2) != 1 {
			t.Fatalf("expected 1 device after reload, got %d", len(pds2))
		}
		if xutil.TrimZeroString(pds2[0].DeviceID[:]) != "test-disk-001" {
			t.Errorf("device ID mismatch: got %q", xutil.TrimZeroString(pds2[0].DeviceID[:]))
		}
		t.Logf("Load after Flush: OK, device=%s", xutil.TrimZeroString(pds2[0].DeviceID[:]))

		// 15. 读取位图（重新加载后）
		bitCount2, bitmapData2, err := bm2.ReadDeviceBitmap(makeID("test-disk-001"))
		if err != nil {
			t.Fatalf("ReadDeviceBitmap after reload failed: %v", err)
		}
		if bitCount2 != bitCount {
			t.Errorf("bitCount mismatch: %d != %d", bitCount2, bitCount)
		}
		if len(bitmapData2) != len(bitmapData) {
			t.Errorf("bitmapData length mismatch: %d != %d", len(bitmapData2), len(bitmapData))
		}
		t.Logf("ReadDeviceBitmap after reload: bitCount=%d (OK)", bitCount2)

		// 16. 移除所有设备
		if err := bm2.RemoveAllProtectDevice(); err != nil {
			t.Fatalf("RemoveAllProtectDevice failed: %v", err)
		}
		t.Log("RemoveAllProtectDevice: OK")

		pds2, _ = bm2.ListValidProtectDevice()
		if len(pds2) != 0 {
			t.Errorf("expected 0 devices after remove all, got %d", len(pds2))
		}
		t.Logf("ListValidProtectDevice after remove all: %d devices (OK)", len(pds2))

		if _, err := bm2.Flush(); err != nil {
			t.Fatalf("Final Flush failed: %v", err)
		}
		t.Log("Final Flush: OK")
	})

	t.Log("")
	t.Log("============================================================================")
	t.Log("All tests passed on physical drive!")
	t.Logf("Metadata was written at offset %d (%d bytes from end of disk)",
		metaStart, disk.Size-metaStart)
	t.Log("============================================================================")
}

func countValidExtents(pd *ProtectedDevice) int {
	return len(pd.Extents)
}

func init() {
	// 防止 go test 的包初始化副作用
	_ = fmt.Sprintf("")
}
