package plugin

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nianyuechan/otonashi/go-yunzai/internal/event"
	"github.com/nianyuechan/otonashi/go-yunzai/pkg/plugin/sdk"
)

func TestAcceptDecisionsRunBeforeRulesInPriorityOrder(t *testing.T) {
	for _, test := range []struct {
		name     string
		decision sdk.AcceptDecision
		want     []string
	}{
		{"next", sdk.AcceptNextPlugin, []string{"accept:first", "accept:second", "rule:first", "rule:second"}},
		{"break", sdk.AcceptBreak, []string{"accept:first", "rule:first", "rule:second"}},
		{"return", sdk.AcceptReturn, []string{"accept:first"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager := New(nil, nil)
			var calls []string
			for i, id := range []string{"first", "second"} {
				p := &fakePlugin{descriptor: sdk.Descriptor{ID: id, Version: "1", Priority: i}, rules: func(r sdk.Router) error {
					if err := r.Accept(func(context.Context, *sdk.Context) (sdk.AcceptDecision, error) {
						calls = append(calls, "accept:"+id)
						if id == "first" {
							return test.decision, nil
						}
						return sdk.AcceptNextPlugin, nil
					}); err != nil {
						return err
					}
					return r.Rule(sdk.Rule{ID: "rule", Event: "message"}, func(context.Context, *sdk.Context) (sdk.Result, error) {
						calls = append(calls, "rule:"+id)
						return sdk.Stop, nil
					})
				}}
				if err := manager.Register(p); err != nil {
					t.Fatal(err)
				}
			}
			startManager(t, manager)
			if err := manager.HandleEvent(context.Background(), &event.Event{PostType: "message"}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(calls, test.want) {
				t.Fatalf("calls = %v, want %v", calls, test.want)
			}
		})
	}
}

func TestAcceptFailureIsIsolated(t *testing.T) {
	for _, mode := range []string{"error", "panic", "invalid decision"} {
		t.Run(mode, func(t *testing.T) {
			manager := New(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
			var later bool
			for _, p := range []*fakePlugin{
				{descriptor: sdk.Descriptor{ID: "bad", Version: "1"}, rules: func(r sdk.Router) error {
					return r.Accept(func(context.Context, *sdk.Context) (sdk.AcceptDecision, error) {
						if mode == "panic" {
							panic("private payload")
						}
						if mode == "error" {
							return sdk.AcceptReturn, errors.New("synthetic error")
						}
						return sdk.AcceptDecision(99), nil
					})
				}},
				{descriptor: sdk.Descriptor{ID: "later", Version: "1"}, rules: func(r sdk.Router) error {
					return r.Rule(sdk.Rule{ID: "rule", Event: "message"}, func(context.Context, *sdk.Context) (sdk.Result, error) { later = true; return sdk.Stop, nil })
				}},
			} {
				if err := manager.Register(p); err != nil {
					t.Fatal(err)
				}
			}
			startManager(t, manager)
			err := manager.HandleEvent(context.Background(), &event.Event{PostType: "message"})
			if !later || err == nil || strings.Contains(err.Error(), "private payload") {
				t.Fatalf("failure isolation: later=%v err=%v", later, err)
			}
		})
	}
}

func TestAcceptRegistrationAndDescriptorFilter(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		manager := New(nil, nil)
		p := &fakePlugin{descriptor: sdk.Descriptor{ID: "bad", Version: "1"}, rules: func(r sdk.Router) error {
			if !duplicate {
				return r.Accept(nil)
			}
			fn := func(context.Context, *sdk.Context) (sdk.AcceptDecision, error) { return sdk.AcceptNextPlugin, nil }
			if err := r.Accept(fn); err != nil {
				return err
			}
			return r.Accept(fn)
		}}
		if err := manager.Register(p); err == nil {
			t.Fatal("invalid accept registration succeeded")
		}
	}
	manager := New(nil, nil)
	p := &fakePlugin{descriptor: sdk.Descriptor{ID: "private", Version: "1", Events: []string{"message.private"}}, rules: func(r sdk.Router) error {
		return r.Accept(func(context.Context, *sdk.Context) (sdk.AcceptDecision, error) {
			t.Error("accept received another event type")
			return sdk.AcceptReturn, nil
		})
	}}
	if err := manager.Register(p); err != nil {
		t.Fatal(err)
	}
	startManager(t, manager)
	if err := manager.HandleEvent(context.Background(), &event.Event{PostType: "message", MessageType: "group"}); err != nil {
		t.Fatal(err)
	}
}

func TestSessionsRunBeforeAcceptsAndSkipOnlyTheirOwnPlugin(t *testing.T) {
	manager := New(nil, nil)
	var calls []string
	var saved sdk.Session
	p := &fakePlugin{descriptor: sdk.Descriptor{ID: "session", Version: "1"}, rules: func(r sdk.Router) error {
		if err := r.Accept(func(_ context.Context, c *sdk.Context) (sdk.AcceptDecision, error) {
			saved = c.Session
			calls = append(calls, "accept:session")
			return sdk.AcceptNextPlugin, nil
		}); err != nil {
			return err
		}
		return r.Rule(sdk.Rule{ID: "rule", Event: "message"}, func(context.Context, *sdk.Context) (sdk.Result, error) {
			calls = append(calls, "rule:session")
			return sdk.Stop, nil
		})
	}}
	other := &fakePlugin{descriptor: sdk.Descriptor{ID: "other", Version: "1"}, rules: func(r sdk.Router) error {
		return r.Accept(func(context.Context, *sdk.Context) (sdk.AcceptDecision, error) {
			calls = append(calls, "accept:other")
			return sdk.AcceptNextPlugin, nil
		})
	}}
	for _, p := range []*fakePlugin{p, other} {
		if err := manager.Register(p); err != nil {
			t.Fatal(err)
		}
	}
	startManager(t, manager)
	evt := &event.Event{PostType: "message", MessageType: "private", UserID: 20}
	if err := manager.HandleEvent(context.Background(), evt); err != nil {
		t.Fatal(err)
	}
	for _, result := range []sdk.Result{sdk.Next, sdk.Stop} {
		if err := saved.Set(time.Minute, func(context.Context, *sdk.Context) (sdk.Result, error) {
			calls = append(calls, "session")
			return result, nil
		}); err != nil {
			t.Fatal(err)
		}
		calls = nil
		if err := manager.HandleEvent(context.Background(), evt); err != nil {
			t.Fatal(err)
		}
		want := []string{"session", "accept:session", "accept:other", "rule:session"}
		if result == sdk.Stop {
			want = []string{"session", "accept:other"}
		}
		if !reflect.DeepEqual(calls, want) {
			t.Fatalf("session/accept/rule order = %v, want %v", calls, want)
		}
	}
}
