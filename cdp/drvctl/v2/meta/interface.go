// Package biotrkmeta 实现 BIOTRKMETA 元数据文件格式 v1.0。
//
// 文件由以下区域组成：
//   - Header（固定 4096 字节）
//   - Protected Device Records（受保护设备及磁盘区间描述）
//   - Bitmap Unit Allocation Map（Bitmap Unit 分配状态）
//   - Bitmap Unit Data（位图实际数据）
//
// 详见 biotrkmeta_layout.md。
package meta
