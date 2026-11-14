//go:build windows

package loadgen

import (
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// Windows 的 time.Now 单调时钟来自系统中断时间，分辨率约为毫秒级；
// 亚毫秒的回复延迟会被记成 0。基准改用 QueryPerformanceCounter。
var (
	performanceCounter = syscall.NewLazyDLL("kernel32.dll").NewProc("QueryPerformanceCounter")
	performanceFreq    = syscall.NewLazyDLL("kernel32.dll").NewProc("QueryPerformanceFrequency")
	frequencyOnce      sync.Once
	frequency          int64
)

func clockNow() int64 {
	var counter int64
	_, _, _ = performanceCounter.Call(uintptr(unsafe.Pointer(&counter)))
	return counter
}

func clockDuration(start, end int64) time.Duration {
	if end <= start {
		return 0
	}
	frequencyOnce.Do(func() {
		var value int64
		_, _, _ = performanceFreq.Call(uintptr(unsafe.Pointer(&value)))
		if value <= 0 {
			value = 1
		}
		frequency = value
	})
	delta := end - start
	return time.Duration(delta/frequency)*time.Second +
		time.Duration((delta%frequency)*int64(time.Second)/frequency)
}
