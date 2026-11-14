package renderer

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"go-render/pkg/render/api"
)

type fakePage struct {
	render func(context.Context, api.Request) ([]byte, error)
	closed atomic.Int32
}

func (p *fakePage) Render(ctx context.Context, req api.Request) ([]byte, error) {
	return p.render(ctx, req)
}

func (p *fakePage) Close() error {
	p.closed.Add(1)
	return nil
}

type fakeBrowser struct {
	done    chan struct{}
	newPage func() Page
	closed  atomic.Int32
}

func (b *fakeBrowser) NewPage(context.Context) (Page, error) { return b.newPage(), nil }
func (b *fakeBrowser) Done() <-chan struct{}                 { return b.done }
func (b *fakeBrowser) Close() error {
	b.closed.Add(1)
	select {
	case <-b.done:
	default:
		close(b.done)
	}
	return nil
}

func testConfig() Config {
	return Config{Concurrency: 1, QueueSize: 4, BrowserStartTimeout: time.Second}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func startService(t *testing.T, cfg Config, factory BrowserFactory) *Service {
	t.Helper()
	service := NewService(factory, cfg, testLogger(), NewMetrics(prometheus.NewRegistry()))
	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	return service
}

func closeService(t *testing.T, service *Service) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := service.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestServiceLimitsConcurrencyAndReusesBrowserPages(t *testing.T) {
	var browserStarts, pageStarts, active, maxActive atomic.Int32
	release := make(chan struct{})
	started := make(chan struct{}, 4)
	factory := func(context.Context) (Browser, error) {
		browserStarts.Add(1)
		browser := &fakeBrowser{done: make(chan struct{})}
		browser.newPage = func() Page {
			pageStarts.Add(1)
			return &fakePage{render: func(ctx context.Context, _ api.Request) ([]byte, error) {
				current := active.Add(1)
				for {
					maximum := maxActive.Load()
					if current <= maximum || maxActive.CompareAndSwap(maximum, current) {
						break
					}
				}
				started <- struct{}{}
				select {
				case <-release:
				case <-ctx.Done():
					active.Add(-1)
					return nil, ctx.Err()
				}
				active.Add(-1)
				return []byte("png"), nil
			}}
		}
		return browser, nil
	}
	cfg := Config{Concurrency: 2, QueueSize: 4, BrowserStartTimeout: time.Second}
	service := startService(t, cfg, factory)

	const requests = 4
	var wg sync.WaitGroup
	errs := make(chan error, requests)
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, err := service.Render(ctx, api.Request{HTML: "ok"})
			errs <- err
		}()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("two workers did not start")
		}
	}
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Render() error = %v", err)
		}
	}
	if got := maxActive.Load(); got != 2 {
		t.Fatalf("maximum concurrency = %d, want 2", got)
	}
	if got := browserStarts.Load(); got != 1 {
		t.Fatalf("browser starts = %d, want 1", got)
	}
	if got := pageStarts.Load(); got != 2 {
		t.Fatalf("page starts = %d, want 2", got)
	}
	closeService(t, service)
}

func TestServiceRejectsRequestWhenQueueIsFull(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 4)
	factory := func(context.Context) (Browser, error) {
		browser := &fakeBrowser{done: make(chan struct{})}
		browser.newPage = func() Page {
			return &fakePage{render: func(ctx context.Context, _ api.Request) ([]byte, error) {
				started <- struct{}{}
				select {
				case <-release:
					return []byte("png"), nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}}
		}
		return browser, nil
	}
	cfg := testConfig()
	cfg.QueueSize = 1
	service := startService(t, cfg, factory)

	firstDone := make(chan error, 1)
	go func() {
		_, err := service.Render(context.Background(), api.Request{HTML: "first"})
		firstDone <- err
	}()
	<-started
	secondDone := make(chan error, 1)
	go func() {
		_, err := service.Render(context.Background(), api.Request{HTML: "second"})
		secondDone <- err
	}()
	waitFor(t, func() bool { return service.queueLen.Load() == 1 })

	_, err := service.Render(context.Background(), api.Request{HTML: "third"})
	assertErrorCode(t, err, api.CodeQueueFull)
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Render() error = %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second Render() error = %v", err)
	}
	closeService(t, service)
}

func TestServiceQueueWaitUsesRequestDeadline(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 4)
	factory := func(context.Context) (Browser, error) {
		browser := &fakeBrowser{done: make(chan struct{})}
		browser.newPage = func() Page {
			return &fakePage{render: func(ctx context.Context, _ api.Request) ([]byte, error) {
				started <- struct{}{}
				select {
				case <-release:
					return []byte("png"), nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}}
		}
		return browser, nil
	}
	service := startService(t, testConfig(), factory)

	firstDone := make(chan error, 1)
	go func() {
		_, err := service.Render(context.Background(), api.Request{HTML: "first"})
		firstDone <- err
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := service.Render(ctx, api.Request{HTML: "queued"})
	assertErrorCode(t, err, api.CodeTimeout)
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Render() error = %v", err)
	}
	closeService(t, service)
}

func TestServiceRecreatesCrashedPageWithoutRestartingBrowser(t *testing.T) {
	var browserStarts, pageStarts atomic.Int32
	factory := func(context.Context) (Browser, error) {
		browserStarts.Add(1)
		browser := &fakeBrowser{done: make(chan struct{})}
		browser.newPage = func() Page {
			pageNumber := pageStarts.Add(1)
			return &fakePage{render: func(context.Context, api.Request) ([]byte, error) {
				if pageNumber == 1 {
					return nil, ErrPageCrashed
				}
				return []byte("png"), nil
			}}
		}
		return browser, nil
	}
	service := startService(t, testConfig(), factory)
	_, err := service.Render(context.Background(), api.Request{HTML: "crash"})
	assertErrorCode(t, err, api.CodePageCrashed)
	data, err := service.Render(context.Background(), api.Request{HTML: "ok"})
	if err != nil || string(data) != "png" {
		t.Fatalf("second Render() = %q, %v", data, err)
	}
	if browserStarts.Load() != 1 || pageStarts.Load() != 2 {
		t.Fatalf("browser starts = %d, page starts = %d; want 1, 2", browserStarts.Load(), pageStarts.Load())
	}
	closeService(t, service)
}

func TestServiceRebuildsCrashedBrowser(t *testing.T) {
	var browserStarts, pageStarts atomic.Int32
	factory := func(context.Context) (Browser, error) {
		browserNumber := browserStarts.Add(1)
		browser := &fakeBrowser{done: make(chan struct{})}
		browser.newPage = func() Page {
			pageStarts.Add(1)
			return &fakePage{render: func(context.Context, api.Request) ([]byte, error) {
				if browserNumber == 1 {
					close(browser.done)
					return nil, ErrBrowserCrashed
				}
				return []byte("png"), nil
			}}
		}
		return browser, nil
	}
	service := startService(t, testConfig(), factory)
	_, err := service.Render(context.Background(), api.Request{HTML: "crash"})
	assertErrorCode(t, err, api.CodeBrowserUnavailable)
	data, err := service.Render(context.Background(), api.Request{HTML: "ok"})
	if err != nil || string(data) != "png" {
		t.Fatalf("second Render() = %q, %v", data, err)
	}
	if browserStarts.Load() != 2 || pageStarts.Load() != 2 {
		t.Fatalf("browser starts = %d, page starts = %d; want 2, 2", browserStarts.Load(), pageStarts.Load())
	}
	closeService(t, service)
}

func TestServiceCloseWaitsForActiveRenderAndRejectsNewWork(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 4)
	factory := func(context.Context) (Browser, error) {
		browser := &fakeBrowser{done: make(chan struct{})}
		browser.newPage = func() Page {
			return &fakePage{render: func(context.Context, api.Request) ([]byte, error) {
				started <- struct{}{}
				<-release
				return []byte("png"), nil
			}}
		}
		return browser, nil
	}
	service := startService(t, testConfig(), factory)
	renderDone := make(chan error, 1)
	go func() {
		_, err := service.Render(context.Background(), api.Request{HTML: "active"})
		renderDone <- err
	}()
	<-started
	closeDone := make(chan error, 1)
	go func() { closeDone <- service.Close(context.Background()) }()
	select {
	case err := <-closeDone:
		t.Fatalf("Close() returned before active render finished: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	_, err := service.Render(context.Background(), api.Request{HTML: "new"})
	assertErrorCode(t, err, api.CodeUnavailable)
	close(release)
	if err := <-renderDone; err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if err := <-closeDone; err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestServiceAppliesBrowserStartTimeout(t *testing.T) {
	factory := func(ctx context.Context) (Browser, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 100*time.Millisecond {
			return nil, errors.New("browser factory did not receive startup deadline")
		}
		browser := &fakeBrowser{done: make(chan struct{})}
		browser.newPage = func() Page {
			return &fakePage{render: func(context.Context, api.Request) ([]byte, error) {
				return []byte("png"), nil
			}}
		}
		return browser, nil
	}
	cfg := testConfig()
	cfg.BrowserStartTimeout = 50 * time.Millisecond
	service := startService(t, cfg, factory)
	closeService(t, service)
}

func TestServiceStartFailureCanBeRetried(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	factory := func(context.Context) (Browser, error) {
		if fail.Load() {
			return nil, errors.New("synthetic startup failure")
		}
		browser := &fakeBrowser{done: make(chan struct{})}
		browser.newPage = func() Page {
			return &fakePage{render: func(context.Context, api.Request) ([]byte, error) {
				return []byte("png"), nil
			}}
		}
		return browser, nil
	}

	service := NewService(factory, testConfig(), testLogger(), NewMetrics(prometheus.NewRegistry()))
	if err := service.Start(context.Background()); err == nil {
		t.Fatal("first Start() unexpectedly succeeded")
	}

	fail.Store(false)
	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("second Start() error = %v", err)
	}
	closeService(t, service)
}

func TestServiceCloseReturnsWhenWorkerExceedsDeadline(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	factory := func(context.Context) (Browser, error) {
		browser := &fakeBrowser{done: make(chan struct{})}
		browser.newPage = func() Page {
			return &fakePage{render: func(context.Context, api.Request) ([]byte, error) {
				started <- struct{}{}
				<-release
				return []byte("png"), nil
			}}
		}
		return browser, nil
	}

	service := startService(t, testConfig(), factory)
	renderDone := make(chan error, 1)
	go func() {
		_, err := service.Render(context.Background(), api.Request{HTML: "blocked"})
		renderDone <- err
	}()
	<-started

	closeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := service.Close(closeCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close() error = %v, want context deadline exceeded", err)
	}

	close(release)
	select {
	case <-renderDone:
	case <-time.After(time.Second):
		t.Fatal("render worker did not finish after release")
	}
}

func TestBrowserManagerDoesNotRestartAfterClose(t *testing.T) {
	var starts atomic.Int32
	factory := func(context.Context) (Browser, error) {
		starts.Add(1)
		browser := &fakeBrowser{done: make(chan struct{})}
		browser.newPage = func() Page { return &fakePage{} }
		return browser, nil
	}
	manager := &browserManager{factory: factory, metrics: NewMetrics(prometheus.NewRegistry()), startTimeout: time.Second}
	slot, err := manager.newPage(context.Background())
	if err != nil {
		t.Fatalf("newPage() error = %v", err)
	}
	if err := slot.page.Close(); err != nil {
		t.Fatal(err)
	}
	if err := manager.close(); err != nil {
		t.Fatalf("close() error = %v", err)
	}
	if _, err := manager.newPage(context.Background()); !errors.Is(err, errBrowserManagerClosed) {
		t.Fatalf("newPage() after close error = %v, want errBrowserManagerClosed", err)
	}
	if starts.Load() != 1 {
		t.Fatalf("browser factory starts = %d, want 1", starts.Load())
	}
}

func TestBrowserManagerCloseDoesNotWaitForBrowserStart(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	browser := &fakeBrowser{done: make(chan struct{})}
	browser.newPage = func() Page { return &fakePage{} }
	factory := func(context.Context) (Browser, error) {
		close(entered)
		<-release
		return browser, nil
	}
	manager := &browserManager{factory: factory, metrics: NewMetrics(prometheus.NewRegistry()), startTimeout: time.Second}
	result := make(chan error, 1)
	go func() {
		_, err := manager.newPage(context.Background())
		result <- err
	}()
	<-entered
	closed := make(chan error, 1)
	go func() { closed <- manager.close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("close() blocked on an in-progress browser start")
	}
	close(release)
	select {
	case err := <-result:
		if !errors.Is(err, errBrowserManagerClosed) {
			t.Fatalf("newPage() error = %v, want errBrowserManagerClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("newPage() did not finish")
	}
	if browser.closed.Load() != 1 {
		t.Fatalf("browser started after close was not released: closed=%d", browser.closed.Load())
	}
}

func assertErrorCode(t *testing.T, err error, code string) {
	t.Helper()
	var apiErr *api.Error
	if !errors.As(err, &apiErr) || apiErr.Code != code {
		t.Fatalf("error = %#v, want code %q", err, code)
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was not met before deadline")
}
