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
	Sent            int `json:"sent"`
	PairedReplies   int `json:"paired_replies"`
	UnpairedReplies int `json:"unpaired_replies"`
	ExpectedReplies int `json:"expected_replies"`
	MissingReplies  int `json:"missing_replies"`
	// ExpectedTextReplies 统计本应产生固定文本回复（#ping/#help 等）的事件数。
	// fixed text 没有唯一序号，不能与某一条事件精确配对。
	ExpectedTextReplies int `json:"expected_text_replies"`
	// MissingTextReplies 是固定文本回复的保守缺失下界：期望数减去已观察到的
	// 非序号文本回复数，最小为 0。它不等于“全量消息精确丢失数”。
	MissingTextReplies int     `json:"missing_text_replies"`
	ActionCount        int     `json:"action_count"`
	ActionErrors       int     `json:"action_errors"`
	ThroughputEPS      float64 `json:"throughput_eps"`
	LoadWallMS         float64 `json:"load_wall_ms"`
	// TextReplies 统计收到的非序号文本回复（#ping/#help 固定文本、渲染降级
	// 文本）；render 负载用它证明失败路径发出了可读降级文本。
	TextReplies      int      `json:"text_replies"`
	TextReplySamples []string `json:"text_reply_samples,omitempty"`
	// ActualInjectionEPS 由事件首尾 sent_ns 计算：负载窗口内的持续注入速率。
	// 与命令行 requested（目标速率）和 ThroughputEPS（分母含 grace）区分，
	// 容量判定以这个口径为准。
	ActualInjectionEPS float64 `json:"actual_injection_eps"`

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
	ProtocolVersion string  `json:"protocol_version"`
	Mode            string  `json:"mode"`
	Workload        string  `json:"workload"`
	Kind            string  `json:"kind"`
	Rate            float64 `json:"rate,omitempty"`
	DurationMS      float64 `json:"duration_ms,omitempty"`
	Count           int     `json:"count,omitempty"`
	SelfID          int64   `json:"self_id"`
	// 消息构造参数自记录：容量验证的参数证据取自 JSON 本身，而不是文件名。
	UserID      int64          `json:"user_id,omitempty"`
	GroupID     int64          `json:"group_id,omitempty"`
	MessageType string         `json:"message_type,omitempty"`
	GroupCount  int            `json:"group_count,omitempty"`
	UserCount   int            `json:"user_count,omitempty"`
	StartedAt   string         `json:"started_at"`
	FinishedAt  string         `json:"finished_at"`
	Summary     Summary        `json:"summary"`
	Events      []EventRecord  `json:"events"`
	Samples     []SampleRecord `json:"samples"`
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

// actualInjectionEPS 用事件首尾 sent_ns 计算持续注入速率：sent / 事件时间跨度。
// 单一事件或时间跨度为 0 时返回 0（无法定义速率）。
func actualInjectionEPS(events []EventRecord, sent int) float64 {
	first, last := int64(0), int64(0)
	for _, event := range events {
		if event.SentNS <= 0 {
			continue
		}
		if first == 0 || event.SentNS < first {
			first = event.SentNS
		}
		if event.SentNS > last {
			last = event.SentNS
		}
	}
	if first == 0 || last <= first {
		return 0
	}
	return float64(sent) / (float64(last-first) / 1e9)
}

// BuildSummary 由事件与采样记录计算汇总。
func BuildSummary(events []EventRecord, samples []SampleRecord, actionCount, actionErrors, loadWallMS float64, sent int, textReplies int, textSamples []string) Summary {
	summary := Summary{
		Sent:             sent,
		ActionCount:      int(actionCount),
		ActionErrors:     int(actionErrors),
		LoadWallMS:       loadWallMS,
		TextReplies:      textReplies,
		TextReplySamples: textSamples,
	}
	if loadWallMS > 0 {
		summary.ThroughputEPS = float64(sent) / (loadWallMS / 1000)
	}
	summary.ActualInjectionEPS = actualInjectionEPS(events, sent)
	expectedTextReplies := 0
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
			expectedTextReplies++
		}
	}
	summary.ExpectedTextReplies = expectedTextReplies
	if expectedTextReplies > textReplies {
		summary.MissingTextReplies = expectedTextReplies - textReplies
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
