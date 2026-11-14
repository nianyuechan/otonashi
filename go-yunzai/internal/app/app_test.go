package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/trss-yunzai/go-yunzai/internal/config"
	"github.com/trss-yunzai/go-yunzai/pkg/plugin/sdk"
)

func TestNewWiresRenderClient(t *testing.T) {
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "plugins.sqlite")
	cfg.OneBot.AccessToken = "app-test-token"
	application, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer application.Close()
	if application.renderer == nil {
		t.Fatal("New() did not construct the render client")
	}
}

func TestNewAppliesConfiguredPluginCapabilities(t *testing.T) {
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "plugins.sqlite")
	cfg.OneBot.AccessToken = "offline-test-token"
	cfg.Plugins.AllowedCapabilities = []sdk.Capability{sdk.CapabilityMessages}
	cfg.Debug.Enabled = true
	if application, err := New(cfg); err == nil {
		application.Close()
		t.Fatal("application ignored configured capability policy")
	}
}

func TestNewOmitsDebugPluginWhenDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(t.TempDir(), "plugins.sqlite")
	cfg.OneBot.AccessToken = "offline-test-token"
	cfg.Plugins.AllowedCapabilities = []sdk.Capability{sdk.CapabilityMessages}
	cfg.Debug.Enabled = false
	application, err := New(cfg)
	if err != nil {
		t.Fatalf("New() rejected a config without the debug plugin: %v", err)
	}
	application.Close()
}

type workerPlugin struct {
	started chan struct{}
	exited  atomic.Bool
	stopped atomic.Bool
}

func (p *workerPlugin) Descriptor() sdk.Descriptor {
	return sdk.Descriptor{ID: "test.worker", Version: "1"}
}

func (p *workerPlugin) Register(router sdk.Router) error {
	return router.Task(sdk.Task{ID: "worker"}, func(ctx context.Context, _ *sdk.TaskContext) error {
		close(p.started)
		<-ctx.Done()
		p.exited.Store(true)
		return nil
	})
}

func (p *workerPlugin) Start(context.Context) error { return nil }

func (p *workerPlugin) Stop(context.Context) error {
	if !p.exited.Load() {
		return fmt.Errorf("plugin stopped before its worker exited")
	}
	p.stopped.Store(true)
	return nil
}

func TestSessionConversationAndShutdownThroughOneBot(t *testing.T) {
	for _, messageType := range []string{"private", "group"} {
		t.Run(messageType, func(t *testing.T) {
			// 与 runNoteConversation 相同的停滞容忍口径：实测本机存在 ~30s 的
			// 机器级 IO 停滞（多个并行测试包同时被拖慢），正常会话在数百毫秒内
			// 完成；30s 只吸收瞬态停滞，真实卡死仍会失败并留下会话记录。
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			conversation := make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				conn, err := websocket.Accept(w, req, nil)
				if err != nil {
					conversation <- err
					return
				}
				defer conn.CloseNow()
				inputs := []string{"#session", "answer", "ordinary"}
				outputs := []string{"[go-yunzai] 请在 30 秒内发送下一条消息", "[go-yunzai] 会话验证完成", config.DefaultDebugReply}
				for i, input := range inputs {
					incoming := map[string]any{
						"post_type": "message", "message_type": messageType, "self_id": 10,
						"user_id": 20, "message_id": i + 1,
						"message": []map[string]any{{"type": "text", "data": map[string]any{"text": input}}},
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
					if action.Action != "send_msg" || action.Params.MessageType != messageType || len(action.Params.Message) != 1 || action.Params.Message[0].Type != "text" || action.Params.Message[0].Data.Text != outputs[i] {
						conversation <- fmt.Errorf("unexpected reply %d: %s", i, data)
						return
					}
					if (messageType == "private" && action.Params.UserID != 20) || (messageType == "group" && action.Params.GroupID != 30) {
						conversation <- fmt.Errorf("incorrect reply target: %s", data)
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
			cfg.Storage.Path = filepath.Join(t.TempDir(), "plugins.sqlite")
			cfg.OneBot.URL = "ws" + strings.TrimPrefix(server.URL, "http")
			cfg.OneBot.AccessToken = "offline-test-token"
			cfg.Debug.Enabled = true
			cfg.Plugins.Masters = map[int64][]int64{10: {20}}
			application, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer application.Close()
			worker := &workerPlugin{started: make(chan struct{})}
			if err := application.plugins.Register(worker); err != nil {
				t.Fatal(err)
			}
			finished := make(chan error, 1)
			go func() { finished <- application.Run(ctx) }()
			t.Cleanup(func() { cancel() })
			select {
			case err := <-conversation:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("conversation did not finish")
			}
			select {
			case <-worker.started:
			case <-ctx.Done():
				t.Fatal("background worker did not start")
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
			if !worker.stopped.Load() {
				t.Fatal("plugin stop hook was skipped")
			}
		})
	}
}
