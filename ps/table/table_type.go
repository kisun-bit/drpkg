package table

type TableType string

const (
	// TableTypeUnknown 未知分区表
	// 一般指磁盘第一个LBA中为非全0数据
	TableTypeUnknown TableType = "unknown"
	// TableTypeMBR MBR分区表
	TableTypeMBR TableType = "mbr"
	// TableTypeGPT GPT分区表
	TableTypeGPT TableType = "gpt"
	// TableTypeRaw 未初始化磁盘
	// 一般指磁盘第一个LBA中为全0数据
	TableTypeRaw TableType = "raw"
)
