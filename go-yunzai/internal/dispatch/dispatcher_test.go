package dispatch

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nianyuechan/otonashi/go-yunzai/internal/event"
)

func TestDispatchBoundsConcurrency(t *testing.T) {
	dispatcher := NewWithLimit(slog.New(slog.NewTextHandler(io.Discard, nil)), 4)
	var active, peak, completed atomic.Int32
	release := make(chan struct{})
	dispatcher.Register(event.HandlerFunc(func(context.Context, *event.Event) error {
		current := active.Add(1)
		for {
			maximum := peak.Load()
			if current <= maximum || peak.CompareAndSwap(maximum, current) {
				break
			}
		}
		<-release
		active.Add(-1)
		completed.Add(1)
		return nil
	}))

	const events = 12
	var dispatched sync.WaitGroup
	for range events {
		dispatched.Add(1)
		go func() {
			defer dispatched.Done()
			dispatcher.Dispatch(context.Background(), &event.Event{})
		}()
	}
	waitFor(t, func() bool { return active.Load() == 4 })
	if got := peak.Load(); got > 4 {
		t.Fatalf("peak concurrency = %d, want <= 4", got)
	}
	close(release)
	dispatched.Wait()
	waitFor(t, func() bool { return completed.Load() == events })
	dispatcher.Wait()
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was not met before deadline")
}
