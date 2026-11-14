package renderer

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/inspector"
	"github.com/chromedp/cdproto/network"
	cdpage "github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"go-render/pkg/render/api"
)

type chromedpBrowser struct {
	ctx         context.Context
	allocCancel context.CancelFunc
}

func NewChromedpBrowser(ctx context.Context, executable string) (Browser, error) {
	opts := append(slices.Clone(chromedp.DefaultExecAllocatorOptions[:]),
		chromedp.ExecPath(executable),
		chromedp.DisableGPU,
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	if err := initializeTarget(ctx, browserCtx); err != nil {
		browserCancel()
		allocCancel()
		return nil, err
	}
	return &chromedpBrowser{ctx: browserCtx, allocCancel: allocCancel}, nil
}

func (b *chromedpBrowser) NewPage(ctx context.Context) (Page, error) {
	pageCtx, pageCancel := chromedp.NewContext(b.ctx)
	if err := initializeTarget(ctx, pageCtx); err != nil {
		pageCancel()
		return nil, err
	}
	p := &chromedpPage{
		ctx:         pageCtx,
		browserDone: b.ctx.Done(),
		network:     newNetworkTracker(),
	}
	if err := chromedp.Listen(pageCtx, p.handleEvent); err != nil {
		pageCancel()
		return nil, err
	}
	if err := chromedp.Do(pageCtx,
		protocolAction(network.Enable, network.EnableParams{}),
		protocolAction(inspector.Enable, cdp.Empty{}),
	); err != nil {
		pageCancel()
		return nil, err
	}
	return p, nil
}

func (b *chromedpBrowser) Done() <-chan struct{} {
	return b.ctx.Done()
}

func (b *chromedpBrowser) Close() error {
	ctx, cancel := context.WithTimeout(b.ctx, 5*time.Second)
	defer cancel()
	err := chromedp.Cancel(ctx)
	b.allocCancel()
	return err
}

func initializeTarget(ctx context.Context, targetCtx context.Context) error {
	initialized := make(chan error, 1)
	go func() { initialized <- chromedp.Do(targetCtx) }()
	select {
	case err := <-initialized:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func protocolAction[P, R any](command cdp.Command[P, R], params P) chromedp.Action[chromedp.Void] {
	return chromedp.Func(func(ctx context.Context, target *chromedp.Target) error {
		_, err := cdp.Call(ctx, target, command, params)
		return err
	})
}

type chromedpPage struct {
	ctx         context.Context
	browserDone <-chan struct{}
	crashed     atomic.Bool
	network     *networkTracker
}

func (p *chromedpPage) Render(ctx context.Context, req api.Request) ([]byte, error) {
	p.crashed.Store(false)
	p.network.reset()
	renderCtx, cancel := context.WithCancel(p.ctx)
	stopCaller := context.AfterFunc(ctx, cancel)
	defer stopCaller()
	defer cancel()
	if deadline, ok := ctx.Deadline(); ok {
		var deadlineCancel context.CancelFunc
		renderCtx, deadlineCancel = context.WithDeadline(renderCtx, deadline)
		defer deadlineCancel()
	}

	err := chromedp.Do(renderCtx,
		protocolAction(emulation.SetDeviceMetricsOverride, emulation.SetDeviceMetricsOverrideParams{
			Width:             int64(req.Options.Width),
			Height:            int64(req.Options.Height),
			DeviceScaleFactor: req.Options.DeviceScaleFactor,
			Mobile:            false,
		}),
		chromedp.Navigate("about:blank"),
		chromedp.Func(func(ctx context.Context, target *chromedp.Target) error {
			frameTree, err := cdp.Call(ctx, target, cdpage.GetFrameTree, cdp.Empty{})
			if err != nil {
				return err
			}
			_, err = cdp.Call(ctx, target, cdpage.SetDocumentContent, cdpage.SetDocumentContentParams{
				FrameID: frameTree.FrameTree.Frame.ID,
				HTML:    req.HTML,
			})
			return err
		}),
	)
	if err != nil {
		return nil, p.classifyError(ctx, err)
	}

	readyExpression := "document.readyState !== 'loading'"
	if req.Options.WaitUntil != api.WaitDOMContentLoaded {
		readyExpression = "document.readyState === 'complete' && (!document.fonts || document.fonts.status === 'loaded')"
	}
	_, err = chromedp.Run(renderCtx, chromedp.Poll[bool](readyExpression,
		chromedp.WithPollingInterval(20*time.Millisecond),
		chromedp.WithPollingTimeout(0),
	))
	if err == nil && req.Options.WaitSelector != "" {
		err = chromedp.Do(renderCtx, chromedp.WaitReady(chromedp.CSS(req.Options.WaitSelector)))
	}
	if err == nil && req.Options.WaitUntil == api.WaitNetworkIdle {
		err = p.network.wait(renderCtx, 500*time.Millisecond)
	}
	if err != nil {
		return nil, p.classifyError(ctx, err)
	}

	image, err := chromedp.Run(renderCtx, chromedp.CaptureScreenshot())
	if err != nil {
		return nil, p.classifyError(ctx, err)
	}
	return image, nil
}

func (p *chromedpPage) Close() error {
	ctx, cancel := context.WithTimeout(p.ctx, 2*time.Second)
	defer cancel()
	return chromedp.Cancel(ctx)
}

func (p *chromedpPage) handleEvent(event any) {
	switch event := event.(type) {
	case *inspector.EventTargetCrashed:
		p.crashed.Store(true)
	case *network.EventRequestWillBeSent:
		p.network.requestStarted(event)
	case *network.EventLoadingFinished:
		p.network.requestFinished(event.RequestID)
	case *network.EventLoadingFailed:
		p.network.requestFinished(event.RequestID)
	case *network.EventRequestServedFromCache:
		p.network.requestFinished(event.RequestID)
	}
}

func (p *chromedpPage) classifyError(callerCtx context.Context, err error) error {
	if callerCtx.Err() != nil {
		return callerCtx.Err()
	}
	if p.crashed.Load() {
		return fmt.Errorf("%w: %v", ErrPageCrashed, err)
	}
	if done(p.browserDone) {
		return fmt.Errorf("%w: %v", ErrBrowserCrashed, err)
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "target crashed"):
		return fmt.Errorf("%w: %v", ErrPageCrashed, err)
	case strings.Contains(message, "target closed"), strings.Contains(message, "session closed"):
		return fmt.Errorf("%w: %v", ErrPageCrashed, err)
	case strings.Contains(message, "websocket closed"), strings.Contains(message, "browser process"):
		return fmt.Errorf("%w: %v", ErrBrowserCrashed, err)
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return err
	default:
		return err
	}
}

type networkTracker struct {
	mu       sync.Mutex
	pending  map[network.RequestID]struct{}
	activity time.Time
}

func newNetworkTracker() *networkTracker {
	return &networkTracker{pending: make(map[network.RequestID]struct{}), activity: time.Now()}
}

func (t *networkTracker) reset() {
	t.mu.Lock()
	t.pending = make(map[network.RequestID]struct{})
	t.activity = time.Now()
	t.mu.Unlock()
}

func (t *networkTracker) requestStarted(event *network.EventRequestWillBeSent) {
	if event.Request != nil && strings.HasPrefix(event.Request.URL, "data:") {
		return
	}
	t.mu.Lock()
	t.pending[event.RequestID] = struct{}{}
	t.activity = time.Now()
	t.mu.Unlock()
}

func (t *networkTracker) requestFinished(id network.RequestID) {
	t.mu.Lock()
	delete(t.pending, id)
	t.activity = time.Now()
	t.mu.Unlock()
}

func (t *networkTracker) wait(ctx context.Context, quietWindow time.Duration) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		t.mu.Lock()
		idle := len(t.pending) == 0 && time.Since(t.activity) >= quietWindow
		t.mu.Unlock()
		if idle {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
