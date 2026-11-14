package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

const (
	testToken = "handshake-test-secret"
	testSelf  = int64(424242)
)

type recordedAction struct {
	Action string          `json:"action"`
	Params json.RawMessage `json:"params"`
	Echo   string          `json:"echo"`
}

type fakeOneBot struct {
	server   *httptest.Server
	actions  chan recordedAction
	authMode string
	mu       sync.Mutex
	conn     *websocket.Conn
}

func newFakeOneBot(t *testing.T) *fakeOneBot {
	t.Helper()
	return newFakeOneBotForAuth(t, "header")
}

func newFakeOneBotForAuth(t *testing.T, authMode string) *fakeOneBot {
	t.Helper()
	fake := &fakeOneBot{actions: make(chan recordedAction, 8), authMode: authMode}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorized := false
		switch fake.authMode {
		case "header":
			authorized = r.Header.Get("Authorization") == "Bearer "+testToken && r.URL.Query().Get("access_token") == ""
		case "query":
			authorized = r.Header.Get("Authorization") == "" && r.URL.Query().Get("access_token") == testToken
		}
		if !authorized {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("接受假 WS 连接: %v", err)
			return
		}
		fake.mu.Lock()
		fake.conn = conn
		fake.mu.Unlock()
		defer conn.CloseNow()
		for {
			_, data, err := conn.Read(context.Background())
			if err != nil {
				return
			}
			var action recordedAction
			if err := json.Unmarshal(data, &action); err != nil {
				t.Errorf("解码 action: %v", err)
				continue
			}
			fake.actions <- action
			response := map[string]any{"status": "ok", "retcode": 0, "echo": action.Echo}
			switch action.Action {
			case "get_login_info":
				response["data"] = map[string]any{"user_id": testSelf, "nickname": "tester", "sensitive": testToken}
			case "get_status":
				response["data"] = map[string]any{"online": true, "good": true}
			case "send_private_msg":
				response["data"] = map[string]any{"message_id": 1}
			default:
				response["status"] = "failed"
				response["retcode"] = 1404
			}
			payload, _ := json.Marshal(response)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			_ = conn.Write(ctx, websocket.MessageText, payload)
			cancel()
		}
	}))
	t.Cleanup(func() {
		fake.mu.Lock()
		if fake.conn != nil {
			fake.conn.CloseNow()
		}
		fake.mu.Unlock()
		fake.server.Close()
	})
	return fake
}

func (f *fakeOneBot) url() string {
	return "ws" + strings.TrimPrefix(f.server.URL, "http")
}

func TestRunMissingToken(t *testing.T) {
	code, output := execute(nil, "", "ws://127.0.0.1:0")
	if code != 2 || !strings.Contains(output, "SNOWLUMA_TOKEN") || !strings.Contains(output, "未设置") {
		t.Fatalf("code=%d output=%q", code, output)
	}
}

func TestRunReadOnlyAndRedactsToken(t *testing.T) {
	fake := newFakeOneBot(t)
	code, output := execute(nil, testToken, fake.url())
	actions := drainActions(fake)
	if code != 0 {
		t.Fatalf("code=%d output=%q", code, output)
	}
	if strings.Contains(output, testToken) || !strings.Contains(output, "Bearer ***") {
		t.Fatalf("token 未正确打码：%q", output)
	}
	if !strings.Contains(output, `"retcode":0`) || !strings.Contains(output, `"online":true`) {
		t.Fatalf("缺少原始 action JSON：%q", output)
	}
	if len(actions) != 2 || actions[0].Action != "get_login_info" || actions[1].Action != "get_status" {
		t.Fatalf("默认模式不是只读：%+v", actions)
	}
}

func TestRunRejectsNonSelfTarget(t *testing.T) {
	fake := newFakeOneBot(t)
	code, output := execute([]string{"-send-self-test", "-target-qq", "999"}, testToken, fake.url())
	actions := drainActions(fake)
	if code != 2 || !strings.Contains(output, "目标 QQ 与机器人自身不一致") {
		t.Fatalf("code=%d output=%q", code, output)
	}
	for _, action := range actions {
		if action.Action == "send_private_msg" {
			t.Fatal("非自身目标触发了私聊发送")
		}
	}
}

func TestRunSendSelfTest(t *testing.T) {
	fake := newFakeOneBot(t)
	code, output := execute([]string{"-send-self-test"}, testToken, fake.url())
	actions := drainActions(fake)
	if code != 0 {
		t.Fatalf("code=%d output=%q", code, output)
	}
	if len(actions) != 3 || actions[2].Action != "send_private_msg" {
		t.Fatalf("自身测试 action 不正确：%+v", actions)
	}
	var params struct {
		UserID int64 `json:"user_id"`
	}
	if err := json.Unmarshal(actions[2].Params, &params); err != nil || params.UserID != testSelf {
		t.Fatalf("自身测试目标不正确：%s", actions[2].Params)
	}
}

func TestRunRejectsGroupFlag(t *testing.T) {
	fake := newFakeOneBot(t)
	code, _ := execute([]string{"-send-self-test", "-group-id", "123"}, testToken, fake.url())
	actions := drainActions(fake)
	if code != 2 || len(actions) != 0 {
		t.Fatalf("群目标未被拒绝：code=%d actions=%+v", code, actions)
	}
}

func TestRunQueryAuth(t *testing.T) {
	fake := newFakeOneBotForAuth(t, "query")
	code, output := execute([]string{"-auth-mode", "query"}, testToken, fake.url())
	actions := drainActions(fake)
	if code != 0 {
		t.Fatalf("code=%d output=%q", code, output)
	}
	if strings.Contains(output, testToken) || !strings.Contains(output, "access_token=***") {
		t.Fatalf("query token 未正确打码：%q", output)
	}
	if len(actions) != 2 {
		t.Fatalf("query 鉴权未完成只读 action：%+v", actions)
	}
}

func TestRunRejectsInvalidAuthMode(t *testing.T) {
	fake := newFakeOneBot(t)
	code, output := execute([]string{"-auth-mode", "cookie"}, testToken, fake.url())
	if code != 2 || !strings.Contains(output, "auth-mode 只支持 header 或 query") {
		t.Fatalf("code=%d output=%q", code, output)
	}
	if actions := drainActions(fake); len(actions) != 0 {
		t.Fatalf("非法鉴权模式仍发起请求：%+v", actions)
	}
}

func TestRunUnauthorizedExplainsShared401(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()
	code, output := execute(nil, testToken, "ws"+strings.TrimPrefix(server.URL, "http"))
	for _, expected := range []string{"401", "缺少认证头、格式错误和 token 不匹配", "-auth-mode query", "Unauthorized"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("401 提示缺少 %q：%q", expected, output)
		}
	}
	if code != 1 {
		t.Fatalf("code=%d, want 1", code)
	}
}

func TestRedactingWriterMasksQueryEscapedToken(t *testing.T) {
	secret := "a b+c&="
	var output bytes.Buffer
	writer := &redactingWriter{writer: &output, secret: secret}
	_, _ = writer.Write([]byte("access_token=" + url.QueryEscape(secret)))
	if strings.Contains(output.String(), url.QueryEscape(secret)) || !strings.Contains(output.String(), "***") {
		t.Fatalf("URL 编码 token 未打码：%q", output.String())
	}
}

func execute(args []string, token, endpoint string) (int, string) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), args, func(string) string { return token }, &stdout, &stderr, endpoint)
	return code, stdout.String() + stderr.String()
}

func drainActions(fake *fakeOneBot) []recordedAction {
	var actions []recordedAction
	for len(fake.actions) > 0 {
		actions = append(actions, <-fake.actions)
	}
	return actions
}
