package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/coder/websocket"
	"github.com/trss-yunzai/go-yunzai/internal/config"
	"github.com/trss-yunzai/go-yunzai/internal/pluginconfig"
	"github.com/trss-yunzai/go-yunzai/internal/storage"
)

type noteStep struct {
	input, output string
	account, user int64
}

func TestNewWiresConfiguredRedisBackend(t *testing.T) {
	server := miniredis.RunT(t)
	cfg := config.Default()
	cfg.OneBot.AccessToken = "offline-test-token"
	cfg.Storage = config.Storage{
		Backend: "redis",
		Redis:   config.RedisStorage{Addr: server.Addr(), MaxRetries: 0, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second},
	}
	application, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()
	if _, ok := application.store.(*storage.Redis); !ok {
		t.Fatalf("configured storage type = %T, want *storage.Redis", application.store)
	}
}

// runNoteConversation drives one #note conversation against a fresh
// application over a fake OneBot WebSocket, restarting the same SQLite file
// when the caller reuses path. Any exit path must stop the application in a
// fixed order (cancel → wait Run → Close → server.Close) before returning, so
// t.TempDir cleanup never races a live SQLite handle. The overall deadline is
// deliberately generous: one observed full-suite failure stalled a single
// store operation for ≥8s (system IO hiccup, not lock contention —
// busy_timeout caps lock waits at 2.5s and errors instead of hanging). The
// deadline absorbs transient stalls, and step evidence plus a goroutine dump
// make any real hang diagnosable instead of just reporting "timed out".
func runNoteConversation(t *testing.T, path, messageType string, steps []noteStep, prepare func(*App)) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conversation := make(chan error, 1)
	var traceMu sync.Mutex
	traceStart := time.Now()
	var trace []string
	record := func(format string, args ...any) {
		traceMu.Lock()
		defer traceMu.Unlock()
		trace = append(trace, fmt.Sprintf("+%dms %s", time.Since(traceStart).Milliseconds(), fmt.Sprintf(format, args...)))
	}
	dump := func(why string) {
		traceMu.Lock()
		traceCopy := strings.Join(trace, "\n")
		traceMu.Unlock()
		state := make([]byte, 1<<16)
		state = state[:runtime.Stack(state, true)]
		t.Logf("%s\n步骤记录:\n%s\n当前 goroutine:\n%s", why, traceCopy, state)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		conn, err := websocket.Accept(w, req, nil)
		if err != nil {
			conversation <- err
			return
		}
		defer conn.CloseNow()
		for i, step := range steps {
			incoming := map[string]any{
				"post_type": "message", "message_type": messageType,
				"self_id": step.account, "user_id": step.user, "message_id": i + 1,
				"message": []map[string]any{{"type": "text", "data": map[string]any{"text": step.input}}},
			}
			if messageType == "group" {
				incoming["group_id"] = 30
			}
			record("step %d: 推送 %q", i, step.input)
			data, _ := json.Marshal(incoming)
			if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
				conversation <- err
				return
			}
			_, data, err = conn.Read(ctx)
			if err != nil {
				conversation <- err
				return
			}
			record("step %d: 收到回复动作", i)
			var action struct {
				Action string `json:"action"`
				Echo   string `json:"echo"`
				Params struct {
					MessageType string `json:"message_type"`
					UserID      int64  `json:"user_id"`
					GroupID     int64  `json:"group_id"`
					Message     []struct {
						Type string `json:"type"`
						Data struct {
							Text string `json:"text"`
						} `json:"data"`
					} `json:"message"`
				} `json:"params"`
			}
			if err := json.Unmarshal(data, &action); err != nil {
				conversation <- err
				return
			}
			if action.Action != "send_msg" || action.Params.MessageType != messageType || len(action.Params.Message) != 1 || action.Params.Message[0].Type != "text" || action.Params.Message[0].Data.Text != step.output {
				conversation <- fmt.Errorf("note step %d unexpected reply: %s", i, data)
				return
			}
			if (messageType == "private" && action.Params.UserID != step.user) || (messageType == "group" && action.Params.GroupID != 30) {
				conversation <- fmt.Errorf("note reply addressed to wrong subject")
				return
			}
			response, _ := json.Marshal(map[string]any{"status": "ok", "retcode": 0, "echo": action.Echo, "data": map[string]any{"message_id": 100 + i}})
			if err := conn.Write(ctx, websocket.MessageText, response); err != nil {
				conversation <- err
				return
			}
		}
		record("全部 %d 步完成", len(steps))
		conversation <- nil
		_, _, _ = conn.Read(ctx)
	}))
	cfg := config.Default()
	cfg.Storage.Path = path
	cfg.OneBot.URL = "ws" + strings.TrimPrefix(server.URL, "http")
	cfg.OneBot.AccessToken = "offline-test-token"
	cfg.Debug.Enabled = true
	application, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	// t.Cleanup 在调用方 t.TempDir 的清理之前运行（LIFO），保证任何失败路径都
	// 先有序停止应用、释放 SQLite 句柄，再轮到临时目录删除。正常路径已在函数
	// 体内完成同样的停止序列，这里通过 stopped 跳过重复等待。
	stopped := false
	t.Cleanup(func() {
		cancel()
		if !stopped {
			select {
			case err := <-finished:
				if err != nil {
					t.Errorf("application run error during cleanup: %v", err)
				}
			case <-time.After(5 * time.Second):
				dump("清理期间应用 5 秒内未停止")
			}
			if err := application.Close(); err != nil {
				t.Errorf("application close during cleanup: %v", err)
			}
		}
		server.Close()
	})
	if prepare != nil {
		prepare(application)
	}
	go func() { finished <- application.Run(ctx) }()
	select {
	case err := <-conversation:
		if err != nil {
			dump("note conversation failed")
			t.Fatal(err)
		}
	case <-ctx.Done():
		dump("note conversation timed out")
		t.Fatal("note conversation timed out")
	}
	if err := application.dispatcher.WaitContext(ctx); err != nil {
		dump("dispatcher drain timed out")
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		dump("application did not stop")
		t.Fatal("application did not stop")
	}
	if err := application.Close(); err != nil {
		t.Fatal(err)
	}
	stopped = true
}

func TestNoteSurvivesApplicationRestartThroughOneBot(t *testing.T) {
	for _, kind := range []string{"private", "group"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "note.sqlite")
			runNoteConversation(t, path, kind, []noteStep{
				{"#note", "还没有记事", 10, 20},
				{"#note set restart-proof", "记事已保存，重启后仍可读取", 10, 20},
				{"#note", "还没有记事", 10, 21},
				{"#note", "还没有记事", 11, 20},
			}, nil)
			runNoteConversation(t, path, kind, []noteStep{
				{"#note", "restart-proof", 10, 20},
				{"#note set too-long", "记事内容须为 1 至 4 字节", 10, 20},
				{"#note", "restart-proof", 10, 20},
				{"#note delete", "记事已删除", 10, 20},
				{"#note", "还没有记事", 10, 20},
			}, func(a *App) {
				if err := a.settings.Replace(map[string]pluginconfig.Layers{"builtin.debug": {User: map[string]any{"note_max_bytes": 4}}}); err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}

func TestNoteStorageFailureRepliesUnavailable(t *testing.T) {
	runNoteConversation(t, filepath.Join(t.TempDir(), "note.sqlite"), "private", []noteStep{
		{"#note", "记事存储暂时不可用，请稍后重试", 10, 20},
		{"#note set value", "记事存储暂时不可用，请稍后重试", 10, 20},
	}, func(a *App) {
		if err := a.store.Close(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestReloadPluginSettingsAndFailedReloadRetainsView(t *testing.T) {
	cfg := config.Default()
	cfg.OneBot.AccessToken = "offline-test-token"
	cfg.Storage.Path = filepath.Join(t.TempDir(), "note.sqlite")
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	path := filepath.Join(t.TempDir(), "settings.yaml")
	valid := "onebot:\n  access_token: offline-test-token\nplugins:\n  settings:\n    builtin.debug:\n      user:\n        note_max_bytes: 8\n"
	if err := os.WriteFile(path, []byte(valid), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.ReloadPluginSettings(path); err != nil {
		t.Fatal(err)
	}
	view := a.settings.Snapshot().View("builtin.debug", 0, 0)
	if value, _ := view.Get("note_max_bytes"); string(value) != "8" {
		t.Fatalf("reload value = %s", value)
	}
	if _, ok := view.Get("access_token"); ok {
		t.Fatal("core credentials exposed as plugin settings")
	}
	if err := os.WriteFile(path, []byte("unknown_core_field: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.ReloadPluginSettings(path); err == nil {
		t.Fatal("invalid file accepted")
	}
	if a.settings.Snapshot().View("builtin.debug", 0, 0).Revision() != view.Revision() {
		t.Fatal("failed reload replaced working configuration")
	}
}
