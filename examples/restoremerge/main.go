// Command restoremerge 演示磁盘恢复时用 restore.Merger 把小 IO 日志流
// 合并成大块写入。
//
// 背景：原机采集的日志往往包含大量小 IO（512B/4KB/8KB 交替），
// 严格按原始粒度 replay 会产生海量写系统调用与小 IO，拖低瞬时恢复速度。
// restore.Merger 把相邻小块在内存中攒批，攒满后作为大块一次提交，
// 在保持与逐块 replay 完全等价的前提下大幅减少提交次数。
//
// 用法：
//
//	go run ./examples/restoremerge             # 恢复到临时镜像文件并校验内容
//	go run ./examples/restoremerge /path/img   # 恢复到指定镜像文件
package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"

	humanize "github.com/dustin/go-humanize"
	"github.com/kisun-bit/drpkg/disk/restore"
)

const (
	kib         = uint64(1024)
	mib         = 1024 * kib
	defaultCap  = 16 * mib // 未指定目标时创建的临时镜像容量
	granularity = uint64(512)
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "restoremerge:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	// ---- 1. 确定目标磁盘 ----
	// 不传参数时创建一个临时镜像文件；也可以指定已有的镜像文件。
	// 块设备（如 /dev/sdb）同样可用，直接作为参数传入即可。
	targetPath := ""
	if len(os.Args) > 1 {
		targetPath = os.Args[1]
	} else {
		dir, err := os.MkdirTemp("", "restoremerge-*")
		if err != nil {
			return err
		}
		targetPath = filepath.Join(dir, "target.img")
	}
	capacity, err := ensureImage(targetPath, defaultCap)
	if err != nil {
		return err
	}
	fmt.Printf("目标: %s (容量 %s)\n", targetPath, humanize.Bytes(capacity))

	// ---- 2. 打开恢复目标 ----
	// Granularity 是去重位图粒度（一个 bit 代表多少字节），
	// 日志块的偏移必须按它对齐。
	target, err := restore.OpenPath(targetPath, &restore.Option{
		Granularity: granularity,
	})
	if err != nil {
		return err
	}
	defer target.Close()

	// ---- 3. 创建合并器 ----
	// MaxBufferBytes 是攒批缓冲区上限，攒满即刷出：
	//   - 越大：提交的 IO 越大、次数越少，但内存占用越高；
	//   - 为 0 使用默认 4MiB（restore.DefaultMergeBuffer），对 NVMe 顺序写已足够。
	// 单个块长度达到该上限时绕过缓冲区直写，不会拷贝大块数据。
	merger, err := target.NewMerger(&restore.MergeOption{
		MaxBufferBytes: 4 * mib,
	})
	if err != nil {
		return err
	}

	// ---- 4. 把日志块流喂给合并器 ----
	// 实际恢复时，这里的 blocks 来自日志解析器。
	// 示例生成一段贴近原机的日志：顺序小 IO（512B/4KB/8KB 交替）为主，
	// 中间留一个空洞，末尾再重复回放一批已写过的块（模拟重复日志条目）。
	blocks, expected := genLog(capacity)
	for _, blk := range blocks {
		if err := merger.Append(ctx, blk); err != nil {
			return fmt.Errorf("Append(offset=%d): %w", blk.Offset, err)
		}
	}

	// ---- 5. 关闭目标前必须 Flush ----
	// Append 不保证立即落盘：尾部不足一个缓冲区的小块会留在内存里，
	// 不 Flush 就会丢数据。
	if err := merger.Flush(ctx); err != nil {
		return fmt.Errorf("Flush: %w", err)
	}

	// ---- 6. 打印合并统计 ----
	// 恢复过程中也可以随时调用 Stats() 上报进度与合并效果。
	s := merger.Stats()
	fmt.Printf("日志小 IO:   %d 个，共 %s\n", s.BlocksIn, humanize.Bytes(s.BytesIn))
	fmt.Printf("提交大 IO:   %d 次，平均 %s/次\n", s.IOSubmitted(), humanize.Bytes(s.AvgIOSize()))
	fmt.Printf("实际写入:    %s（位图去重跳过 %s）\n", humanize.Bytes(s.Written), humanize.Bytes(s.Skipped))

	// ---- 7. 校验恢复结果 ----
	// expected 是按"先写者胜"语义（与 Target 去重行为一致）计算的期望镜像，
	// 逐字节比对可验证合并写入与逐块 replay 完全等价。
	content, err := os.ReadFile(targetPath)
	if err != nil {
		return err
	}
	if !bytes.Equal(content, expected) {
		return fmt.Errorf("恢复内容与期望镜像不一致")
	}
	fmt.Println("内容校验通过：与期望镜像逐字节一致")
	return nil
}

// ensureImage 确保目标文件存在：存在则取其大小作为容量，
// 不存在则创建 size 字节的稀疏镜像。
func ensureImage(path string, size uint64) (uint64, error) {
	if fi, err := os.Stat(path); err == nil {
		if fi.Size() == 0 {
			return 0, fmt.Errorf("%s: 文件存在但大小为 0，无法作为恢复目标", path)
		}
		return uint64(fi.Size()), nil
	} else if !os.IsNotExist(err) {
		return 0, err
	}

	f, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if err := f.Truncate(int64(size)); err != nil {
		return 0, err
	}
	return size, nil
}

// genLog 生成贴近原机的小 IO 日志流（确定性，便于校验）：
//
//  1. 顺序小 IO：512B/4KB/8KB/4KB/512B 交替，覆盖 [0, gapStart) 与
//     [gapStart+gapLen, streamEnd)，中间留一个空洞（模拟日志空洞）；
//  2. 重复回放：把开头一批块原样再提交一遍（模拟重复日志条目），
//     这些块会被去重位图整体跳过。
//
// 返回值：块序列，以及按"先写者胜"语义计算的期望目标镜像。
func genLog(capacity uint64) ([]*restore.Block, []byte) {
	expected := make([]byte, capacity)
	firstWrite := make([]bool, capacity/granularity)
	sizes := []uint64{512, 4 * kib, 8 * kib, 4 * kib, 512}

	var blocks []*restore.Block

	// 1) 顺序小 IO，中间留一个空洞
	gapStart := capacity / 2
	gapLen := 64 * kib
	streamEnd := capacity * 3 / 4

	i := 0
	appendRun := func(from, to uint64) {
		pos := from
		for pos < to {
			size := sizes[i%len(sizes)]
			if pos+size > to {
				size = to - pos
			}
			blk := makeBlock(pos, size)
			blocks = append(blocks, blk)
			apply(expected, firstWrite, pos, blk.Data)
			pos += size
			i++
		}
	}
	appendRun(0, gapStart)
	appendRun(gapStart+gapLen, streamEnd)

	// 2) 重复回放开头一批块（去重应全部跳过）
	dupCount := 512
	if dupCount > len(blocks) {
		dupCount = len(blocks)
	}
	blocks = append(blocks, blocks[:dupCount]...)

	return blocks, expected
}

// makeBlock 生成数据仅由偏移决定的块，保证同一偏移重复回放时内容一致。
func makeBlock(off, size uint64) *restore.Block {
	data := make([]byte, size)
	for j := range data {
		data[j] = byte((off + uint64(j)) % 251)
	}
	return &restore.Block{Offset: off, Length: size, Data: data}
}

// apply 按"先写者胜"把块数据应用到期望镜像（与 Target 的去重语义一致）。
// off 与 len(data) 均为粒度对齐。
func apply(expected []byte, firstWrite []bool, off uint64, data []byte) {
	g := granularity
	for b := off / g; b < (off+uint64(len(data)))/g; b++ {
		if firstWrite[b] {
			continue
		}
		firstWrite[b] = true
		from := b*g - off
		copy(expected[b*g:(b+1)*g], data[from:from+g])
	}
}
