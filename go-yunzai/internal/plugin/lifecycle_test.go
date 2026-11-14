package plugin

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trss-yunzai/go-yunzai/internal/event"
	"github.com/trss-yunzai/go-yunzai/pkg/plugin/sdk"
)

func awaitSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for plugin work")
	}
}

func TestBackgroundTaskCancelsAndRestarts(t *testing.T) {
	started, canceled := make(chan struct{}, 2), make(chan struct{}, 2)
	manager := New(&fakeSender{}, nil)
	p := &fakePlugin{
		descriptor: sdk.Descriptor{ID: "worker", Version: "1"},
		rules: func(r sdk.Router) error {
			return r.Task(sdk.Task{ID: "background"}, func(ctx context.Context, c *sdk.TaskContext) error {
				if c.PluginID != "worker" || c.Sender == nil {
					return errors.New("missing task context")
				}
				started <- struct{}{}
				<-ctx.Done()
				canceled <- struct{}{}
				return ctx.Err()
			})
		},
	}
	if err := manager.Register(p); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := manager.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		awaitSignal(t, started)
		if err := manager.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := manager.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
		awaitSignal(t, canceled)
	}
	if p.started != 2 || p.stopped != 2 {
		t.Fatalf("lifecycle counts = %d/%d, want 2/2", p.started, p.stopped)
	}
	select {
	case <-started:
		t.Fatal("idempotent Start launched a duplicate worker")
	default:
	}
}

func TestPeriodicTaskSurvivesFailureAndPanicWithoutOverlap(t *testing.T) {
	manager := New(&fakeSender{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	var calls, active, overlap atomic.Int32
	completed := make(chan struct{})
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	p := &fakePlugin{
		descriptor: sdk.Descriptor{ID: "periodic", Version: "1"},
		rules: func(r sdk.Router) error {
			return r.Task(sdk.Task{ID: "poll", Every: time.Millisecond}, func(context.Context, *sdk.TaskContext) error {
				if active.Add(1) != 1 {
					overlap.Add(1)
				}
				defer active.Add(-1)
				switch calls.Add(1) {
				case 1:
					close(entered)
					<-release
					return errors.New("temporary failure")
				case 2:
					panic("temporary panic")
				case 3:
					close(completed)
				}
				return nil
			})
		},
	}
	if err := manager.Register(p); err != nil {
		t.Fatal(err)
	}
	startManager(t, manager)
	awaitSignal(t, entered)
	<-time.After(5 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatal("periodic task started another invocation while busy")
	}
	once.Do(func() { close(release) })
	awaitSignal(t, completed)
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if overlap.Load() != 0 || active.Load() != 0 {
		t.Fatalf("tasks overlapped or were not drained: overlap=%d active=%d", overlap.Load(), active.Load())
	}
}

func TestShutdownDeadlinePreventsOverlappingRestart(t *testing.T) {
	manager := New(&fakeSender{}, nil)
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }); _ = manager.Stop(context.Background()) })
	p := &fakePlugin{
		descriptor: sdk.Descriptor{ID: "slow", Version: "1"},
		rules: func(r sdk.Router) error {
			return r.Task(sdk.Task{ID: "slow"}, func(context.Context, *sdk.TaskContext) error {
				close(entered)
				<-release
				return nil
			})
		},
	}
	if err := manager.Register(p); err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	awaitSignal(t, entered)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := manager.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop = %v, want deadline exceeded", err)
	}
	if err := manager.Start(context.Background()); err == nil {
		t.Fatal("Start overlapped an unfinished worker")
	}
	if err := manager.Register(&fakePlugin{descriptor: sdk.Descriptor{ID: "new", Version: "1"}}); err == nil {
		t.Fatal("Register succeeded during shutdown")
	}
	releaseOnce.Do(func() { close(release) })
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p.stopped != 1 {
		t.Fatalf("stop hook called %d times", p.stopped)
	}
}

func TestStopCancelsHandlersAndDiscardsSessions(t *testing.T) {
	manager := New(&fakeSender{}, nil)
	entered := make(chan struct{})
	var old sdk.Session
	var ordinary atomic.Int32
	p := &fakePlugin{
		descriptor: sdk.Descriptor{ID: "conversation", Version: "1"},
		rules: func(r sdk.Router) error {
			return r.Rule(sdk.Rule{ID: "message", Event: "message"}, func(ctx context.Context, c *sdk.Context) (sdk.Result, error) {
				if ordinary.Add(1) != 1 {
					return sdk.Stop, nil
				}
				old = c.Session
				if err := old.Set(time.Hour, func(context.Context, *sdk.Context) (sdk.Result, error) {
					panic("a session from a previous run must never execute")
				}); err != nil {
					return sdk.Stop, err
				}
				close(entered)
				<-ctx.Done()
				return sdk.Stop, nil
			})
		},
	}
	if err := manager.Register(p); err != nil {
		t.Fatal(err)
	}
	startManager(t, manager)
	evt := &event.Event{PostType: "message", MessageType: "private", UserID: 20, SelfID: 10}
	handled := make(chan error, 1)
	go func() { handled <- manager.HandleEvent(context.Background(), evt) }()
	awaitSignal(t, entered)
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-handled; err != nil {
		t.Fatal(err)
	}
	if err := old.Set(time.Minute, func(context.Context, *sdk.Context) (sdk.Result, error) { return sdk.Stop, nil }); !errors.Is(err, sdk.ErrNotRunning) {
		t.Fatalf("old context remained usable: %v", err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := manager.HandleEvent(context.Background(), evt); err != nil {
		t.Fatal(err)
	}
	if ordinary.Load() != 2 {
		t.Fatal("a stale session intercepted the new run")
	}
}

func TestFailedStartDoesNotLaunchTasks(t *testing.T) {
	manager := New(&fakeSender{}, nil)
	var calls atomic.Int32
	first := &fakePlugin{descriptor: sdk.Descriptor{ID: "first", Version: "1"}, rules: func(r sdk.Router) error {
		return r.Task(sdk.Task{ID: "worker"}, func(context.Context, *sdk.TaskContext) error { calls.Add(1); return nil })
	}}
	second := &fakePlugin{descriptor: sdk.Descriptor{ID: "second", Version: "1"}, startErr: errors.New("start failed")}
	for _, p := range []*fakePlugin{first, second} {
		if err := manager.Register(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := manager.Start(context.Background()); err == nil {
		t.Fatal("Start accepted a failing plugin")
	}
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 || first.stopped != 1 || second.stopped != 0 {
		t.Fatalf("failed startup leaked work: calls=%d stopped=%d/%d", calls.Load(), first.stopped, second.stopped)
	}
}

func TestConcurrentStopRunsHooksOnce(t *testing.T) {
	manager := New(&fakeSender{}, nil)
	p := &fakePlugin{descriptor: sdk.Descriptor{ID: "one", Version: "1"}}
	if err := manager.Register(p); err != nil {
		t.Fatal(err)
	}
	startManager(t, manager)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := manager.Stop(context.Background()); err != nil {
				t.Errorf("Stop: %v", err)
			}
		}()
	}
	wg.Wait()
	if p.stopped != 1 {
		t.Fatalf("stop hook called %d times, want 1", p.stopped)
	}
}

func TestManagerRejectsInvalidTasksWithoutPartialRegistration(t *testing.T) {
	handler := func(context.Context, *sdk.TaskContext) error { return nil }
	for _, test := range []struct {
		name      string
		task      sdk.Task
		handler   sdk.TaskFunc
		duplicate bool
	}{
		{name: "blank id", task: sdk.Task{ID: " "}, handler: handler},
		{name: "negative interval", task: sdk.Task{ID: "task", Every: -time.Second}, handler: handler},
		{name: "nil handler", task: sdk.Task{ID: "task"}},
		{name: "duplicate id", task: sdk.Task{ID: "task"}, handler: handler, duplicate: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager := New(&fakeSender{}, nil)
			p := &fakePlugin{descriptor: sdk.Descriptor{ID: "worker", Version: "1"}, rules: func(r sdk.Router) error {
				if test.duplicate {
					if err := r.Task(test.task, test.handler); err != nil {
						return err
					}
				}
				return r.Task(test.task, test.handler)
			}}
			if err := manager.Register(p); err == nil {
				t.Fatal("Register accepted an invalid task")
			}
			if err := manager.Register(&fakePlugin{descriptor: p.descriptor}); err != nil {
				t.Fatalf("failed registration reserved the plugin id: %v", err)
			}
		})
	}
}

type panicLifecyclePlugin struct {
	fakePlugin
	panicOnStart bool
	panicOnStop  bool
}

func (p *panicLifecyclePlugin) Start(ctx context.Context) error {
	if p.panicOnStart {
		panic("private panic payload")
	}
	return p.fakePlugin.Start(ctx)
}

func (p *panicLifecyclePlugin) Stop(ctx context.Context) error {
	if p.panicOnStop {
		panic("private panic payload")
	}
	return p.fakePlugin.Stop(ctx)
}

func TestLifecyclePanicStillCleansUpOtherPlugins(t *testing.T) {
	for _, phase := range []string{"start", "stop"} {
		t.Run(phase, func(t *testing.T) {
			manager := New(&fakeSender{}, nil)
			first := &fakePlugin{descriptor: sdk.Descriptor{ID: "first", Version: "1"}}
			panicking := &panicLifecyclePlugin{
				fakePlugin:   fakePlugin{descriptor: sdk.Descriptor{ID: "panicking", Version: "1"}},
				panicOnStart: phase == "start", panicOnStop: phase == "stop",
			}
			for _, p := range []sdk.Plugin{first, panicking} {
				if err := manager.Register(p); err != nil {
					t.Fatal(err)
				}
			}
			err := manager.Start(context.Background())
			if phase == "stop" {
				if err != nil {
					t.Fatal(err)
				}
				err = manager.Stop(context.Background())
			}
			if err == nil || !strings.Contains(err.Error(), "panicked") || strings.Contains(err.Error(), "private panic payload") {
				t.Fatalf("expected an isolated panic without its payload, got %v", err)
			}
			if first.stopped != 1 {
				t.Fatalf("other plugin stop hooks = %d, want 1", first.stopped)
			}
		})
	}
}

func TestHandlerPanicDoesNotStopLaterPlugins(t *testing.T) {
	manager := New(&fakeSender{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	var handled bool
	for _, p := range []*fakePlugin{
		{descriptor: sdk.Descriptor{ID: "panic", Version: "1"}, rules: func(r sdk.Router) error {
			return r.Rule(sdk.Rule{ID: "panic", Event: "message"}, func(context.Context, *sdk.Context) (sdk.Result, error) {
				panic("private panic payload")
			})
		}},
		{descriptor: sdk.Descriptor{ID: "later", Version: "1"}, rules: func(r sdk.Router) error {
			return r.Rule(sdk.Rule{ID: "later", Event: "message"}, func(context.Context, *sdk.Context) (sdk.Result, error) {
				handled = true
				return sdk.Stop, nil
			})
		}},
	} {
		if err := manager.Register(p); err != nil {
			t.Fatal(err)
		}
	}
	startManager(t, manager)
	err := manager.HandleEvent(context.Background(), &event.Event{PostType: "message"})
	if !handled || err == nil || strings.Contains(err.Error(), "private panic payload") {
		t.Fatalf("handler panic was not safely isolated: handled=%v err=%v", handled, err)
	}
}
