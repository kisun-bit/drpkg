package hkd

import "fmt"

// Vol 对象用于存储 HKD 磁盘备份的 Cluster 数据。
//
// 一个 HKD 磁盘备份由一个 index 对象和多个 vol 分卷对象组成。
// 每个 vol 分卷对象包含若干个背靠背写入的 Cluster，index 对象记录每个
// Cluster 所属的 vol 分卷以及在分卷内的字节偏移。
//
// 由于不同文件系统以及 CIFS 等共享文件系统对单个文件的大小存在限制，
// HKD 采用分卷方式存储备份数据，将一个磁盘的备份数据拆分到多个 vol 对象中，
// 从而突破单文件大小限制，也利于控制单个文件大小并提升写入效率。

// defaultVolMaxSize 是单个 vol 分卷对象的最大容量，默认 1 TiB。
// 当当前 vol 达到容量上限后，将创建新的 vol 分卷继续存储后续 Cluster。
const defaultVolMaxSize uint64 = 1 << 40

// volFileName 返回第 volID 个分卷的文件名（不含目录前缀），形如 "00000000.hkc"。
func volFileName(volID uint32) string {
	return fmt.Sprintf("%08d.hkc", volID)
}
