// Package loadgen 实现阶段零基准负载器：以 OneBot v11 协议向被测机器人推送
// 消息事件，并测量"事件发出 → 回复动作到达"的端到端延迟。
package loadgen

import (
	"errors"
	"flag"
	"fmt"
	"time"
)

// Mode 决定负载器的 WebSocket 角色。
type Mode string

const (
	// ModeServer 监听端口，等待被测机器人连入（go-yunzai 正向 WS 客户端）。
	ModeServer Mode = "server"
	// ModeClient 主动连入被测机器人（旧版 TRSS-Yunzai 反向 WS 服务端）。
	ModeClient Mode = "client"
)

// Workload 选择消息构成。
type Workload string

const (
	// WorkloadReply 全部消息由命中规则回复，其中一半携带可精确配对的序号。
	WorkloadReply Workload = "reply"
	// WorkloadScan 全部消息不命中任何规则，测量纯扫描成本。
	WorkloadScan Workload = "scan"
	// WorkloadRender 全部消息为 #render 命令（主人权限，固定页面），驱动阶段
	// 零渲染链路基线；回复为图片或降级文本，不参与延迟配对。
	WorkloadRender Workload = "render"
)

// Kind 选择负载节奏。
type Kind string

const (
	// KindSteady 按固定速率持续推送。
	KindSteady Kind = "steady"
	// KindBurst 尽快推送固定数量的事件。
	KindBurst Kind = "burst"
)

// ProtocolVersion 标识工作负载定义；新旧两侧与 docs/plan.md 中的说明保持一致。
const ProtocolVersion = "1"

// Config 是一次基准运行的完整参数。
type Config struct {
	Mode           Mode
	URL            string
	Listen         string
	Token          string
	Workload       Workload
	Kind           Kind
	Rate           float64
	Duration       time.Duration
	Count          int
	SelfID         int64
	UserID         int64
	GroupID        int64
	MessageType    string
	GroupCount     int
	UserCount      int
	Warmup         time.Duration
	ReplyGrace     time.Duration
	SamplePID      int
	SampleInterval time.Duration
	OutPath        string
}

func (c Config) validate() error {
	switch c.Mode {
	case ModeServer, ModeClient:
	default:
		return fmt.Errorf("mode 必须是 server 或 client，当前 %q", c.Mode)
	}
	switch c.Workload {
	case WorkloadReply, WorkloadScan, WorkloadRender:
	default:
		return fmt.Errorf("workload 必须是 reply、scan 或 render，当前 %q", c.Workload)
	}
	switch c.Kind {
	case KindSteady, KindBurst:
	default:
		return fmt.Errorf("scenario 必须是 steady 或 burst，当前 %q", c.Kind)
	}
	if c.Mode == ModeClient && c.URL == "" {
		return errors.New("client 模式需要 --url 指向被测机器人 WS 地址")
	}
	if c.Mode == ModeServer && c.Listen == "" {
		return errors.New("server 模式需要 --listen 监听地址")
	}
	if c.Kind == KindSteady && c.Rate <= 0 {
		return errors.New("steady 场景需要 --rate > 0")
	}
	if c.Kind == KindSteady && c.Duration <= 0 {
		return errors.New("steady 场景需要 --duration > 0")
	}
	if c.Kind == KindBurst && c.Count <= 0 {
		return errors.New("burst 场景需要 --count > 0")
	}
	if c.SelfID <= 0 || c.UserID <= 0 || c.GroupID <= 0 {
		return errors.New("self-id/user-id/group-id 必须为正数")
	}
	if c.MessageType != "" && c.MessageType != "private" && c.MessageType != "group" {
		return errors.New("message-type 必须是 private 或 group")
	}
	if c.GroupCount < 0 || c.UserCount < 0 {
		return errors.New("group-count/user-count 不能为负")
	}
	// group 模式下 push 用 UserCount 做模运算，0 会在第一条消息触发除零 panic。
	if c.MessageType == "group" && (c.GroupCount <= 0 || c.UserCount <= 0 || c.GroupID <= 0) {
		return errors.New("group 模式需要正数 group-count/user-count/group-id")
	}
	if c.Warmup < 0 || c.ReplyGrace < 0 {
		return errors.New("warmup/grace 不能为负")
	}
	if c.SamplePID < 0 {
		return errors.New("pid 不能为负")
	}
	if c.SampleInterval <= 0 {
		return errors.New("sample-interval 必须为正数")
	}
	if c.OutPath == "" {
		return errors.New("out 结果文件路径不能为空")
	}
	return nil
}

// ParseFlags 从命令行构造 Config，并做默认值填充。
func ParseFlags(args []string) (Config, error) {
	cfg := Config{}
	fs := flag.NewFlagSet("onebot-bench", flag.ContinueOnError)
	fs.StringVar((*string)(&cfg.Mode), "mode", string(ModeServer), "server 或 client")
	fs.StringVar(&cfg.URL, "url", "", "client 模式下被测机器人的 ws:// 地址")
	fs.StringVar(&cfg.Listen, "listen", "127.0.0.1:3002", "server 模式监听地址")
	fs.StringVar(&cfg.Token, "token", "bench-token", "OneBot access token（只做声明，负载器不校验）")
	fs.StringVar((*string)(&cfg.Workload), "workload", string(WorkloadReply), "reply、scan 或 render（render 为固定 #render 页面，仅主人权限）")
	fs.StringVar((*string)(&cfg.Kind), "scenario", string(KindSteady), "steady 或 burst")
	fs.Float64Var(&cfg.Rate, "rate", 20, "steady 场景每秒消息数")
	fs.DurationVar(&cfg.Duration, "duration", 60*time.Second, "steady 场景持续时长")
	fs.IntVar(&cfg.Count, "count", 500, "burst 场景消息总数")
	fs.Int64Var(&cfg.SelfID, "self-id", 2081344177, "机器人账号")
	fs.Int64Var(&cfg.UserID, "user-id", 10001, "私聊用户")
	fs.Int64Var(&cfg.GroupID, "group-id", 20001, "群号")
	fs.StringVar(&cfg.MessageType, "message-type", "private", "private 或 group")
	fs.IntVar(&cfg.GroupCount, "group-count", 1, "group 模式下轮转的群数量")
	fs.IntVar(&cfg.UserCount, "user-count", 1, "轮转的发送者用户数量")
	fs.DurationVar(&cfg.Warmup, "warmup", 2*time.Second, "连接建立后的预热等待")
	fs.DurationVar(&cfg.ReplyGrace, "grace", 10*time.Second, "停止推送后等待回复的时长")
	fs.IntVar(&cfg.SamplePID, "pid", 0, "被测进程 PID，用于 CPU/内存采样")
	fs.DurationVar(&cfg.SampleInterval, "sample-interval", time.Second, "资源采样间隔")
	fs.StringVar(&cfg.OutPath, "out", "", "结果 JSON 输出路径")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
