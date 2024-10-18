package basic

import "time"

func GetTimeByMicrosoftTimestamp(timestamp uint64) time.Time {
	// 将微软时间戳转换为Unix时间戳（纳秒）
	// 微软时间戳是以100纳秒为单位的
	// 需要减去Unix时间戳的起始点：1601年1月1日
	unixEpochStart := uint64(116444736000000000) // 1601年1月1日至1970年1月1日的100纳秒计时单位差值
	timestampInNano := int64((timestamp - unixEpochStart)) * 100
	return time.Unix(0, timestampInNano)
}
