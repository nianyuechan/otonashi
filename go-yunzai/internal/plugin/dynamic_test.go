package plugin

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nianyuechan/otonashi/go-yunzai/internal/event"
	"github.com/nianyuechan/otonashi/go-yunzai/internal/message"
	"github.com/nianyuechan/otonashi/go-yunzai/pkg/plugin/sdk"
)

type callbackPlugin struct {
	fakePlugin
	start func(context.Context) error
	stop  func(context.Context) error
}

func (p *callbackPlugin) Start(ctx context.Context) error {
	if p.start != nil {
		return p.start(ctx)
	}
	return p.fakePlugin.Start(ctx)
}

func (p *callbackPlugin) Stop(ctx context.Context) error {
	if p.stop != nil {
		return p.stop(ctx)
	}
	return p.fakePlugin.Stop(ctx)
}

func TestDynamicLoadUsesPriorityAndManagerLifetime(t *testing.T) {
	manager := New(nil, nil)
	if err := manager.Load(context.Background(), &fakePlugin{}); !errors.Is(err, sdk.ErrNotRunning) {
		t.Fatalf("Load before Start = %v", err)
	}
	var calls []string
	makePlugin := func(id string, priority int) *fakePlugin {
		return &fakePlugin{descriptor: sdk.Descriptor{ID: id, Version: "1", Priority: priority}, rules: func(r sdk.Router) error {
			return r.Rule(sdk.Rule{ID: "message", Event: "message"}, func(context.Context, *sdk.Context) (sdk.Result, error) {
				calls = append(calls, id)
				return sdk.Stop, nil
			})
		}}
	}
	base := makePlugin("base", 10)
	if err := manager.Register(base); err != nil {
		t.Fatal(err)
	}
	startManager(t, manager)
	ctx, cancel := context.WithCancel(context.Background())
	loaded := makePlugin("loaded", 1)
	if err := manager.Load(ctx, loaded); err != nil {
		t.Fatal(err)
	}
	cancel() // Ending the load request must not terminate a successful plugin.
	if err := manager.HandleEvent(context.Background(), &event.Event{PostType: "message"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"loaded", "base"}) {
		t.Fatalf("dynamic priority/lifetime = %v", calls)
	}
	if err := manager.Load(context.Background(), makePlugin("loaded", 2)); err == nil {
		t.Fatal("duplicate dynamic id accepted")
	}
	if err := manager.Unload(context.Background(), "loaded"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Unload(context.Background(), "loaded"); err != nil {
		t.Fatal(err)
	}
	calls = nil
	if err := manager.HandleEvent(context.Background(), &event.Event{PostType: "message"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"base"}) || loaded.stopped != 1 || base.stopped != 0 {
		t.Fatalf("unload affected another plugin: calls=%v stopped=%d/%d", calls, loaded.stopped, base.stopped)
	}
}

func TestUnloadCancelsHandlerWorkerAndOldSessionBeforeReload(t *testing.T) {
	manager := New(nil, nil)
	startManager(t, manager)
	workerStarted, handlerStarted := make(chan struct{}), make(chan struct{})
	var workerExited, handlerExited atomic.Bool
	var stopped atomic.Int32
	var oldSession sdk.Session
	p := &callbackPlugin{fakePlugin: fakePlugin{descriptor: sdk.Descriptor{ID: "dynamic", Version: "1"}, rules: func(r sdk.Router) error {
		if err := r.Task(sdk.Task{ID: "worker"}, func(ctx context.Context, _ *sdk.TaskContext) error {
			close(workerStarted)
			<-ctx.Done()
			workerExited.Store(true)
			return nil
		}); err != nil {
			return err
		}
		return r.Rule(sdk.Rule{ID: "hold", Event: "message"}, func(ctx context.Context, c *sdk.Context) (sdk.Result, error) {
			oldSession = c.Session
			if err := c.Session.Set(time.Minute, func(context.Context, *sdk.Context) (sdk.Result, error) { panic("stale session") }); err != nil {
				return sdk.Stop, err
			}
			close(handlerStarted)
			<-ctx.Done()
			handlerExited.Store(true)
			return sdk.Stop, nil
		})
	}}, stop: func(context.Context) error {
		if !workerExited.Load() || !handlerExited.Load() {
			return errors.New("stop hook raced active work")
		}
		stopped.Add(1)
		return nil
	}}
	if err := manager.Load(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	awaitSignal(t, workerStarted)
	evt := &event.Event{PostType: "message", MessageType: "private", SelfID: 10, UserID: 20}
	finished := make(chan error, 1)
	go func() { finished <- manager.HandleEvent(context.Background(), evt) }()
	awaitSignal(t, handlerStarted)
	if err := manager.Unload(context.Background(), "dynamic"); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if stopped.Load() != 1 {
		t.Fatalf("stop hooks = %d", stopped.Load())
	}
	if err := oldSession.Set(time.Minute, func(context.Context, *sdk.Context) (sdk.Result, error) { return sdk.Stop, nil }); !errors.Is(err, sdk.ErrNotRunning) {
		t.Fatalf("old session still usable: %v", err)
	}
	var received bool
	replacement := &fakePlugin{descriptor: sdk.Descriptor{ID: "dynamic", Version: "2"}, rules: func(r sdk.Router) error {
		return r.Rule(sdk.Rule{ID: "message", Event: "message"}, func(context.Context, *sdk.Context) (sdk.Result, error) { received = true; return sdk.Stop, nil })
	}}
	if err := manager.Load(context.Background(), replacement); err != nil {
		t.Fatal(err)
	}
	if err := manager.HandleEvent(context.Background(), evt); err != nil {
		t.Fatal(err)
	}
	if !received {
		t.Fatal("replacement plugin did not receive the event")
	}
}

func TestUnloadTimeoutReservesIDUntilCleanupFinishes(t *testing.T) {
	manager := New(nil, nil)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }); _ = manager.Stop(context.Background()) })
	startManager(t, manager)
	p := &fakePlugin{descriptor: sdk.Descriptor{ID: "slow", Version: "1"}, rules: func(r sdk.Router) error {
		return r.Task(sdk.Task{ID: "slow"}, func(context.Context, *sdk.TaskContext) error { close(entered); <-release; return nil })
	}}
	if err := manager.Load(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	awaitSignal(t, entered)
	mgrRun := manager.plugins[0].run
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := manager.Unload(ctx, "slow"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Unload = %v", err)
	}
	replacement := &fakePlugin{descriptor: p.descriptor}
	if err := manager.Load(context.Background(), replacement); err == nil {
		t.Fatal("reloaded before old work exited")
	}
	// Other plugin lifecycle operations remain available during the drain.
	if err := manager.Load(context.Background(), &fakePlugin{descriptor: sdk.Descriptor{ID: "other", Version: "1"}}); err != nil {
		t.Fatal(err)
	}
	once.Do(func() { close(release) })
	awaitSignal(t, mgrRun.done)
	if err := manager.Load(context.Background(), replacement); err != nil {
		t.Fatal(err)
	}
	if p.stopped != 1 {
		t.Fatalf("old stop hooks = %d", p.stopped)
	}
}

func TestFailedDynamicLoadRollsBackWithoutLaunchingTasks(t *testing.T) {
	manager := New(nil, nil)
	startManager(t, manager)
	var calls atomic.Int32
	p := &fakePlugin{descriptor: sdk.Descriptor{ID: "bad", Version: "1"}, startErr: errors.New("start failed"), rules: func(r sdk.Router) error {
		return r.Task(sdk.Task{ID: "worker"}, func(context.Context, *sdk.TaskContext) error { calls.Add(1); return nil })
	}}
	if err := manager.Load(context.Background(), p); err == nil {
		t.Fatal("failing plugin loaded")
	}
	if calls.Load() != 0 || p.stopped != 0 {
		t.Fatal("failed Start launched tasks or invoked Stop on an unstarted plugin")
	}
	if err := manager.Load(context.Background(), &fakePlugin{descriptor: p.descriptor}); err != nil {
		t.Fatalf("failed load left the id reserved: %v", err)
	}
}

func TestLoadCanceledAfterSuccessfulStartRollsBack(t *testing.T) {
	manager := New(nil, nil)
	startManager(t, manager)
	ctx, cancel := context.WithCancel(context.Background())
	var stopped bool
	p := &callbackPlugin{fakePlugin: fakePlugin{descriptor: sdk.Descriptor{ID: "cancel", Version: "1"}}, start: func(context.Context) error { cancel(); return nil }, stop: func(context.Context) error { stopped = true; return nil }}
	if err := manager.Load(ctx, p); !errors.Is(err, context.Canceled) || !stopped {
		t.Fatalf("canceled startup did not roll back: stopped=%v err=%v", stopped, err)
	}
	if err := manager.Load(context.Background(), &fakePlugin{descriptor: p.descriptor}); err != nil {
		t.Fatal(err)
	}
}

func TestUnloadInvalidatesSnapshotsTakenByEarlierEvents(t *testing.T) {
	manager := New(nil, nil)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	var staleCalls atomic.Int32
	for _, p := range []*fakePlugin{
		{descriptor: sdk.Descriptor{ID: "first", Version: "1", Priority: 1}, rules: func(r sdk.Router) error {
			return r.Accept(func(context.Context, *sdk.Context) (sdk.AcceptDecision, error) {
				close(entered)
				<-release
				return sdk.AcceptNextPlugin, nil
			})
		}},
		{descriptor: sdk.Descriptor{ID: "stale", Version: "1", Priority: 2}, rules: func(r sdk.Router) error {
			return r.Rule(sdk.Rule{ID: "message", Event: "message"}, func(context.Context, *sdk.Context) (sdk.Result, error) { staleCalls.Add(1); return sdk.Stop, nil })
		}},
	} {
		if err := manager.Register(p); err != nil {
			t.Fatal(err)
		}
	}
	startManager(t, manager)
	finished := make(chan error, 1)
	go func() { finished <- manager.HandleEvent(context.Background(), &event.Event{PostType: "message"}) }()
	awaitSignal(t, entered)
	if err := manager.Unload(context.Background(), "stale"); err != nil {
		t.Fatal(err)
	}
	once.Do(func() { close(release) })
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if staleCalls.Load() != 0 {
		t.Fatal("an old event snapshot invoked the unloaded plugin")
	}
}

func TestConcurrentUnloadAndStopRunHookOnce(t *testing.T) {
	manager := New(nil, nil)
	startManager(t, manager)
	entered, release := make(chan struct{}), make(chan struct{})
	var stopped atomic.Int32
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }); _ = manager.Stop(context.Background()) })
	p := &callbackPlugin{fakePlugin: fakePlugin{descriptor: sdk.Descriptor{ID: "one", Version: "1"}}, stop: func(context.Context) error { stopped.Add(1); close(entered); <-release; return nil }}
	if err := manager.Load(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	unloaded := make(chan error, 1)
	go func() { unloaded <- manager.Unload(context.Background(), "one") }()
	awaitSignal(t, entered)
	finished := make(chan error, 1)
	go func() { finished <- manager.Stop(context.Background()) }()
	once.Do(func() { close(release) })
	if err := <-unloaded; err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if stopped.Load() != 1 {
		t.Fatalf("stop hooks = %d", stopped.Load())
	}
}

func TestReloadDoesNotInheritCooldown(t *testing.T) {
	manager := NewWithOptions(nil, nil, Options{Policy: sdk.Policy{Cooldown: time.Hour}})
	startManager(t, manager)
	var calls int
	makePlugin := func() *fakePlugin {
		return &fakePlugin{descriptor: sdk.Descriptor{ID: "cool", Version: "1"}, rules: func(r sdk.Router) error {
			return r.Rule(sdk.Rule{ID: "message", Event: "message"}, func(context.Context, *sdk.Context) (sdk.Result, error) { calls++; return sdk.Stop, nil })
		}}
	}
	for range 2 {
		if err := manager.Load(context.Background(), makePlugin()); err != nil {
			t.Fatal(err)
		}
		if err := manager.HandleEvent(context.Background(), &event.Event{PostType: "message", MessageType: "private", SelfID: 10, UserID: 20, Message: []message.Segment{message.Text("hello")}}); err != nil {
			t.Fatal(err)
		}
		if err := manager.Unload(context.Background(), "cool"); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("reload inherited a prior generation's cooldown: calls=%d", calls)
	}
}

func TestLoadingPluginIsInvisibleAndDoesNotBlockExistingEvents(t *testing.T) {
	manager := New(nil, nil)
	var baseCalls, loadedCalls atomic.Int32
	base := &fakePlugin{descriptor: sdk.Descriptor{ID: "base", Version: "1"}, rules: func(r sdk.Router) error {
		return r.Rule(sdk.Rule{ID: "message", Event: "message"}, func(context.Context, *sdk.Context) (sdk.Result, error) { baseCalls.Add(1); return sdk.Stop, nil })
	}}
	if err := manager.Register(base); err != nil {
		t.Fatal(err)
	}
	startManager(t, manager)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }); _ = manager.Stop(context.Background()) })
	p := &callbackPlugin{fakePlugin: fakePlugin{descriptor: sdk.Descriptor{ID: "loading", Version: "1"}, rules: func(r sdk.Router) error {
		return r.Rule(sdk.Rule{ID: "message", Event: "message"}, func(context.Context, *sdk.Context) (sdk.Result, error) { loadedCalls.Add(1); return sdk.Stop, nil })
	}}, start: func(context.Context) error { close(entered); <-release; return nil }}
	loaded := make(chan error, 1)
	go func() { loaded <- manager.Load(context.Background(), p) }()
	awaitSignal(t, entered)
	finished := make(chan error, 1)
	evt := &event.Event{PostType: "message"}
	go func() { finished <- manager.HandleEvent(context.Background(), evt) }()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("plugin startup blocked existing events")
	}
	if baseCalls.Load() != 1 || loadedCalls.Load() != 0 {
		t.Fatal("unready plugin was admitted")
	}
	once.Do(func() { close(release) })
	if err := <-loaded; err != nil {
		t.Fatal(err)
	}
	if err := manager.HandleEvent(context.Background(), evt); err != nil {
		t.Fatal(err)
	}
	if baseCalls.Load() != 2 || loadedCalls.Load() != 1 {
		t.Fatal("loaded plugin was not published after startup")
	}
}

type descriptorPanicPlugin struct{ fakePlugin }

func (*descriptorPanicPlugin) Descriptor() sdk.Descriptor { panic("private registration payload") }

func TestRegistrationPanicsDoNotPoisonRunningManager(t *testing.T) {
	manager := New(nil, nil)
	startManager(t, manager)
	for _, p := range []sdk.Plugin{
		&descriptorPanicPlugin{},
		&fakePlugin{descriptor: sdk.Descriptor{ID: "bad", Version: "1"}, rules: func(sdk.Router) error { panic("private registration payload") }},
	} {
		if err := manager.Load(context.Background(), p); err == nil {
			t.Fatal("registration panic was not returned as an error")
		}
	}
	if err := manager.Load(context.Background(), &fakePlugin{descriptor: sdk.Descriptor{ID: "bad", Version: "1"}}); err != nil {
		t.Fatalf("registration panic poisoned the manager: %v", err)
	}
}
