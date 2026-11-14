package bench

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/trss-yunzai/go-yunzai/pkg/plugin/sdk"
)

type stubStore struct {
	data   map[string][]byte
	getErr error
	setErr error
	gets   int
	sets   int
}

func (s *stubStore) Get(ctx context.Context, key string) ([]byte, error) {
	s.gets++
	if s.getErr != nil {
		return nil, s.getErr
	}
	value, ok := s.data[key]
	if !ok {
		return nil, sdk.ErrKeyNotFound
	}
	return append([]byte{}, value...), nil
}

func (s *stubStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	s.sets++
	if s.setErr != nil {
		return s.setErr
	}
	s.data[key] = append([]byte{}, value...)
	return nil
}

func (s *stubStore) Delete(ctx context.Context, key string) error {
	delete(s.data, key)
	return nil
}

func storeEvent(messageType string, groupID, userID int64, text string) *sdk.Context {
	return &sdk.Context{
		Event: &sdk.Event{
			MessageType: messageType,
			GroupID:     groupID,
			UserID:      userID,
			Message:     []sdk.Segment{{Type: "text", Data: map[string]any{"text": text}}},
		},
		Runtime: sdk.RuntimeContext{
			AccountID:   2081344177,
			UserID:      int64(userID),
			GroupID:     int64(groupID),
			MessageType: messageType,
		},
		Store: &stubStore{data: map[string][]byte{}},
	}
}

func storeHandler(t *testing.T) (sdk.HandlerFunc, *StorePlugin) {
	t.Helper()
	plugin := &StorePlugin{}
	router := &fakeRouter{}
	if err := plugin.Register(router); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	var handler sdk.HandlerFunc
	for _, rule := range router.rules {
		if rule.rule.ID == "bench.store.write" {
			handler = rule.handler
		}
	}
	if handler == nil {
		t.Fatal("bench.store.write rule not registered")
	}
	return handler, plugin
}

func TestStorePluginWritesPerUserSession(t *testing.T) {
	handler, _ := storeHandler(t)
	// 同一用户两次写入应命中同一键并覆盖；另一用户使用不同键。
	store := &stubStore{data: map[string][]byte{}}
	first := storeEvent("group", 20001, 10001, "bench 1")
	first.Store = store
	if _, err := handler(context.Background(), first); err != nil {
		t.Fatalf("first write failed: %v", err)
	}
	second := storeEvent("group", 20001, 10001, "bench 2")
	second.Store = store
	if _, err := handler(context.Background(), second); err != nil {
		t.Fatalf("second write failed: %v", err)
	}
	other := storeEvent("group", 20002, 10002, "bench 3")
	other.Store = store
	if _, err := handler(context.Background(), other); err != nil {
		t.Fatalf("other user write failed: %v", err)
	}
	if store.sets != 3 || store.gets != 3 {
		t.Fatalf("sets=%d gets=%d, want 3/3", store.sets, store.gets)
	}
	keys := make([]string, 0, len(store.data))
	for key := range store.data {
		keys = append(keys, key)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 isolated keys (per group+user), got %d: %v", len(keys), keys)
	}
	value := string(store.data["bench:a:2081344177:t:group:g:20001:u:10001"])
	if value != "bench 2" {
		t.Fatalf("same-user value = %q, want overwritten \"bench 2\"", value)
	}
	value = string(store.data["bench:a:2081344177:t:group:g:20002:u:10002"])
	if value != "bench 3" {
		t.Fatalf("other-user value = %q, want \"bench 3\"", value)
	}
}

func TestStorePluginRejectsMissingStore(t *testing.T) {
	handler, _ := storeHandler(t)
	ctx := storeEvent("private", 0, 10001, "bench 4")
	ctx.Store = nil
	_, err := handler(context.Background(), ctx)
	if err == nil || !strings.Contains(err.Error(), "store not injected") {
		t.Fatalf("expected store-not-injected error, got %v", err)
	}
}

func TestStorePluginSurfacesStoreErrors(t *testing.T) {
	handler, _ := storeHandler(t)
	ctx := storeEvent("private", 0, 10001, "bench 5")
	ctx.Store.(*stubStore).setErr = sdk.ErrStoreUnavailable
	if _, err := handler(context.Background(), ctx); !errors.Is(err, sdk.ErrStoreUnavailable) {
		t.Fatalf("expected ErrStoreUnavailable, got %v", err)
	}

	handler, _ = storeHandler(t)
	ctx = storeEvent("private", 0, 10001, "bench 6")
	ctx.Store.(*stubStore).getErr = sdk.ErrStoreUnavailable
	if _, err := handler(context.Background(), ctx); !errors.Is(err, sdk.ErrStoreUnavailable) {
		t.Fatalf("expected ErrStoreUnavailable from get, got %v", err)
	}
}

// TestStorePluginGroupKeyFallsBackToSender 验证群消息缺少顶层 user_id 时键按
// sender.user_id 隔离（OneBot v11 群事件 user_id 可选，负载器 group 事件不带）。
func TestStorePluginGroupKeyFallsBackToSender(t *testing.T) {
	handler, _ := storeHandler(t)
	ctx := &sdk.Context{
		Event: &sdk.Event{
			MessageType: "group",
			GroupID:     20001,
			Message:     []sdk.Segment{{Type: "text", Data: map[string]any{"text": "bench 1"}}},
			Sender:      sdk.SenderInfo{UserID: 10001},
		},
		Runtime: sdk.RuntimeContext{
			AccountID:   2081344177,
			MessageType: "group",
			GroupID:     20001,
		},
		Store: &stubStore{data: map[string][]byte{}},
	}
	if _, err := handler(context.Background(), ctx); err != nil {
		t.Fatalf("group write failed: %v", err)
	}
	store := ctx.Store.(*stubStore)
	if _, ok := store.data["bench:a:2081344177:t:group:g:20001:u:10001"]; !ok {
		keys := make([]string, 0, len(store.data))
		for key := range store.data {
			keys = append(keys, key)
		}
		t.Fatalf("sender-isolated key missing, got %v", keys)
	}
}
