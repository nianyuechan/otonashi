package loadgen

import "log/slog"
import "time"

// NewPidMonitor 创建进程资源监视器（供 cmd 层使用）。
func NewPidMonitor(pid int, interval time.Duration, log *slog.Logger) *pidMonitor {
	return newPidMonitor(pid, interval, log)
}
