package loadgen

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
)

// EventRecord 是一条事件的测量结果。
type EventRecord struct {
	Index       int     `json:"index"`
	Seq         int64   `json:"seq"`
	MessageType string  `json:"message_type"`
	Text        string  `json:"text"`
	SentNS      int64   `json:"sent_ns"`
	ReplyNS     int64   `json:"reply_ns,omitempty"`
	LatencyMS   float64 `json:"latency_ms,omitempty"`
}

// SampleRecord 是一条进程资源采样。
type SampleRecord struct {
	AtMS       float64 `json:"at_ms"`
	CPUPercent float64 `json:"cpu_percent"`
	RSSBytes   uint64  `json:"rss_bytes"`
}

// Summary 是一次基准运行的汇总指标。
type Summary struct {
	Sent            int     `json:"sent"`
	PairedReplies   int     `json:"paired_replies"`
	UnpairedReplies int     `json:"unpaired_replies"`
	ExpectedReplies int     `json:"expected_replies"`
	MissingReplies  int     `json:"missing_replies"`
	ActionCount     int     `json:"action_count"`
	ActionErrors    int     `json:"action_errors"`
	ThroughputEPS   float64 `json:"throughput_eps"`
	LoadWallMS      float64 `json:"load_wall_ms"`

	LatencyCount  int     `json:"latency_count"`
	LatencyMeanMS float64 `json:"latency_mean_ms"`
	LatencyP50MS  float64 `json:"latency_p50_ms"`
	LatencyP90MS  float64 `json:"latency_p90_ms"`
	LatencyP95MS  float64 `json:"latency_p95_ms"`
	LatencyP99MS  float64 `json:"latency_p99_ms"`
	LatencyMaxMS  float64 `json:"latency_max_ms"`

	CPUAvgPercent float64 `json:"cpu_avg_percent"`
	CPUMaxPercent float64 `json:"cpu_max_percent"`
	RSSAvgBytes   float64 `json:"rss_avg_bytes"`
	RSSMaxBytes   uint64  `json:"rss_max_bytes"`
}

// Result 是写入磁盘的完整结果。
type Result struct {
	ProtocolVersion string         `json:"protocol_version"`
	Mode            string         `json:"mode"`
	Workload        string         `json:"workload"`
	Kind            string         `json:"kind"`
	Rate            float64        `json:"rate,omitempty"`
	DurationMS      float64        `json:"duration_ms,omitempty"`
	Count           int            `json:"count,omitempty"`
	SelfID          int64          `json:"self_id"`
	StartedAt       string         `json:"started_at"`
	FinishedAt      string         `json:"finished_at"`
	Summary         Summary        `json:"summary"`
	Events          []EventRecord  `json:"events"`
	Samples         []SampleRecord `json:"samples"`
}

// percentile 对已排序切片取分位数（线性插值）。
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	rank := (p / 100) * float64(len(sorted)-1)
	lower := int(math.Floor(rank))
	upper := int(math.Ceil(rank))
	if lower == upper {
		return sorted[lower]
	}
	fraction := rank - float64(lower)
	return sorted[lower]*(1-fraction) + sorted[upper]*fraction
}

// latencies 返回所有已配对回复的延迟毫秒列表（保持推送顺序，未排序）。
// 零延迟样本同样保留：它们代表计时分辨率的下界，而不是“没有回复”。
func latencies(events []EventRecord) []float64 {
	out := make([]float64, 0, len(events))
	for _, event := range events {
		if event.ReplyNS <= 0 {
			continue
		}
		latency := event.LatencyMS
		if latency <= 0 && event.SentNS > 0 {
			latency = float64(event.ReplyNS-event.SentNS) / 1e6
		}
		if latency < 0 {
			latency = 0
		}
		out = append(out, latency)
	}
	return out
}

// BuildSummary 由事件与采样记录计算汇总。
func BuildSummary(events []EventRecord, samples []SampleRecord, actionCount, actionErrors, loadWallMS float64, sent int) Summary {
	summary := Summary{
		Sent:         sent,
		ActionCount:  int(actionCount),
		ActionErrors: int(actionErrors),
		LoadWallMS:   loadWallMS,
	}
	if loadWallMS > 0 {
		summary.ThroughputEPS = float64(sent) / (loadWallMS / 1000)
	}
	for _, event := range events {
		if event.Seq > 0 {
			summary.ExpectedReplies++
			if event.ReplyNS > 0 {
				summary.PairedReplies++
			} else {
				summary.MissingReplies++
			}
		} else if expectedReplyText(messageSpec{Text: event.Text}) != "" {
			summary.UnpairedReplies++
		}
	}
	latencies := latencies(events)
	summary.LatencyCount = len(latencies)
	if len(latencies) > 0 {
		total := 0.0
		for _, value := range latencies {
			total += value
		}
		summary.LatencyMeanMS = total / float64(len(latencies))
		sorted := append([]float64(nil), latencies...)
		sort.Float64s(sorted)
		summary.LatencyP50MS = percentile(sorted, 50)
		summary.LatencyP90MS = percentile(sorted, 90)
		summary.LatencyP95MS = percentile(sorted, 95)
		summary.LatencyP99MS = percentile(sorted, 99)
		summary.LatencyMaxMS = sorted[len(sorted)-1]
	}
	if len(samples) > 0 {
		cpuTotal := 0.0
		var rssTotal, rssMax uint64
		cpuMax := 0.0
		for _, sample := range samples {
			cpuTotal += sample.CPUPercent
			if sample.CPUPercent > cpuMax {
				cpuMax = sample.CPUPercent
			}
			rssTotal += sample.RSSBytes
			if sample.RSSBytes > rssMax {
				rssMax = sample.RSSBytes
			}
		}
		summary.CPUAvgPercent = cpuTotal / float64(len(samples))
		summary.CPUMaxPercent = cpuMax
		summary.RSSAvgBytes = float64(rssTotal) / float64(len(samples))
		summary.RSSMaxBytes = rssMax
	}
	return summary
}

// WriteResult 把结果写成 JSON 文件（自动创建父目录）。
func WriteResult(path string, result Result) error {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
