package util

import (
	"time"
)

var (
	loc = time.UTC
)

// Get the current approximate time - this is useful for code that
// needs to check the time very frequently but does not care too much
// about accuracy.
func Now() time.Time {
	return GetTime().Now()
}

func SetGlobalTimezone(timezone string) error {
	var err error

	loc, err = time.LoadLocation(timezone)
	return err
}

func ParseTimeFromInt64(t int64) time.Time {
	var sec, dec int64

	// Maybe it is in ns
	if t > 20000000000000000 { // 11 October 2603 in microsec
		dec = t

	} else if t > 20000000000000 { // 11 October 2603 in milliseconds
		dec = t * 1000

	} else if t > 20000000000 { // 11 October 2603 in seconds
		dec = t * 1000000

	} else {
		sec = t
	}

	if sec == 0 && dec == 0 {
		return time.Time{}
	}

	return time.Unix(int64(sec), int64(dec))
}
