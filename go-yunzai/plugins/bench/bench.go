// Package bench 提供阶段零性能基准插件。规则集、顺序、优先级和回复文案必须
// 与 bench/old-plugin/index.js 的旧版插件保持一致，任何一侧修改都要同步另一侧，
// 并在基线报告中标注工作负载版本。
package bench

import (
	"context"
	"fmt"
	"regexp"
	"strconv"

	"github.com/nianyuechan/otonashi/go-yunzai/pkg/plugin/sdk"
)

// Priority 与旧版插件 priority 保持一致。
const Priority = 50

// Version 标识工作负载定义；新旧两侧必须一致。
const Version = "1"

var helpReply = "[bench] 帮助：阶段零基准规则集 v1（ping/help/bench + 28 条扫描规则）"

var echoPattern = regexp.MustCompile(`^bench (\d+)$`)

// Plugin 是基准插件，仅覆盖消息匹配与回复链路，不访问存储和渲染。
type Plugin struct{}

// New 创建基准插件实例。
func New() sdk.Plugin { return &Plugin{} }

func (p *Plugin) Descriptor() sdk.Descriptor {
	return sdk.Descriptor{
		ID:           "bench.workload",
		Version:      Version,
		Priority:     Priority,
		Events:       []string{"message"},
		Capabilities: []sdk.Capability{sdk.CapabilityMessages},
	}
}

// missRules 是 28 条在基准负载中永不命中的真实风格命令，保证每条消息都要
// 扫描完整个规则集。两侧的顺序与正则字符串保持一致。
var missRules = []string{
	`^#签到$`, `^#体力$`, `^#状态$`, `^#今日运势$`, `^#天气\s+.+$`,
	`^#菜单$`, `^#角色查询\s+.+$`, `^#深境螺旋$`, `^#原神卡片$`, `^#星穹铁道签到$`,
	`^#崩坏三签到$`, `^#绝区零签到$`, `^#米游社签到$`, `^#抽卡记录\s+.+$`, `^#面板\s+.+$`,
	`^#更新面板$`, `^#帮助列表$`, `^#绑定\s+.+$`, `^#解绑$`, `^#深渊\s+.+$`,
	`^#群管\s+.+$`, `^#点歌\s+.+$`, `^#歌词\s+.+$`, `^#翻译\s+.+$`, `^#百科\s+.+$`,
	`^#新闻$`, `^#日历$`, `^#汇率\s+.+$`,
}

func (p *Plugin) Register(router sdk.Router) error {
	rules := []sdk.Rule{
		{ID: "bench.ping", Event: "message", Pattern: `^#ping$`},
		{ID: "bench.help", Event: "message", Pattern: `^#help$`},
		{ID: "bench.echo", Event: "message", Pattern: `^bench (\d+)$`},
	}
	for i, pattern := range missRules {
		rules = append(rules, sdk.Rule{ID: fmt.Sprintf("bench.miss.%02d", i), Event: "message", Pattern: pattern})
	}

	if err := router.Rule(rules[0], func(ctx context.Context, pluginCtx *sdk.Context) (sdk.Result, error) {
		_, err := pluginCtx.Reply(ctx, sdk.Segment{Type: "text", Data: map[string]any{"text": "[bench] pong"}})
		return sdk.Stop, err
	}); err != nil {
		return err
	}
	if err := router.Rule(rules[1], func(ctx context.Context, pluginCtx *sdk.Context) (sdk.Result, error) {
		_, err := pluginCtx.Reply(ctx, sdk.Segment{Type: "text", Data: map[string]any{"text": helpReply}})
		return sdk.Stop, err
	}); err != nil {
		return err
	}
	if err := router.Rule(rules[2], handleEcho); err != nil {
		return err
	}
	for i, rule := range rules[3:] {
		index := i
		if err := router.Rule(rule, func(ctx context.Context, pluginCtx *sdk.Context) (sdk.Result, error) {
			return sdk.Stop, fmt.Errorf("bench.miss.%02d 不应在基准负载中命中", index)
		}); err != nil {
			return err
		}
	}
	return nil
}

func handleEcho(ctx context.Context, pluginCtx *sdk.Context) (sdk.Result, error) {
	text := ""
	for _, segment := range pluginCtx.Event.Message {
		if segment.Type == "text" {
			if value, ok := segment.Data["text"].(string); ok {
				text += value
			}
		}
	}
	match := echoPattern.FindStringSubmatch(text)
	if match == nil {
		return sdk.Stop, fmt.Errorf("bench.echo 命中但文本不匹配: %q", text)
	}
	_, err := pluginCtx.Reply(ctx, sdk.Segment{Type: "text", Data: map[string]any{
		"text": "[bench] " + match[1],
	}})
	if err != nil {
		return sdk.Stop, err
	}
	if _, err := strconv.ParseInt(match[1], 10, 64); err != nil {
		return sdk.Stop, fmt.Errorf("bench.echo 序号无效: %q", match[1])
	}
	return sdk.Stop, nil
}

func (p *Plugin) Start(context.Context) error { return nil }

func (p *Plugin) Stop(context.Context) error { return nil }
