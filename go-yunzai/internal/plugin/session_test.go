package plugin

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nianyuechan/otonashi/go-yunzai/internal/event"
	"github.com/nianyuechan/otonashi/go-yunzai/internal/message"
	"github.com/nianyuechan/otonashi/go-yunzai/pkg/plugin/sdk"
)

func TestSessionContinuationRunsOnceBeforeRules(t *testing.T) {
	var calls []string
	manager := New(&fakeSender{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	p := &fakePlugin{
		descriptor: sdk.Descriptor{ID: "conversation", Version: "1"},
		rules: func(router sdk.Router) error {
			if err := router.Rule(sdk.Rule{ID: "begin", Event: "message", Pattern: `^begin$`}, func(_ context.Context, c *sdk.Context) (sdk.Result, error) {
				calls = append(calls, "begin")
				return sdk.Stop, c.Session.Set(time.Minute, func(context.Context, *sdk.Context) (sdk.Result, error) {
					calls = append(calls, "continuation")
					return sdk.Stop, nil
				})
			}); err != nil {
				return err
			}
			return router.Rule(sdk.Rule{ID: "fallback", Event: "message"}, func(context.Context, *sdk.Context) (sdk.Result, error) {
				calls = append(calls, "fallback")
				return sdk.Stop, nil
			})
		},
	}
	if err := manager.Register(p); err != nil {
		t.Fatal(err)
	}
	startManager(t, manager)
	for _, text := range []string{"begin", "answer", "ordinary"} {
		if err := manager.HandleEvent(context.Background(), &event.Event{
			PostType: "message", MessageType: "private", SelfID: 11, UserID: 22,
			Message: []message.Segment{message.Text(text)},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(calls); got != 3 || calls[0] != "begin" || calls[1] != "continuation" || calls[2] != "fallback" {
		t.Fatalf("conversation calls = %v", calls)
	}
}

func TestSessionScopesDoNotConsumeOtherConversations(t *testing.T) {
	store := newSessionStore(time.Now)
	defer store.close()
	base := sdk.Event{SelfID: 10, UserID: 20, GroupID: 30, MessageType: "group", PostType: "message"}
	scope := store.scope("one", &base)
	if err := scope.Set(time.Minute, func(context.Context, *sdk.Context) (sdk.Result, error) { return sdk.Stop, nil }); err != nil {
		t.Fatal(err)
	}
	otherAccount, otherUser, otherGroup, otherType := base, base, base, base
	otherAccount.SelfID++
	otherUser.UserID++
	otherGroup.GroupID++
	otherType.MessageType, otherType.GroupID = "private", 0
	for _, other := range []scopedSession{
		store.scope("two", &base), store.scope("one", &otherAccount), store.scope("one", &otherUser),
		store.scope("one", &otherGroup), store.scope("one", &otherType),
	} {
		if store.take(other.key) != nil {
			t.Fatalf("unrelated scope consumed session: %+v", other.key)
		}
	}
	if store.take(scope.key) == nil {
		t.Fatal("original conversation lost its pending session")
	}
}

func TestSessionReplaceClearAndExpire(t *testing.T) {
	now := time.Unix(100, 0)
	store := newSessionStore(func() time.Time { return now })
	defer store.close()
	scope := store.scope("one", &sdk.Event{UserID: 20, MessageType: "private"})
	var got int
	handler := func(value int) sdk.HandlerFunc {
		return func(context.Context, *sdk.Context) (sdk.Result, error) { got = value; return sdk.Stop, nil }
	}
	if err := scope.Set(time.Minute, handler(1)); err != nil {
		t.Fatal(err)
	}
	if err := scope.Set(time.Minute, handler(2)); err != nil {
		t.Fatal(err)
	}
	fn := store.take(scope.key)
	if fn == nil {
		t.Fatal("replacement missing")
	}
	_, _ = fn(context.Background(), nil)
	if got != 2 {
		t.Fatalf("replacement result = %d, want 2", got)
	}
	if err := scope.Set(time.Minute, handler(3)); err != nil {
		t.Fatal(err)
	}
	scope.Clear()
	if store.take(scope.key) != nil {
		t.Fatal("Clear retained continuation")
	}
	if err := scope.Set(time.Minute, handler(4)); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	if store.take(scope.key) != nil {
		t.Fatal("expired continuation still available")
	}
	store.close()
	if err := scope.Set(time.Minute, handler(5)); !errors.Is(err, sdk.ErrNotRunning) {
		t.Fatalf("Set after shutdown = %v, want ErrNotRunning", err)
	}
}

func TestSessionTimerReclaimsIdleEntries(t *testing.T) {
	store := newSessionStore(time.Now)
	defer store.close()
	scope := store.scope("one", &sdk.Event{UserID: 20, MessageType: "private"})
	if err := scope.Set(time.Millisecond, func(context.Context, *sdk.Context) (sdk.Result, error) { return sdk.Stop, nil }); err != nil {
		t.Fatal(err)
	}
	timeout := time.NewTimer(time.Second)
	defer timeout.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		store.mu.Lock()
		remaining := len(store.entries)
		store.mu.Unlock()
		if remaining == 0 {
			return
		}
		select {
		case <-timeout.C:
			t.Fatal("idle session was never reclaimed")
		case <-ticker.C:
		}
	}
}

func TestSessionConcurrentMessagesConsumeOnlyOnce(t *testing.T) {
	store := newSessionStore(time.Now)
	defer store.close()
	scope := store.scope("one", &sdk.Event{UserID: 20, MessageType: "private"})
	if err := scope.Set(time.Minute, func(context.Context, *sdk.Context) (sdk.Result, error) { return sdk.Stop, nil }); err != nil {
		t.Fatal(err)
	}
	var consumed atomic.Int32
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if store.take(scope.key) != nil {
				consumed.Add(1)
			}
		}()
	}
	wg.Wait()
	if consumed.Load() != 1 {
		t.Fatalf("continuation consumed %d times", consumed.Load())
	}
}
