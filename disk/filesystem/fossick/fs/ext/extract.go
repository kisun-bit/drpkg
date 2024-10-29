package ext

import (
	"encoding/binary"
	"fmt"
	"github.com/kisun-bit/drpkg/util"
	"github.com/kisun-bit/drpkg/util/logger"
	"github.com/pkg/errors"
	"math"
	"os"
)

// Extract 导出含有EXT2/3/4文件系统的设备的位图.
//
// EXT4的提取有效数据的逻辑参考于:
// 1. https://www.kernel.org/doc/html/latest/filesystems/ext4/globals.html.
// 2. https://opensource.com/article/17/5/introduction-ext4-filesystem.
// 3. https://ext4.wiki.kernel.org/index.php/Ext4_Disk_Layout.
// 4. https://blogs.oracle.com/linux/post/understanding-ext4-disk-layout-part-1.
func Extract(device string) (clusterSize int, bitmapBinary []byte, err error) {
	logger.Debugf("EXT2/3/4 Extract(%s). Enter", device)

	deviceHandle, err := os.Open(device)
	if err != nil {
		return 0, nil, err
	}
	defer func() {
		_ = deviceHandle.Close()
	}()

	// 获取SuperBlock.
	sb := make([]byte, EXT234SuperBlockSize)
	n, err := deviceHandle.ReadAt(sb, EXT234SuperBlockStartOff)
	if err != nil {
		return 0, nil, errors.Errorf("failed to read super block of ext2/3/4, %v", err)
	}
	if n != EXT234SuperBlockSize {
		return 0, nil, errors.Errorf("invalid size(%v) of super block", n)
	}

	// 获取块大小.
	sLogBlockSize := binary.LittleEndian.Uint32(sb[0x18:0x1C])
	blockSize := int64(math.Pow(2, float64(10+sLogBlockSize)))
	logger.Debugf("EXT2/3/4 Extract(%s). Block size is %v", device, blockSize)

	// 获取每一个块组的块个数.
	numbBlocksPerGroup := int64(binary.LittleEndian.Uint32(sb[0x20:0x24]))
	logger.Debugf("EXT2/3/4 Extract(%s). Blocks per group is %v", device, numbBlocksPerGroup)

	// 获取总块数.
	numBlocks := int64(binary.LittleEndian.Uint32(sb[0x4:0x8]))
	logger.Debugf("EXT2/3/4 Extract(%s). Total blocks is %v", device, numBlocks)

	// 计算块组数.
	// 说明：这里为何向上取整呢？
	// 因为笔者在对ext4文件系统进行resize2f2之后，发现numbBlocksPerGroup大于numBlocks的现象.
	numBlockGroups := int64(math.Ceil(float64(numBlocks) / float64(numbBlocksPerGroup)))
	if numbBlocksPerGroup > numBlocks {
		logger.Warnf("EXT2/3/4 Extract(%s). Total-groups less than Blocks-per-group", device)
	}
	logger.Debugf("EXT2/3/4 Extract(%s). Total groups is %v", device, numBlockGroups)

	// 获取块组描述器大小.
	// 在 ext2、ext3 和 ext4 中（未启用 64 位功能时），块组描述符只有 32 字节长.
	// 在启用了 64 位功能的 ext4 文件系统上，块组描述符至少扩展到如下所述的 64 字节.
	groupDescriptorSize := int64(binary.LittleEndian.Uint16(sb[0xFE:0x100]))
	if groupDescriptorSize == 0 {
		groupDescriptorSize = int64(32)
	}
	logger.Debugf("EXT2/3/4 Extract(%s). Group Descripter size is %v", device, groupDescriptorSize)

	for blockGroupIndex := int64(0); blockGroupIndex < numBlockGroups; blockGroupIndex++ {
		// 额外说明：如果设置了sparse_super特征标志，则仅在组号为0或3、5或7的幂的组中保留超级块和组描述符的冗余副本。
		// 如果未设置该标志，则保留冗余副本在所有块组中.

		curGroupDesc := fmt.Sprintf("Group%v(%v-%v)",
			blockGroupIndex, blockGroupIndex*numbBlocksPerGroup, (blockGroupIndex+1)*numbBlocksPerGroup-1)

		// 获取当前块组的块位图所处的块组编号（注意当前块组的块位图不一定位于当前块组）.
		superBlockSize := (1024/blockSize + 1) * blockSize
		descriptorLocation := superBlockSize + blockGroupIndex*groupDescriptorSize // GDT在SuperBlock之后.
		descriptor := make([]byte, groupDescriptorSize)
		_, err = deviceHandle.ReadAt(descriptor, descriptorLocation)
		if err != nil {
			return 0, nil, errors.Errorf(
				"failed to read group descriptor from %s", curGroupDesc)
		}

		// 获取位图位置.
		dataBitmapLowLocation := int64(binary.LittleEndian.Uint32(descriptor[0x0:0x4]))
		dataBitmapLocation := dataBitmapLowLocation * blockSize
		if groupDescriptorSize >= 64 {
			dataBitmapHighLocation := int64(binary.LittleEndian.Uint32(descriptor[0x20:0x24]))
			dataBitmapLocation = (dataBitmapHighLocation<<32 | dataBitmapLowLocation) * blockSize
		}

		// 获取位图.
		bitmap := make([]byte, blockSize)
		_, err = deviceHandle.ReadAt(bitmap, dataBitmapLocation)
		if err != nil {
			return 0, nil, errors.Errorf(
				"failed to read bitmap from %s", curGroupDesc)
		}

		// 获取第一个可用块数的Block位置.
		unusedBlockLowLocation := int64(binary.LittleEndian.Uint16(descriptor[0xC:0xE]))
		unusedBlockLocation := unusedBlockLowLocation
		if groupDescriptorSize >= 64 {
			unusedBlockHighLocation := int64(binary.LittleEndian.Uint16(descriptor[0x2C:0x2E]))
			unusedBlockLocation = unusedBlockHighLocation<<16 | unusedBlockLowLocation
		}
		firstUnusedBlockLocation := (blockGroupIndex+1)*numbBlocksPerGroup - unusedBlockLocation

		// logger.Debugf("EXT2/3/4 Extract(%s) %s, GDT#%v, BlockBitmap#%v, FirstUnusedBlockIndex#%v",
		//	 device, curGroupDesc, descriptorLocation, dataBitmapLocation, firstUnusedBlockLocation)
		// logger.Debugf("EXT2/3/4 Extract(%s) %s. Data-bitmap(Location at %v) is\n%s",
		//	 device, curGroupDesc, bitmapBlockLowLoc, hex.Dump(bitmap))

		// 块组的数据位图为空时, 强制修正块组位图，从而保证EXT2/3/4的基本结构.
		// 保证SuperBlock至第一个可用块之间的位图点全部为1. 除非, 第一个可用块的块号恰好是此块组的起始索引, 便不做修正.
		if bitmap[0]&0b11000000 != 0b11000000 {
			needFixBlocks := firstUnusedBlockLocation - blockGroupIndex*numbBlocksPerGroup
			if needFixBlocks == 0 {
				// 说明此块组仅有blocks，无SuperBlock、GDT等等等.
				// logger.Debugf("EXT2/3/4 Extract(%s) %s. Ignore to fix data bitmap", device, curGroupDesc)
			} else if needFixBlocks > 0 {
				util.SetBits(bitmap, int(needFixBlocks))
				logger.Warnf("EXT2/3/4 Extract(%s) %s. fix %v bits",
					device, curGroupDesc, needFixBlocks)
			} else {
				return 0, nil, errors.Errorf(
					"failed to fix bitmap from %s, invalid first unused block index %v",
					curGroupDesc, firstUnusedBlockLocation)
			}
		}

		// 位图信息整合.
		bitmapBinary = append(bitmapBinary, bitmap...)
	}

	return int(blockSize), bitmapBinary, nil
}
