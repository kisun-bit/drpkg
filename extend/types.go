package extend

type SegmentLayoutType string

const (
	SegmentLayoutTypeUnknown SegmentLayoutType = "unknown"
	SegmentLayoutTypeLine    SegmentLayoutType = "line"
	SegmentLayoutTypeMirror  SegmentLayoutType = "mirror"
)

// Segment 表示卷在设备上的连续数据区间
type Segment struct {
	Device string `json:"device"` // 设备路径，例如 "/dev/sda" 或 "\\.\PHYSICALDRIVE0"
	Start  uint64 `json:"start"`  // 起始偏移量
	Size   uint64 `json:"size"`   // 区间大小
}

type volumeSegment struct {
	start int64
	size  int64
}
