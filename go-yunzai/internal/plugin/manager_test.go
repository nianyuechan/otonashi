package plugin

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/trss-yunzai/go-yunzai/internal/event"
	"github.com/trss-yunzai/go-yunzai/internal/message"
	"github.com/trss-yunzai/go-yunzai/pkg/plugin/sdk"
)

type fakeSender struct {
	outgoing message.Outgoing
}

func (s *fakeSender) SendMessage(_ context.Context, outgoing message.Outgoing) (message.Result, error) {
	s.outgoing = outgoing
	return message.Result{MessageID: 1}, nil
}

type fakePlugin struct {
	descriptor sdk.Descriptor
	rules      func(sdk.Router) error
	startErr   error
	stopErr    error
	started    int
	stopped    int
}

func (p *fakePlugin) Descriptor() sdk.Descriptor { return p.descriptor }

func (p *fakePlugin) Register(router sdk.Router) error {
	if p.rules == nil {
		return nil
	}
	return p.rules(router)
}

func (p *fakePlugin) Start(context.Context) error {
	p.started++
	return p.startErr
}

func (p *fakePlugin) Stop(context.Context) error {
	p.stopped++
	return p.stopErr
}

func startManager(t *testing.T, manager *Manager) {
	t.Helper()
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := manager.Stop(ctx); err != nil {
			t.Errorf("Stop() error = %v", err)
		}
	})
}

func TestManagerDispatchesByPriorityAndRuleResult(t *testing.T) {
	var calls []string
	manager := New(&fakeSender{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	low := &fakePlugin{
		descriptor: sdk.Descriptor{ID: "low", Version: "1.0.0", Priority: 10},
		rules: func(router sdk.Router) error {
			return router.Rule(sdk.Rule{ID: "low.hello", Event: "message.private", Pattern: `^hello$`}, func(context.Context, *sdk.Context) (sdk.Result, error) {
				calls = append(calls, "low")
				return sdk.Next, nil
			})
		},
	}
	high := &fakePlugin{
		descriptor: sdk.Descriptor{ID: "high", Version: "1.0.0", Priority: 20},
		rules: func(router sdk.Router) error {
			return router.Rule(sdk.Rule{ID: "high.message", Event: "message"}, func(context.Context, *sdk.Context) (sdk.Result, error) {
				calls = append(calls, "high")
				return sdk.Stop, nil
			})
		},
	}
	if err := manager.Register(high); err != nil {
		t.Fatal(err)
	}
	if err := manager.Register(low); err != nil {
		t.Fatal(err)
	}

	startManager(t, manager)
	err := manager.HandleEvent(context.Background(), &event.Event{
		PostType:    "message",
		MessageType: "private",
		Message:     []message.Segment{message.Text("hello")},
	})
	if err != nil {
		t.Fatalf("HandleEvent() error = %v", err)
	}
	if want := []string{"low", "high"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("handler order = %v, want %v", calls, want)
	}
}

func TestManagerPluginFailureDoesNotStopOtherPlugins(t *testing.T) {
	var calls []string
	manager := New(&fakeSender{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	failing := &fakePlugin{
		descriptor: sdk.Descriptor{ID: "failing", Version: "1.0.0"},
		rules: func(router sdk.Router) error {
			return router.Rule(sdk.Rule{ID: "failing.any", Event: "message"}, func(context.Context, *sdk.Context) (sdk.Result, error) {
				calls = append(calls, "failing")
				return sdk.Stop, errors.New("synthetic failure")
			})
		},
	}
	working := &fakePlugin{
		descriptor: sdk.Descriptor{ID: "working", Version: "1.0.0", Priority: 10},
		rules: func(router sdk.Router) error {
			return router.Rule(sdk.Rule{ID: "working.any", Event: "message"}, func(context.Context, *sdk.Context) (sdk.Result, error) {
				calls = append(calls, "working")
				return sdk.Stop, nil
			})
		},
	}
	if err := manager.Register(failing); err != nil {
		t.Fatal(err)
	}
	if err := manager.Register(working); err != nil {
		t.Fatal(err)
	}
	startManager(t, manager)
	if err := manager.HandleEvent(context.Background(), &event.Event{PostType: "message"}); err == nil {
		t.Fatal("HandleEvent() error = nil, want plugin error")
	}
	if want := []string{"failing", "working"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("handler calls = %v, want %v", calls, want)
	}
}

func TestManagerStartRollsBackOnFailure(t *testing.T) {
	manager := New(&fakeSender{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	first := &fakePlugin{descriptor: sdk.Descriptor{ID: "first", Version: "1.0.0"}}
	second := &fakePlugin{descriptor: sdk.Descriptor{ID: "second", Version: "1.0.0"}, startErr: errors.New("cannot start")}
	if err := manager.Register(first); err != nil {
		t.Fatal(err)
	}
	if err := manager.Register(second); err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background()); err == nil {
		t.Fatal("Start() error = nil, want failure")
	}
	if first.started != 1 || first.stopped != 1 {
		t.Fatalf("first lifecycle = started %d stopped %d, want 1/1", first.started, first.stopped)
	}
	if second.started != 1 || second.stopped != 0 {
		t.Fatalf("second lifecycle = started %d stopped %d, want 1/0", second.started, second.stopped)
	}
}

func TestManagerRejectsDuplicatePluginAndRule(t *testing.T) {
	manager := New(&fakeSender{}, nil)
	invalid := &fakePlugin{
		descriptor: sdk.Descriptor{ID: "same", Version: "1.0.0"},
		rules: func(router sdk.Router) error {
			if err := router.Rule(sdk.Rule{ID: "same.rule", Event: "message"}, func(context.Context, *sdk.Context) (sdk.Result, error) { return sdk.Stop, nil }); err != nil {
				return err
			}
			return router.Rule(sdk.Rule{ID: "same.rule", Event: "message"}, func(context.Context, *sdk.Context) (sdk.Result, error) { return sdk.Stop, nil })
		},
	}
	if err := manager.Register(invalid); err == nil {
		t.Fatal("Register() error = nil, want duplicate rule error")
	}
	valid := &fakePlugin{descriptor: sdk.Descriptor{ID: "same", Version: "1.0.0"}}
	if err := manager.Register(valid); err != nil {
		t.Fatalf("Register(valid) error = %v", err)
	}
	if err := manager.Register(&fakePlugin{descriptor: sdk.Descriptor{ID: "same", Version: "1.0.0"}}); err == nil {
		t.Fatal("Register() error = nil, want duplicate plugin error")
	}
}

func TestManagerExposesMessageSenderToPlugin(t *testing.T) {
	sender := &fakeSender{}
	manager := New(sender, slog.New(slog.NewTextHandler(io.Discard, nil)))
	plugin := &fakePlugin{
		descriptor: sdk.Descriptor{ID: "sender", Version: "1.0.0"},
		rules: func(router sdk.Router) error {
			return router.Rule(sdk.Rule{ID: "sender.message", Event: "message.private"}, func(ctx context.Context, pluginCtx *sdk.Context) (sdk.Result, error) {
				_, err := pluginCtx.Sender.SendMessage(ctx, sdk.Outgoing{
					MessageType: "private",
					UserID:      pluginCtx.Event.UserID,
					Message:     []sdk.Segment{{Type: "text", Data: map[string]any{"text": "sdk reply"}}},
				})
				return sdk.Stop, err
			})
		},
	}
	if err := manager.Register(plugin); err != nil {
		t.Fatal(err)
	}
	startManager(t, manager)
	if err := manager.HandleEvent(context.Background(), &event.Event{
		PostType:    "message",
		MessageType: "private",
		UserID:      1001,
		Message:     []message.Segment{message.Text("hello")},
	}); err != nil {
		t.Fatalf("HandleEvent() error = %v", err)
	}
	if sender.outgoing.UserID != 1001 || len(sender.outgoing.Message) != 1 || sender.outgoing.Message[0].Data["text"] != "sdk reply" {
		t.Fatalf("unexpected SDK outgoing message: %+v", sender.outgoing)
	}
}

func TestManagerPolicyRejectsUndeclaredCapability(t *testing.T) {
	manager := NewWithOptions(&fakeSender{}, nil, Options{
		Policy: sdk.Policy{AllowedCapabilities: []sdk.Capability{sdk.CapabilityMessages}},
	})
	plugin := &fakePlugin{descriptor: sdk.Descriptor{
		ID:           "render-plugin",
		Version:      "1.0.0",
		Capabilities: []sdk.Capability{sdk.CapabilityMessages, sdk.CapabilityRender},
	}}
	if err := manager.Register(plugin); err == nil {
		t.Fatal("Register() error = nil, want capability policy failure")
	}
}

func TestManagerPolicyFiltersSubjectsAndProvidesRuntime(t *testing.T) {
	var runtime sdk.RuntimeContext
	var calls int
	manager := NewWithOptions(&fakeSender{}, nil, Options{
		Policy: sdk.Policy{
			AllowUsers: []int64{1001},
			DenyUsers:  []int64{1002},
		},
	})
	plugin := &fakePlugin{
		descriptor: sdk.Descriptor{ID: "policy", Version: "1.0.0"},
		rules: func(router sdk.Router) error {
			return router.Rule(sdk.Rule{ID: "policy.message", Event: "message"}, func(_ context.Context, pluginCtx *sdk.Context) (sdk.Result, error) {
				calls++
				runtime = pluginCtx.Runtime
				return sdk.Next, nil
			})
		},
	}
	if err := manager.Register(plugin); err != nil {
		t.Fatal(err)
	}
	startManager(t, manager)
	for _, userID := range []int64{1001, 1002, 1003} {
		if err := manager.HandleEvent(context.Background(), &event.Event{PostType: "message", UserID: userID, MessageID: 9, SelfID: 2081344177}); err != nil {
			t.Fatalf("HandleEvent(%d) error = %v", userID, err)
		}
	}
	if calls != 1 {
		t.Fatalf("handler calls = %d, want 1", calls)
	}
	if runtime.AccountID != 2081344177 || runtime.UserID != 1001 || runtime.MessageID != 9 {
		t.Fatalf("runtime context = %+v", runtime)
	}
}

func TestManagerCooldownIsPerSubject(t *testing.T) {
	now := time.Unix(100, 0)
	var calls int
	manager := NewWithOptions(&fakeSender{}, nil, Options{
		Policy: sdk.Policy{Cooldown: time.Minute},
		Now:    func() time.Time { return now },
	})
	plugin := &fakePlugin{
		descriptor: sdk.Descriptor{ID: "cooldown", Version: "1.0.0"},
		rules: func(router sdk.Router) error {
			return router.Rule(sdk.Rule{ID: "cooldown.message", Event: "message"}, func(context.Context, *sdk.Context) (sdk.Result, error) {
				calls++
				return sdk.Next, nil
			})
		},
	}
	if err := manager.Register(plugin); err != nil {
		t.Fatal(err)
	}
	startManager(t, manager)
	eventFor := func(userID int64) *event.Event {
		return &event.Event{PostType: "message", UserID: userID}
	}
	if err := manager.HandleEvent(context.Background(), eventFor(1001)); err != nil {
		t.Fatal(err)
	}
	if err := manager.HandleEvent(context.Background(), eventFor(1001)); err != nil {
		t.Fatal(err)
	}
	if err := manager.HandleEvent(context.Background(), eventFor(1002)); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls before expiry = %d, want 2", calls)
	}
	now = now.Add(time.Minute)
	if err := manager.HandleEvent(context.Background(), eventFor(1001)); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("calls after expiry = %d, want 3", calls)
	}
}
