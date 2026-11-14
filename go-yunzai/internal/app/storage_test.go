package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func runNoteConversation(t *testing.T, path, messageType string, steps []noteStep, prepare func(*App)) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	conversation := make(chan error, 1)
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
		conversation <- nil
		_, _, _ = conn.Read(ctx)
	}))
	defer server.Close()
	cfg := config.Default()
	cfg.Storage.Path = path
	cfg.OneBot.URL = "ws" + strings.TrimPrefix(server.URL, "http")
	cfg.OneBot.AccessToken = "offline-test-token"
	application, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()
	if prepare != nil {
		prepare(application)
	}
	finished := make(chan error, 1)
	go func() { finished <- application.Run(ctx) }()
	select {
	case err := <-conversation:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("note conversation timed out")
	}
	if err := application.dispatcher.WaitContext(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("application did not stop")
	}
	if err := application.Close(); err != nil {
		t.Fatal(err)
	}
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
