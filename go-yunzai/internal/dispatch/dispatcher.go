package dispatch

import (
	"context"
	"log/slog"
	"sync"

	"github.com/nianyuechan/otonashi/go-yunzai/internal/event"
)

// DefaultMaxConcurrent 限制同时处理的事件数，避免突发流量无限创建 goroutine。
const DefaultMaxConcurrent = 256

// Dispatcher 分发事件，不增加跨事件串行队列。
type Dispatcher struct {
	mu       sync.RWMutex
	handlers []event.Handler
	logger   *slog.Logger
	wg       sync.WaitGroup
	slots    chan struct{}
}

func New(logger *slog.Logger) *Dispatcher {
	return NewWithLimit(logger, DefaultMaxConcurrent)
}

func NewWithLimit(logger *slog.Logger, maxConcurrent int) *Dispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}
	return &Dispatcher{logger: logger, slots: make(chan struct{}, maxConcurrent)}
}

func (d *Dispatcher) Register(handler event.Handler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers = append(d.handlers, handler)
}

func (d *Dispatcher) Dispatch(ctx context.Context, evt *event.Event) {
	select {
	case d.slots <- struct{}{}:
	case <-ctx.Done():
		return
	}
	d.mu.RLock()
	handlers := append([]event.Handler(nil), d.handlers...)
	d.wg.Add(1)
	d.mu.RUnlock()

	go func() {
		defer d.wg.Done()
		defer func() { <-d.slots }()
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
