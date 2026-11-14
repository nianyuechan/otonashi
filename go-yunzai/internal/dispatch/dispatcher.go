package dispatch

import (
	"context"
	"log/slog"
	"sync"

	"github.com/trss-yunzai/go-yunzai/internal/event"
)

// Dispatcher 分发事件，不增加跨事件串行队列。
type Dispatcher struct {
	mu       sync.RWMutex
	handlers []event.Handler
	logger   *slog.Logger
	wg       sync.WaitGroup
}

func New(logger *slog.Logger) *Dispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &Dispatcher{logger: logger}
}

func (d *Dispatcher) Register(handler event.Handler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers = append(d.handlers, handler)
}

func (d *Dispatcher) Dispatch(ctx context.Context, evt *event.Event) {
	d.mu.RLock()
	handlers := append([]event.Handler(nil), d.handlers...)
	d.wg.Add(1)
	d.mu.RUnlock()

	go func() {
		defer d.wg.Done()
		for _, handler := range handlers {
			if err := handler.HandleEvent(ctx, evt); err != nil {
				d.logger.Error("event handler failed", "post_type", evt.PostType, "error", err)
			}
		}
	}()
}

// Wait 在适配器停止接收新事件后调用。
func (d *Dispatcher) Wait() {
	d.wg.Wait()
}

// WaitContext bounds the drain after the adapter has stopped accepting events.
func (d *Dispatcher) WaitContext(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
