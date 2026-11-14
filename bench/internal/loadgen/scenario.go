package loadgen

import (
	"strconv"
)

// scanTexts 与 go-yunzai plugins/bench 的扫描文本列表保持一致，全部不命中
// 任何基准规则。修改时必须同步 plugins/bench/bench_test.go。
var scanTexts = []string{
	"今天感觉好累啊什么都不想做",
	"这个视频太好笑了哈哈哈哈",
	"晚上吃火锅还是烧烤呢纠结",
	"我刚看完一部电影剧情还不错",
	"外面又开始下雨了记得带伞",
	"最近有没有什么好看的电视剧",
	"这台新手机拍照效果真不错",
	"周末打算去爬山呼吸一下新鲜空气",
	"公司楼下的咖啡店换新菜单了",
	"今天的地铁怎么这么挤啊",
	"游戏更新之后版本体验好多了",
	"这天气太热了只想待在空调房里",
}

// messageSpec 是一条待推送事件。
type messageSpec struct {
	// Seq 大于 0 表示使用可精确配对的 bench 规则；0 表示固定文本或扫描文本。
	Seq    int64
	Text   string
	IsScan bool
}

// generator 按 Workload 生成确定性消息序列。基准 v1 只产生私聊消息。
type generator struct {
	workload Workload
	nextSeq  int64
}

func newGenerator(workload Workload) *generator {
	return &generator{workload: workload, nextSeq: 1}
}

func (g *generator) next(index int) messageSpec {
	if g.workload == WorkloadScan {
		return messageSpec{Text: scanTexts[index%len(scanTexts)], IsScan: true}
	}
	if g.workload == WorkloadRender {
		return messageSpec{Text: "#render"}
	}
	// reply 负载：偶数序号使用可配对的 bench 规则，奇数序号交替 #ping/#help。
	if index%2 == 0 {
		seq := g.nextSeq
		g.nextSeq++
		return messageSpec{Seq: seq, Text: "bench " + strconv.FormatInt(seq, 10)}
	}
	if (index/2)%2 == 0 {
		return messageSpec{Text: "#ping"}
	}
	return messageSpec{Text: "#help"}
}

// expectedReplyText 计算基准插件对一条消息的期望回复文本，空串表示无回复。
func expectedReplyText(spec messageSpec) string {
	switch {
	case spec.Seq > 0:
		return "[bench] " + strconv.FormatInt(spec.Seq, 10)
	case spec.Text == "#ping":
		return "[bench] pong"
	case spec.Text == "#help":
		return "[bench] 帮助：阶段零基准规则集 v1（ping/help/bench + 28 条扫描规则）"
	default:
		return ""
	}
}

// extractSequence 从回复动作的文本参数中提取序号。#ping/#help 的固定文本
// 回复属于正常情况，返回 0 表示无需配对；只有 "[bench] <数字>" 才配对。
func extractSequence(text string) int64 {
	const prefix = "[bench] "
	if len(text) <= len(prefix) || text[:len(prefix)] != prefix {
		return 0
	}
	seq, err := strconv.ParseInt(text[len(prefix):], 10, 64)
	if err != nil || seq <= 0 {
		return 0
	}
	return seq
}
