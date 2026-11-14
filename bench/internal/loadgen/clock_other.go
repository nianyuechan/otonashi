//go:build !windows

package loadgen

import "time"

func clockNow() int64 { return time.Now().UnixNano() }

func clockDuration(start, end int64) time.Duration {
	if end <= start {
		return 0
	}
	return time.Duration(end - start)
}
