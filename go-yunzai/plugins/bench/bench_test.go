package bench

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/trss-yunzai/go-yunzai/pkg/plugin/sdk"
)

type recordedRule struct {
	rule    sdk.Rule
	handler sdk.HandlerFunc
}

type fakeRouter struct {
	accepts int
	tasks   int
	rules   []recordedRule
}

func (f *fakeRouter) Rule(rule sdk.Rule, handler sdk.HandlerFunc) error {
	f.rules = append(f.rules, recordedRule{rule: rule, handler: handler})
	return nil
}

func (f *fakeRouter) Accept(sdk.AcceptFunc) error {
	f.accepts++
	return nil
}

func (f *fakeRouter) Task(sdk.Task, sdk.TaskFunc) error {
	f.tasks++
	return nil
}

func TestRegisterMatchesOldPluginRuleSet(t *testing.T) {
	router := &fakeRouter{}
	if err := New().Register(router); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if len(router.rules) != 3+len(missRules) {
		t.Fatalf("expected %d rules, got %d", 3+len(missRules), len(router.rules))
	}
	if router.accepts != 0 || router.tasks != 0 {
		t.Fatalf("bench plugin must not register accepts or tasks, got %d/%d", router.accepts, router.tasks)
	}
	for _, pattern := range missRules {
		if _, err := regexp.Compile(pattern); err != nil {
			t.Fatalf("miss rule %q does not compile: %v", pattern, err)
		}
	}
	for i, rule := range router.rules {
		if rule.rule.Event != "message" {
			t.Fatalf("rule %d event = %q, want message", i, rule.rule.Event)
		}
		if rule.rule.Pattern == "" && i >= 3 {
			t.Fatalf("rule %d must have a pattern", i)
		}
	}
}

// TestMissRulesDoNotMatchLoadText 保证扫描负载文本不会命中任何规则，
// 否则回复延迟样本会被污染。
func TestMissRulesDoNotMatchLoadText(t *testing.T) {
	loadTexts := scanLoadTexts()
	compiled := make([]*regexp.Regexp, 0, len(missRules)+3)
	for _, pattern := range append([]string{`^#ping$`, `^#help$`, `^bench (\d+)$`}, missRules...) {
		compiled = append(compiled, regexp.MustCompile(pattern))
	}
	for _, text := range loadTexts {
		for i, re := range compiled {
			if re.MatchString(text) {
				t.Fatalf("scan load text %q matches rule %d", text, i)
			}
		}
	}
}

func TestEchoHandlerRepliesWithSequence(t *testing.T) {
	router := &fakeRouter{}
	if err := New().Register(router); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	var echoHandler sdk.HandlerFunc
	for _, rule := range router.rules {
		if rule.rule.ID == "bench.echo" {
			echoHandler = rule.handler
		}
	}
	if echoHandler == nil {
		t.Fatal("bench.echo rule not registered")
	}

	for _, seq := range []string{"1", "42", "999999"} {
		ctx := &sdk.Context{
			Event: &sdk.Event{
				MessageType: "private",
				UserID:      10001,
				Message: []sdk.Segment{{
					Type: "text",
					Data: map[string]any{"text": "bench " + seq},
				}},
			},
		}
		sender := &stubSender{}
		ctx.Sender = sender
		result, err := echoHandler(context.Background(), ctx)
		if err != nil {
			t.Fatalf("echo handler failed for seq %s: %v", seq, err)
		}
		if result != sdk.Stop {
			t.Fatalf("echo handler result = %v, want Stop", result)
		}
		want := "[bench] " + seq
		if len(sender.messages) != 1 || sender.messages[0] != want {
			t.Fatalf("reply = %v, want [%s]", sender.messages, want)
		}
	}
}

func TestEchoHandlerRejectsNonNumeric(t *testing.T) {
	router := &fakeRouter{}
	if err := New().Register(router); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	var echoHandler sdk.HandlerFunc
	for _, rule := range router.rules {
		if rule.rule.ID == "bench.echo" {
			echoHandler = rule.handler
		}
	}
	ctx := &sdk.Context{
		Event: &sdk.Event{
			MessageType: "private",
			UserID:      10001,
			Message: []sdk.Segment{{
				Type: "text",
				Data: map[string]any{"text": "bench abc"},
			}},
		},
		Sender: &stubSender{},
	}
	// 正则 ^bench (\d+)$ 不匹配 "bench abc"，处理器必须报错而不是静默通过。
	_, err := echoHandler(context.Background(), ctx)
	if err == nil || !strings.Contains(err.Error(), "不应在基准负载中命中") && !strings.Contains(err.Error(), "命中但文本不匹配") {
		t.Fatalf("expected echo mismatch error, got %v", err)
	}
}

func TestDescriptorStability(t *testing.T) {
	descriptor := New().Descriptor()
	if descriptor.ID != "bench.workload" || descriptor.Priority != Priority {
		t.Fatalf("descriptor changed: %+v", descriptor)
	}
	if descriptor.Version != "1" {
		t.Fatalf("workload version = %q, want 1", descriptor.Version)
	}
}

type stubSender struct {
	messages []string
}

func (s *stubSender) SendMessage(_ context.Context, outgoing sdk.Outgoing) (sdk.SendResult, error) {
	var text strings.Builder
	for _, segment := range outgoing.Message {
		if value, ok := segment.Data["text"].(string); ok {
			text.WriteString(value)
		}
	}
	s.messages = append(s.messages, text.String())
	if outgoing.MessageType == "private" && outgoing.UserID <= 0 {
		return sdk.SendResult{}, errors.New("private requires user id")
	}
	return sdk.SendResult{MessageID: 1}, nil
}

// scanLoadTexts 与 bench 模块负载器的扫描文本列表保持一致。
func scanLoadTexts() []string {
	return []string{
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
}

// 保证 miss 规则数量符合工作负载定义（3 条命中规则 + 28 条扫描规则）。
func TestWorkloadShape(t *testing.T) {
	if len(missRules) != 28 {
		t.Fatalf("missRules = %d, want 28", len(missRules))
	}
	if Priority != 50 {
		t.Fatalf("priority = %d, want 50", Priority)
	}
}
