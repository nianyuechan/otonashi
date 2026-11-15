package loadgen

import (
	"encoding/json"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPercentile(t *testing.T) {
	sorted := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	cases := []struct {
		p    float64
		want float64
	}{
		{50, 5.5}, {0, 1}, {100, 10}, {90, 9.1}, {95, 9.55},
	}
	for _, c := range cases {
		got := percentile(sorted, c.p)
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("percentile(%v) = %v, want %v", c.p, got, c.want)
		}
	}
	if got := percentile(nil, 50); got != 0 {
		t.Errorf("percentile(nil) = %v, want 0", got)
	}
}

func TestGeneratorReplyWorkloadDeterministic(t *testing.T) {
	g := newGenerator(WorkloadReply)
	seqs := []int64{}
	for i := 0; i < 100; i++ {
		spec := g.next(i)
		if i%2 == 0 {
			if spec.Seq == 0 {
				t.Fatalf("index %d 应携带配对序号", i)
			}
			if spec.Text != "bench "+jsonNumber(spec.Seq) {
				t.Fatalf("index %d 文本 %q 与序号不一致", i, spec.Text)
			}
			seqs = append(seqs, spec.Seq)
		} else {
			if spec.Seq != 0 || spec.IsScan {
				t.Fatalf("index %d 不应携带序号", i)
			}
		}
	}
	for i, seq := range seqs {
		if seq != int64(i+1) {
			t.Fatalf("配对序号应严格递增: index %d got %d want %d", i, seq, i+1)
		}
	}
}

func jsonNumber(value int64) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func TestGeneratorScanWorkloadNoHit(t *testing.T) {
	g := newGenerator(WorkloadScan)
	for i := 0; i < len(scanTexts)*3; i++ {
		spec := g.next(i)
		if !spec.IsScan || spec.Seq != 0 {
			t.Fatalf("scan 负载 index %d 应为扫描文本: %+v", i, spec)
		}
		if expectedReplyText(spec) != "" {
			t.Fatalf("scan 文本 %q 不应有期望回复", spec.Text)
		}
	}
}

func TestExpectedReplyText(t *testing.T) {
	if got := expectedReplyText(messageSpec{Seq: 7, Text: "bench 7"}); got != "[bench] 7" {
		t.Errorf("seq reply = %q", got)
	}
	if got := expectedReplyText(messageSpec{Text: "#ping"}); got != "[bench] pong" {
		t.Errorf("ping reply = %q", got)
	}
	if got := expectedReplyText(messageSpec{Text: "#help"}); !strings.HasPrefix(got, "[bench] 帮助") {
		t.Errorf("help reply = %q", got)
	}
	if got := expectedReplyText(messageSpec{Text: "聊天内容", IsScan: true}); got != "" {
		t.Errorf("scan reply = %q", got)
	}
}

func TestExtractSequence(t *testing.T) {
	if seq := extractSequence("[bench] 42"); seq != 42 {
		t.Fatalf("extractSequence = %d", seq)
	}
	if seq := extractSequence("[bench] pong"); seq != 0 {
		t.Fatalf("pong 不应配对: %d", seq)
	}
	if seq := extractSequence("[bench] abc"); seq != 0 {
		t.Fatalf("非数字文本不应配对: %d", seq)
	}
	if seq := extractSequence("其他文本"); seq != 0 {
		t.Fatalf("无前缀文本不应配对: %d", seq)
	}
}

// TestRespondAnswersActions 验证动作应答负载与回复配对。
func TestRespondAnswersActions(t *testing.T) {
	cfg := Config{SelfID: 2081344177, UserID: 10001, GroupID: 20001, Workload: WorkloadReply}
	session := newSession(cfg, nil, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))

	// 预置一条已推送的可配对事件。
	session.mu.Lock()
	session.events = append(session.events, EventRecord{Index: 0, Seq: 1, Text: "bench 1", SentNS: time.Now().UnixNano()})
	session.sentIndex[1] = 0
	session.sentAt[1] = clockNow()
	session.mu.Unlock()

	reply := incomingAction{
		Action: "send_msg",
		Params: mustJSON(t, map[string]any{
			"message_type": "private",
			"user_id":      cfg.UserID,
			"message": []map[string]any{{
				"type": "text",
				"data": map[string]any{"text": "[bench] 1"},
			}},
		}),
		Echo: "m1-1",
	}
	payload := session.respond(reply)
	if payload == nil {
		t.Fatal("send_msg 应有应答")
	}
	var response map[string]any
	if err := json.Unmarshal(payload, &response); err != nil {
		t.Fatalf("应答不是 JSON: %v", err)
	}
	if response["retcode"].(float64) != 0 || response["echo"] != "m1-1" {
		t.Fatalf("应答内容错误: %v", response)
	}
	if _, ok := response["data"].(map[string]any)["message_id"]; !ok {
		t.Fatalf("send_msg 应答缺少 message_id: %v", response)
	}

	records := session.eventRecords()
	if len(records) != 1 || records[0].LatencyMS <= 0 {
		t.Fatalf("未生成配对延迟样本: %+v", records)
	}
	if records[0].LatencyMS < 0 {
		t.Fatalf("延迟不能为负: %+v", records[0])
	}
}

// TestRespondCoversOldInitSequence 验证旧版 connect() 初始化动作的应答形状。
func TestRespondCoversOldInitSequence(t *testing.T) {
	cfg := Config{SelfID: 2081344177, UserID: 10001, GroupID: 20001, Workload: WorkloadReply}
	session := newSession(cfg, nil, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))

	actions := []struct {
		name   string
		action string
		check  func(*testing.T, map[string]any)
	}{
		{"_set_model_show", "_set_model_show", nil},
		{"get_login_info", "get_login_info", func(t *testing.T, r map[string]any) {
			data := r["data"].(map[string]any)
			if data["user_id"].(float64) != float64(cfg.SelfID) {
				t.Fatal("get_login_info 缺少 user_id")
			}
		}},
		{"get_guild_service_profile", "get_guild_service_profile", nil},
		{"get_online_clients", "get_online_clients", func(t *testing.T, r map[string]any) {
			// 旧版读 response.clients，缺失会得到 undefined。
			if _, ok := r["clients"].([]any); !ok {
				t.Fatal("get_online_clients 需要顶层 clients 数组")
			}
		}},
		{"get_version_info", "get_version_info", nil},
		{"get_cookies", "get_cookies", func(t *testing.T, r map[string]any) {
			// 空字符串使旧版跳过 18 个域的串行 cookie 请求。
			if cookies, ok := r["cookies"].(string); !ok || cookies != "" {
				t.Fatalf("get_cookies 需要顶层空 cookies，得到 %v", r["cookies"])
			}
		}},
		{"get_csrf_token", "get_csrf_token", func(t *testing.T, r map[string]any) {
			if _, ok := r["token"]; !ok {
				t.Fatal("get_csrf_token 需要顶层 token")
			}
		}},
		{"get_friend_list", "get_friend_list", nil},
		{"get_group_list", "get_group_list", func(t *testing.T, r map[string]any) {
			groups := r["data"].([]any)
			if len(groups) != 1 {
				t.Fatalf("get_group_list 应返回 1 个群，得到 %d", len(groups))
			}
		}},
		{"get_group_member_list", "get_group_member_list", nil},
		{"未知动作", "get_unknown_action", func(t *testing.T, r map[string]any) {
			if r["retcode"].(float64) != 0 {
				t.Fatal("未知动作也应成功应答")
			}
		}},
	}
	for _, item := range actions {
		payload := session.respond(incomingAction{Action: item.action, Params: json.RawMessage("{}"), Echo: "e-" + item.name})
		if payload == nil {
			t.Fatalf("%s 无应答", item.name)
		}
		var response map[string]any
		if err := json.Unmarshal(payload, &response); err != nil {
			t.Fatalf("%s 应答不是 JSON: %v", item.name, err)
		}
		if item.check != nil {
			item.check(t, response)
		}
	}
	if _, _, msgID := session.counters(); msgID != 0 {
		t.Fatalf("初始化动作不应消耗 message_id: %d", msgID)
	}
}

// TestRecordReplyPairingErrors 验证重复与未知序号的错误计数。
func TestRecordReplyPairingErrors(t *testing.T) {
	cfg := Config{SelfID: 1, UserID: 2, GroupID: 3, Workload: WorkloadReply}
	session := newSession(cfg, nil, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	session.mu.Lock()
	session.events = append(session.events, EventRecord{Index: 0, Seq: 5, Text: "bench 5", SentNS: time.Now().UnixNano()})
	session.sentIndex[5] = 0
	session.mu.Unlock()

	session.recordReply("[bench] 5", time.Now())
	session.recordReply("[bench] 5", time.Now())
	session.recordReply("[bench] 99", time.Now())
	_, errs, _ := session.counters()
	if errs != 2 {
		t.Fatalf("应记录 2 个配对错误，得到 %d", errs)
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("编码测试数据: %v", err)
	}
	return data
}

func TestBuildSummaryCounts(t *testing.T) {
	now := time.Now().UnixNano()
	events := []EventRecord{
		{Index: 0, Seq: 1, Text: "bench 1", SentNS: now, ReplyNS: now + 5_000_000, LatencyMS: 5},
		{Index: 1, Text: "#ping", SentNS: now},
		{Index: 2, Seq: 2, Text: "bench 2", SentNS: now},
		{Index: 3, Text: "聊天内容", SentNS: now},
	}
	summary := BuildSummary(events, nil, 1, 0, 1000, 4, 0, nil)
	if summary.Sent != 4 || summary.ExpectedReplies != 2 || summary.PairedReplies != 1 || summary.MissingReplies != 1 {
		t.Fatalf("计数错误: %+v", summary)
	}
	if summary.UnpairedReplies != 1 {
		t.Fatalf("unpaired = %d, want 1", summary.UnpairedReplies)
	}
	if summary.ExpectedTextReplies != 1 || summary.MissingTextReplies != 1 {
		t.Fatalf("text coverage = expected %d / missing %d, want 1/1", summary.ExpectedTextReplies, summary.MissingTextReplies)
	}
	if summary.LatencyP50MS != 5 || summary.LatencyMaxMS != 5 {
		t.Fatalf("延迟汇总错误: %+v", summary)
	}
	if summary.ThroughputEPS != 4 {
		t.Fatalf("吞吐 = %v, want 4", summary.ThroughputEPS)
	}
}

func TestBuildSummaryTextReplyCoverage(t *testing.T) {
	now := time.Now().UnixNano()
	events := []EventRecord{
		{Index: 0, Text: "#ping", SentNS: now},
		{Index: 1, Text: "#help", SentNS: now},
		{Index: 2, Text: "聊天内容", SentNS: now},
	}
	// 只收到 1 条固定文本回复：期望 2 条，保守缺失下界为 1。
	summary := BuildSummary(events, nil, 1, 0, 1000, 3, 1, nil)
	if summary.ExpectedTextReplies != 2 || summary.MissingTextReplies != 1 || summary.TextReplies != 1 {
		t.Fatalf("text coverage 错误: %+v", summary)
	}
	// 观察到的文本回复不少于期望数时，不产生负缺失。
	summary = BuildSummary(events, nil, 2, 0, 1000, 3, 3, nil)
	if summary.ExpectedTextReplies != 2 || summary.MissingTextReplies != 0 {
		t.Fatalf("text coverage 上限错误: %+v", summary)
	}
}

func TestWriteResultRejectsMissingSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "result.json")
	result := Result{
		ProtocolVersion: ProtocolVersion,
		Mode:            "server",
		Workload:        "reply",
		Kind:            "steady",
		StartedAt:       "2026-11-15T00:00:00Z",
		FinishedAt:      "2026-11-15T00:00:01Z",
		Summary:         Summary{Sent: 1},
	}
	if err := WriteResult(path, result); err == nil {
		t.Fatal("WriteResult accepted a result without schema_version")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("invalid result should not be written: %v", err)
	}
}

func TestLegacyResultJSONReads(t *testing.T) {
	// 2026-11-15 之前的结果没有 schema_version；读取必须继续成功。
	const legacy = `{
		"protocol_version": "1",
		"mode": "server",
		"workload": "reply",
		"kind": "steady",
		"started_at": "2026-11-15T00:00:00Z",
		"finished_at": "2026-11-15T00:00:01Z",
		"summary": {"sent": 1}
	}`
	var result Result
	if err := json.Unmarshal([]byte(legacy), &result); err != nil {
		t.Fatalf("read legacy result: %v", err)
	}
	if result.SchemaVersion != "" || result.ProtocolVersion != "1" || result.Summary.Sent != 1 {
		t.Fatalf("legacy result decode = %+v", result)
	}
}

func TestBuildSummaryKeepsZeroLatencyPairedReplies(t *testing.T) {
	now := time.Now().UnixNano()
	events := []EventRecord{
		{Index: 0, Seq: 1, Text: "bench 1", SentNS: now, ReplyNS: now},
		{Index: 1, Seq: 2, Text: "bench 2", SentNS: now, ReplyNS: now + 1_000, LatencyMS: 0.001},
		{Index: 2, Seq: 3, Text: "bench 3", SentNS: now},
	}
	summary := BuildSummary(events, nil, 2, 0, 1000, 3, 0, nil)
	if summary.PairedReplies != 2 || summary.MissingReplies != 1 {
		t.Fatalf("配对计数错误: %+v", summary)
	}
	if summary.LatencyCount != summary.PairedReplies {
		t.Fatalf("latency_count = %d, paired_replies = %d；零延迟样本被丢弃", summary.LatencyCount, summary.PairedReplies)
	}
	if summary.LatencyP50MS != 0.0005 || summary.LatencyMaxMS != 0.001 {
		t.Fatalf("零延迟样本未参与分位数: %+v", summary)
	}
}

func TestWriteResultAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "result.json")
	result := Result{
		SchemaVersion:   ResultSchemaVersion,
		ProtocolVersion: ProtocolVersion,
		Mode:            "server",
		Workload:        "reply",
		Kind:            "steady",
		StartedAt:       "2026-11-15T00:00:00Z",
		FinishedAt:      "2026-11-15T00:00:01Z",
		Summary:         Summary{Sent: 1},
	}
	if err := WriteResult(path, result); err != nil {
		t.Fatalf("WriteResult: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取结果: %v", err)
	}
	if !strings.Contains(string(data), `"protocol_version": "1"`) {
		t.Fatalf("结果内容异常: %s", data)
	}
	if !strings.Contains(string(data), `"schema_version": "1"`) {
		t.Fatalf("结果缺少 schema_version: %s", data)
	}
}

func TestResultSchemaFileIsValidJSON(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "schema", "result-v1.schema.json"))
	if err != nil {
		t.Fatalf("read result schema: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("result schema is not valid JSON: %v", err)
	}
	if doc["$schema"] == "" {
		t.Fatalf("result schema lacks $schema")
	}
	props, ok := doc["properties"].(map[string]any)
	if !ok {
		t.Fatalf("result schema lacks properties")
	}
	version, ok := props["schema_version"].(map[string]any)
	if !ok || version["const"] != ResultSchemaVersion {
		t.Fatalf("schema_version property = %#v, want const %q", props["schema_version"], ResultSchemaVersion)
	}
}

// TestBuildSummaryActualInjectionEPS 验证 actual_injection_eps 由事件首尾
// sent_ns 计算，并与 reported eps（分母含 grace）区分。
func TestBuildSummaryActualInjectionEPS(t *testing.T) {
	start := time.Now().UnixNano()
	const count = 500
	span := int64(time.Second) // 500 条事件均匀铺满 1s → actual 500/s
	events := make([]EventRecord, count)
	for i := range events {
		events[i] = EventRecord{Index: i, Seq: int64(i + 1), SentNS: start + span*int64(i)/(count-1)}
	}
	// load_wall_ms = 2000ms（负载窗口 + grace）→ reported eps = 250。
	summary := BuildSummary(events, nil, 0, 0, 2000, count, 0, nil)
	if math.Abs(summary.ActualInjectionEPS-500) > 0.01 {
		t.Fatalf("actual_injection_eps = %v, want 500", summary.ActualInjectionEPS)
	}
	if math.Abs(summary.ThroughputEPS-250) > 0.01 {
		t.Fatalf("throughput_eps = %v, want 250（分母为 load_wall_ms）", summary.ThroughputEPS)
	}
	if summary.ActualInjectionEPS == summary.ThroughputEPS {
		t.Fatal("actual 与 reported 口径不应相同")
	}
	// 空事件、单事件、时间跨度为 0 都无法定义速率，应为 0 而不是 NaN。
	if got := actualInjectionEPS(nil, 0); got != 0 {
		t.Fatalf("空事件 actual = %v, want 0", got)
	}
	if got := actualInjectionEPS([]EventRecord{{SentNS: start}}, 1); got != 0 {
		t.Fatalf("单事件 actual = %v, want 0", got)
	}
	if got := actualInjectionEPS([]EventRecord{{SentNS: start}, {SentNS: start}}, 2); got != 0 {
		t.Fatalf("时间跨度为 0 的 actual = %v, want 0", got)
	}
}

// TestGeneratorRenderWorkload 验证 render 负载每条消息都是 #render 且不携带序号。
func TestGeneratorRenderWorkload(t *testing.T) {
	gen := newGenerator(WorkloadRender)
	for index := 0; index < 5; index++ {
		spec := gen.next(index)
		if spec.Text != "#render" || spec.Seq != 0 {
			t.Fatalf("render 负载第 %d 条 = %+v, want #render 无序号", index, spec)
		}
	}
}

// TestRespondCountsTextReplies 验证非序号文本回复计数与样本采集；可配对序号
// 回复不计入。
func TestRespondCountsTextReplies(t *testing.T) {
	cfg := Config{SelfID: 2081344177, UserID: 10001, GroupID: 20001, Workload: WorkloadReply}
	session := newSession(cfg, nil, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))

	fallback := incomingAction{
		Action: "send_msg",
		Params: mustJSON(t, map[string]any{
			"message_type": "private",
			"user_id":      cfg.UserID,
			"message": []map[string]any{{
				"type": "text",
				"data": map[string]any{"text": "图片暂时不可用：browser unavailable"},
			}},
		}),
		Echo: "m2-1",
	}
	if session.respond(fallback) == nil {
		t.Fatal("send_msg 应有应答")
	}
	// 同文本重复回复只保留一个样本，但计数继续累加。
	if session.respond(fallback) == nil {
		t.Fatal("send_msg 应有应答")
	}
	paired := incomingAction{
		Action: "send_msg",
		Params: mustJSON(t, map[string]any{
			"message_type": "private",
			"user_id":      cfg.UserID,
			"message": []map[string]any{{
				"type": "text",
				"data": map[string]any{"text": "[bench] 7"},
			}},
		}),
		Echo: "m2-2",
	}
	if session.respond(paired) == nil {
		t.Fatal("send_msg 应有应答")
	}
	count, samples := session.textReplyStats()
	if count != 2 {
		t.Fatalf("text_replies = %d, want 2（配对序号回复不计入）", count)
	}
	if len(samples) != 1 || !strings.Contains(samples[0], "图片暂时不可用") {
		t.Fatalf("text samples = %v, want 含降级文本的单一去重样本", samples)
	}
}

func TestConfigValidate(t *testing.T) {
	base := Config{
		Mode: ModeServer, Listen: "127.0.0.1:3002", Workload: WorkloadReply, Kind: KindSteady,
		Rate: 10, Duration: time.Second, SelfID: 1, UserID: 2, GroupID: 3,
		OutPath: "out.json", SampleInterval: time.Second,
	}
	if err := base.validate(); err != nil {
		t.Fatalf("合法配置被拒绝: %v", err)
	}
	bad := base
	bad.Mode = "wrong"
	if err := bad.validate(); err == nil {
		t.Fatal("非法 mode 应被拒绝")
	}
	bad = base
	bad.Kind = KindBurst
	bad.Count = 0
	if err := bad.validate(); err == nil {
		t.Fatal("burst 缺少 count 应被拒绝")
	}
	bad = base
	bad.GroupID = 0
	if err := bad.validate(); err == nil {
		t.Fatal("group_id=0 应被拒绝")
	}
	bad = base
	bad.Mode = ModeClient
	bad.URL = ""
	if err := bad.validate(); err == nil {
		t.Fatal("client 模式缺少 url 应被拒绝")
	}
	good := base
	good.Workload = WorkloadRender
	if err := good.validate(); err != nil {
		t.Fatalf("合法 render 负载被拒绝: %v", err)
	}
	good = base
	good.MessageType = "group"
	good.GroupCount = 1
	good.UserCount = 1
	if err := good.validate(); err != nil {
		t.Fatalf("合法 group 配置被拒绝: %v", err)
	}
	bad = good
	bad.UserCount = 0
	if err := bad.validate(); err == nil {
		t.Fatal("group 模式 user-count=0 应被拒绝（session.push 会除零 panic）")
	}
	bad = good
	bad.GroupCount = 0
	if err := bad.validate(); err == nil {
		t.Fatal("group 模式 group-count=0 应被拒绝")
	}
}
