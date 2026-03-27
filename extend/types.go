package extend

type SegmentLayoutType string

const (
	SegmentLayoutTypeUnknown SegmentLayoutType = "unknown"
	SegmentLayoutTypeLine    SegmentLayoutType = "line"
	SegmentLayoutTypeMirror  SegmentLayoutType = "mirror"
)

// Segment 表示卷在物理磁盘上的连续数据区间
type Segment struct {
	Disk  string `json:"disk"`  // 磁盘路径，例如 "/dev/sda" 或 "\\.\PHYSICALDRIVE0"
	Start uint64 `json:"start"` // 起始偏移量
	Size  uint64 `json:"size"`  // 区间大小
}

type volumeSegment struct {
	start int64
	size  int64
}
