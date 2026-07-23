package main

import (
	"os"

	"github.com/kisun-bit/drpkg/disk/filesystem/btrfs"
	"github.com/kisun-bit/drpkg/extend"
	"github.com/kisun-bit/drpkg/logger"
)

func testMirrorFs() {
	if len(os.Args) < 3 {
		logger.Fatalf("usage: %s <origin> <target>", os.Args[0])
	}

	origin := os.Args[1]
	target := os.Args[2]

	//
	// 1. 解析源 XFS 文件系统，生成"已使用块"位图
	//

	originSize, e := extend.FileSize(origin)
	if e != nil {
		panic(e)
	}

	p, e := btrfs.NewBitmapParser(origin, 0, int64(originSize))
	if e != nil {
		panic(e)
	}

	bmp, e := p.Dump()
	if e != nil {
		panic(e)
	}

	logger.Debugf("testMirrorFs: bits=%d blocksize=%d used_size=%d",
		bmp.Bits, bmp.BlockSize, bmp.Size())

	//
	// 2. 打开源设备（只读）和目标设备（读写）
	//

	originFile, e := os.OpenFile(origin, os.O_RDONLY, 0)
	if e != nil {
		panic(e)
	}
	defer originFile.Close()

	// 目标文件如果不存在需要创建；如果目标是块设备（如 /dev/sdX），O_CREATE 不生效，不影响正常使用
	targetFile, e := os.OpenFile(target, os.O_RDWR|os.O_CREATE, 0644)
	if e != nil {
		panic(e)
	}
	defer targetFile.Close()

	//
	// 3. 确保目标文件（如果是普通文件而非块设备）有足够大小
	//    避免 WriteAt 在稀疏文件场景下出现问题（部分文件系统对超出当前大小的 WriteAt 处理不一致）
	//

	if fi, statErr := targetFile.Stat(); statErr == nil && fi.Mode().IsRegular() {
		if e := targetFile.Truncate(int64(originSize)); e != nil {
			panic(e)
		}
	}

	//
	// 4. 执行镜像复制：只搬运位图中标记为"已使用"的块
	//

	copied, e := bmp.MirrorFs(originFile, targetFile)
	if e != nil {
		panic(e)
	}

	logger.Infof("testMirrorFs: origin=%s target=%s copied=%d bytes (total=%d bytes)",
		origin, target, copied, originSize)
}

func testBitmapExport() {
	if len(os.Args) < 3 {
		logger.Fatalf("usage: %s <origin> <offset> <size>", os.Args[0])
	}

	origin := os.Args[1]
	offset := extend.MustInt64(os.Args[2])
	size := extend.MustInt64(os.Args[3])

	p, err := btrfs.NewBitmapParser(origin, offset, size)
	if err != nil {
		panic(err)
	}
	bmp, e := p.Dump()
	if e != nil {
		panic(e)
	}
	logger.Debugf("testBitmapExport: \n%v", bmp.UsedSizeHuman())
}

func main() {
	logger.Debug(os.Args)

	//testBitmapExport()
	testMirrorFs()
}
