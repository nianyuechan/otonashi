package plugin

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/nianyuechan/otonashi/go-yunzai/internal/event"
	"github.com/nianyuechan/otonashi/go-yunzai/internal/pluginconfig"
	"github.com/nianyuechan/otonashi/go-yunzai/internal/storage"
	"github.com/nianyuechan/otonashi/go-yunzai/pkg/plugin/sdk"
)

func TestStoreRevokedAfterUnloadAndDataRetainedOnReload(t *testing.T) {
	backend, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	manager := NewWithOptions(nil, nil, Options{Stores: backend})
	var handle sdk.KVStore
	makePlugin := func() *fakePlugin {
		return &fakePlugin{descriptor: sdk.Descriptor{ID: "persistent", Version: "1", Capabilities: []sdk.Capability{sdk.CapabilityStorage}}, rules: func(r sdk.Router) error {
			return r.Rule(sdk.Rule{ID: "capture", Event: "message"}, func(ctx context.Context, c *sdk.Context) (sdk.Result, error) {
				handle = c.Store
				return sdk.Stop, nil
			})
		}}
	}
	if err := manager.Register(makePlugin()); err != nil {
		t.Fatal(err)
	}
	startManager(t, manager)
	if err := manager.HandleEvent(context.Background(), &event.Event{PostType: "message"}); err != nil {
		t.Fatal(err)
	}
	if err := handle.Set(context.Background(), "state", []byte("persisted"), 0); err != nil {
		t.Fatal(err)
	}
	old := handle
	if err := manager.Unload(context.Background(), "persistent"); err != nil {
		t.Fatal(err)
	}
	if err := old.Set(context.Background(), "state", []byte("late"), 0); !errors.Is(err, sdk.ErrNotRunning) {
		t.Fatalf("old handle write = %v", err)
	}
	if err := manager.Load(context.Background(), makePlugin()); err != nil {
		t.Fatal(err)
	}
	if err := manager.HandleEvent(context.Background(), &event.Event{PostType: "message"}); err != nil {
		t.Fatal(err)
	}
	if got, err := handle.Get(context.Background(), "state"); err != nil || string(got) != "persisted" {
		t.Fatalf("reload state = %q, %v", got, err)
	}
	if _, err := old.Get(context.Background(), "state"); !errors.Is(err, sdk.ErrNotRunning) {
		t.Fatalf("reload reactivated old handle: %v", err)
	}
}

func TestConfigRevisionStableAcrossEventPhases(t *testing.T) {
	registry, err := pluginconfig.New(map[string]pluginconfig.Layers{"p": {Defaults: map[string]any{"x": "old"}}})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewWithOptions(nil, nil, Options{Config: registry})
	var views []sdk.ConfigView
	p := &fakePlugin{descriptor: sdk.Descriptor{ID: "p", Version: "1"}, rules: func(r sdk.Router) error {
		if err := r.Accept(func(ctx context.Context, c *sdk.Context) (sdk.AcceptDecision, error) {
			views = append(views, c.Config)
			if len(views) == 1 {
				if err := registry.Replace(map[string]pluginconfig.Layers{"p": {User: map[string]any{"x": "new"}}}); err != nil {
					return sdk.AcceptReturn, err
				}
			}
			return sdk.AcceptNextPlugin, nil
		}); err != nil {
			return err
		}
		return r.Rule(sdk.Rule{ID: "view", Event: "message"}, func(_ context.Context, c *sdk.Context) (sdk.Result, error) {
			views = append(views, c.Config)
			return sdk.Stop, nil
		})
	}}
	if err := manager.Register(p); err != nil {
		t.Fatal(err)
	}
	startManager(t, manager)
	for range 2 {
		if err := manager.HandleEvent(context.Background(), &event.Event{PostType: "message"}); err != nil {
			t.Fatal(err)
		}
	}
	for i, view := range views {
		want := `"old"`
		if i >= 2 {
			want = `"new"`
		}
		value, _ := view.Get("x")
		if string(value) != want || view.Revision() != views[i/2*2].Revision() {
			t.Fatalf("phase %d changed within event: %s revision %d", i, value, view.Revision())
		}
	}
	if len(views) != 4 || views[2].Revision() <= views[0].Revision() {
		t.Fatal("new event did not see new revision")
	}
}

func TestTasksReceiveScopedStoreAndFreshConfig(t *testing.T) {
	backend, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	registry, _ := pluginconfig.New(map[string]pluginconfig.Layers{"task": {User: map[string]any{"x": "old"}}})
	manager := NewWithOptions(nil, nil, Options{Stores: backend, Config: registry})
	seen := make(chan string, 32)
	p := &fakePlugin{descriptor: sdk.Descriptor{ID: "task", Version: "1", Capabilities: []sdk.Capability{sdk.CapabilityStorage}}, rules: func(r sdk.Router) error {
		return r.Task(sdk.Task{ID: "write", Every: time.Millisecond}, func(ctx context.Context, c *sdk.TaskContext) error {
			value, _ := c.Config.Get("x")
			if err := c.Store.Set(ctx, "value", value, 0); err != nil {
				return err
			}
			select {
			case seen <- string(value):
			case <-ctx.Done():
			}
			return nil
		})
	}}
	if err := manager.Register(p); err != nil {
		t.Fatal(err)
	}
	startManager(t, manager)
	// 任务每毫秒触发，正常观察即时完成；60s 宽限期与新的存储打开预算（60s）
	// 同口径，吸收实测存在的机器级 IO 停滞（~30s，多进程同时被拖慢），只影响
	// 真实失败时的报告速度。这里的 storage.Open 用 Background ctx，无预算竞争。
	deadline := time.NewTimer(60 * time.Second)
	defer deadline.Stop()
	select {
	case value := <-seen:
		if value != `"old"` {
			t.Fatal(value)
		}
	case <-deadline.C:
		t.Fatal("task did not run")
	}
	if err := registry.Replace(map[string]pluginconfig.Layers{"task": {User: map[string]any{"x": "new"}}}); err != nil {
		t.Fatal(err)
	}
	// 第二阶段重置计时器：共享的短计时器会在 Replace 之前把余量烧掉；实测本机
	// 存在 ~30s 的机器级 IO 停滞（多进程同时被拖慢），任务每毫秒触发、正常观察
	// 即时完成，60s 宽期限与第一阶段同口径，只影响真实失败时的报告速度。
	if !deadline.Reset(60 * time.Second) {
		// 停滞期间计时器已触发：排空残留信号再重新计时。
		select {
		case <-deadline.C:
		default:
		}
	}
	for {
		select {
		case value := <-seen:
			if value == `"new"` {
				if err := manager.Stop(context.Background()); err != nil {
					t.Fatal(err)
				}
				if got, err := backend.Scope("task").Get(context.Background(), "value"); err != nil || string(got) != `"new"` {
					t.Fatalf("task state = %q %v", got, err)
				}
				if _, err := backend.Scope("other").Get(context.Background(), "value"); !errors.Is(err, sdk.ErrKeyNotFound) {
					t.Fatal("task storage crossed namespaces", err)
				}
				return
			}
		case <-deadline.C:
			t.Fatal("task did not observe new config")
		}
	}
}

type cancelStore struct{ entered chan struct{} }

func (s cancelStore) Get(ctx context.Context, _ string) ([]byte, error) {
	close(s.entered)
	<-ctx.Done()
	return nil, ctx.Err()
}
func (s cancelStore) Set(ctx context.Context, _ string, _ []byte, _ time.Duration) error {
	_, err := s.Get(ctx, "")
	return err
}
func (s cancelStore) Delete(ctx context.Context, _ string) error {
	_, err := s.Get(ctx, "")
	return err
}

func TestRuntimeStoreCancelsOperationAndReportsAbsentBackend(t *testing.T) {
	lifetime, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := runtimeStore{lifetime: lifetime}
	if _, err := s.Get(context.Background(), "key"); !errors.Is(err, sdk.ErrStoreUnavailable) {
		t.Fatalf("absent backend = %v", err)
	}
	entered := make(chan struct{})
	s.backend = cancelStore{entered: entered}
	result := make(chan error, 1)
	go func() { _, err := s.Get(context.Background(), "key"); result <- err }()
	<-entered
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("plugin stop did not cancel backend operation")
	}
}
