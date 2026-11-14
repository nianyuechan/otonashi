package loadgen

import (
	"log/slog"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

// pidMonitor 以固定间隔采样一个进程的 CPU 占用与 RSS。pid 为 0 时是空监视器，
// 所有采样为空，便于在无目标 PID 的测试中复用。
type pidMonitor struct {
	pid      int
	interval time.Duration
	log      *slog.Logger

	proc      *process.Process
	done      chan struct{}
	sampleCh  chan SampleRecord
	samplesCh chan []SampleRecord
}

func newPidMonitor(pid int, interval time.Duration, log *slog.Logger) *pidMonitor {
	if interval <= 0 {
		interval = time.Second
	}
	return &pidMonitor{pid: pid, interval: interval, log: log, done: make(chan struct{})}
}

// Start 开始采样；进程不存在时记录日志并保持空采样。
func (m *pidMonitor) Start() {
	m.sampleCh = make(chan SampleRecord, 4096)
	m.samplesCh = make(chan []SampleRecord, 1)
	if m.pid <= 0 {
		close(m.done)
		m.samplesCh <- nil
		return
	}
	proc, err := process.NewProcess(int32(m.pid))
	if err != nil {
		m.log.Warn("无法找到被测进程，跳过资源采样", "pid", m.pid, "error", err)
		close(m.done)
		m.samplesCh <- nil
		return
	}
	m.proc = proc
	go m.loop()
}

func (m *pidMonitor) loop() {
	defer close(m.samplesCh)
	// interval 0 表示以上次调用以来的增量计算 CPU 百分比。
	_, _ = m.proc.Percent(0)
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-m.done:
			return
		case <-ticker.C:
			cpu, err := m.proc.Percent(0)
			if err != nil {
				m.log.Debug("CPU 采样失败", "error", err)
				cpu = 0
			}
			rss := uint64(0)
			if mem, err := m.proc.MemoryInfo(); err == nil {
				rss = mem.RSS
			}
			select {
			case m.sampleCh <- SampleRecord{
				AtMS:       float64(time.Now().UnixMilli()),
				CPUPercent: cpu,
				RSSBytes:   rss,
			}:
			default:
				m.log.Warn("采样缓冲已满，丢弃样本")
			}
		}
	}
}

// Stop 结束采样并返回全部样本。
func (m *pidMonitor) Stop() []SampleRecord {
	select {
	case <-m.done:
	default:
		close(m.done)
	}
	return <-m.samplesCh
}

// samples 返回当前已收到的样本（阻塞到 Stop 之后由 samplesCh 提供）。
func (m *pidMonitor) samples() []SampleRecord {
	// Run 在 Stop 前调用本方法时，返回已缓冲样本的快照。
	if m.proc == nil {
		return nil
	}
	// 直接读取已缓冲的样本，避免与 Stop 的移交语义冲突。
	all := make([]SampleRecord, 0, len(m.sampleCh))
	for {
		select {
		case sample := <-m.sampleCh:
			all = append(all, sample)
		default:
			return all
		}
	}
}
