package renderer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nianyuechan/otonashi/go-render/pkg/render/api"
)

var (
	ErrPageCrashed          = errors.New("page crashed")
	ErrBrowserCrashed       = errors.New("browser crashed")
	errBrowserManagerClosed = errors.New("browser manager is closed")
)

type Page interface {
	Render(context.Context, api.Request) ([]byte, error)
	Close() error
}

type Browser interface {
	NewPage(context.Context) (Page, error)
	Done() <-chan struct{}
	Close() error
}

type BrowserFactory func(context.Context) (Browser, error)

type Config struct {
	Concurrency         int
	QueueSize           int
	BrowserStartTimeout time.Duration
}

type Service struct {
	factory BrowserFactory
	config  Config
	logger  *slog.Logger
	metrics *Metrics
	manager *browserManager

	jobs     chan renderJob
	queueLen atomic.Int64
	forceCtx context.Context
	force    context.CancelFunc
	wg       sync.WaitGroup

	mu      sync.RWMutex
	started bool
	closed  bool
}

type renderJob struct {
	ctx    context.Context
	req    api.Request
	result chan renderResult
}

type renderResult struct {
	data []byte
	err  error
}

type pageSlot struct {
	page       Page
	generation uint64
}

func NewService(factory BrowserFactory, cfg Config, logger *slog.Logger, metrics *Metrics) *Service {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 1
	}
	if cfg.QueueSize < 0 {
		cfg.QueueSize = 0
	}
	if cfg.BrowserStartTimeout <= 0 {
		cfg.BrowserStartTimeout = 15 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	forceCtx, force := context.WithCancel(context.Background())
	service := &Service{
		factory:  factory,
		config:   cfg,
		logger:   logger,
		metrics:  metrics,
		jobs:     make(chan renderJob, cfg.QueueSize),
		forceCtx: forceCtx,
		force:    force,
	}
	service.manager = &browserManager{factory: factory, metrics: metrics, startTimeout: cfg.BrowserStartTimeout}
	return service
}

func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.closed || s.started {
		s.mu.Unlock()
		return fmt.Errorf("renderer service cannot be started")
	}
	s.started = true
	s.mu.Unlock()

	slots := make([]pageSlot, 0, s.config.Concurrency)
	for i := 0; i < s.config.Concurrency; i++ {
		slot, err := s.manager.newPage(ctx)
		if err != nil {
			for _, created := range slots {
				_ = created.page.Close()
			}
			_ = s.manager.reset()
			s.mu.Lock()
			s.started = false
			s.mu.Unlock()
			return fmt.Errorf("create browser page %d: %w", i+1, err)
		}
		slots = append(slots, slot)
	}
	for _, slot := range slots {
		s.wg.Add(1)
		go s.worker(slot)
	}
	s.logger.Info("renderer started", "concurrency", s.config.Concurrency, "queue_size", s.config.QueueSize)
	return nil
}

func (s *Service) Render(ctx context.Context, req api.Request) (data []byte, err error) {
	start := time.Now()
	defer func() { s.metrics.observe(start, err) }()

	jobCtx, cancel := context.WithCancel(ctx)
	stopForce := context.AfterFunc(s.forceCtx, cancel)
	defer stopForce()
	defer cancel()
	job := renderJob{ctx: jobCtx, req: req, result: make(chan renderResult, 1)}

	s.mu.RLock()
	if !s.started || s.closed {
		s.mu.RUnlock()
		return nil, &api.Error{Code: api.CodeUnavailable, Message: "renderer is not accepting requests"}
	}
	select {
	case <-jobCtx.Done():
		s.mu.RUnlock()
		return nil, mapError(jobCtx.Err())
	default:
	}
	s.queueLen.Add(1)
	s.metrics.queue.Inc()
	select {
	case s.jobs <- job:
		s.mu.RUnlock()
	default:
		s.queueLen.Add(-1)
		s.metrics.queue.Dec()
		s.mu.RUnlock()
		return nil, &api.Error{Code: api.CodeQueueFull, Message: "render queue is full"}
	}

	select {
	case result := <-job.result:
		return result.data, result.err
	case <-jobCtx.Done():
		return nil, mapError(jobCtx.Err())
	}
}

func (s *Service) worker(slot pageSlot) {
	defer s.wg.Done()
	defer func() {
		if slot.page != nil {
			_ = slot.page.Close()
		}
	}()
	for job := range s.jobs {
		s.queueLen.Add(-1)
		s.metrics.queue.Dec()
		if err := job.ctx.Err(); err != nil {
			job.result <- renderResult{err: mapError(err)}
			continue
		}

		var err error
		if slot.page == nil || slot.generation != s.manager.currentGeneration() || s.manager.browserStopped(slot.generation) {
			if slot.page != nil {
				_ = slot.page.Close()
				slot.page = nil
			}
			slot, err = s.manager.newPage(job.ctx)
			if err != nil {
				s.logger.Error("browser page recovery failed", "error", err)
				job.result <- renderResult{err: &api.Error{Code: api.CodeBrowserUnavailable, Message: "browser is unavailable"}}
				continue
			}
		}

		s.metrics.active.Inc()
		data, renderErr := slot.page.Render(job.ctx, job.req)
		s.metrics.active.Dec()
		if renderErr != nil {
			switch {
			case errors.Is(renderErr, ErrBrowserCrashed), s.manager.browserStopped(slot.generation):
				s.logger.Warn("browser crashed; invalidating browser and its pages")
				s.manager.invalidate(slot.generation)
				_ = slot.page.Close()
				slot.page = nil
				renderErr = mapError(ErrBrowserCrashed)
			case errors.Is(renderErr, ErrPageCrashed):
				s.logger.Warn("page crashed; recreating it for the next request")
				_ = slot.page.Close()
				slot.page = nil
				renderErr = mapError(ErrPageCrashed)
			default:
				renderErr = mapError(renderErr)
			}
		}
		job.result <- renderResult{data: data, err: renderErr}
	}
}

func (s *Service) Close(ctx context.Context) error {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		close(s.jobs)
	}
	s.mu.Unlock()

	workersDone := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(workersDone)
	}()
	select {
	case <-workersDone:
		s.force()
		return s.manager.close()
	case <-ctx.Done():
		s.force()
		_ = s.manager.close()
		// A page implementation may fail to honor cancellation. Return at the
		// caller's deadline instead of waiting forever for that worker.
		return ctx.Err()
	}
}

func mapError(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		return err
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return &api.Error{Code: api.CodeTimeout, Message: "render exceeded its deadline"}
	case errors.Is(err, ErrPageCrashed):
		return &api.Error{Code: api.CodePageCrashed, Message: "browser page crashed"}
	case errors.Is(err, ErrBrowserCrashed):
		return &api.Error{Code: api.CodeBrowserUnavailable, Message: "browser crashed"}
	case errors.Is(err, context.Canceled):
		return &api.Error{Code: api.CodeUnavailable, Message: "render was canceled"}
	default:
		return fmt.Errorf("render page: %w", err)
	}
}

type browserManager struct {
	factory      BrowserFactory
	metrics      *Metrics
	startTimeout time.Duration

	mu         sync.Mutex
	startMu    sync.Mutex
	browser    Browser
	generation uint64
	closed     bool
}

func (m *browserManager) newPage(ctx context.Context) (pageSlot, error) {
	// Serialize page acquisition without holding mu: starting a browser can
	// block for seconds, and close/generation queries must not wait on it.
	m.startMu.Lock()
	defer m.startMu.Unlock()

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return pageSlot{}, errBrowserManagerClosed
	}
	browser := m.browser
	generation := m.generation
	if browser != nil && done(browser.Done()) {
		_ = browser.Close()
		browser = nil
		m.browser = nil
	}
	m.mu.Unlock()

	startCtx, cancel := context.WithTimeout(ctx, m.startTimeout)
	defer cancel()
	if browser == nil {
		started, err := m.factory(startCtx)
		if err != nil {
			return pageSlot{}, fmt.Errorf("start browser: %w", err)
		}
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			_ = started.Close()
			return pageSlot{}, errBrowserManagerClosed
		}
		m.browser = started
		m.generation++
		generation = m.generation
		m.mu.Unlock()
		m.metrics.browserStarts.Inc()
		browser = started
	}
	page, err := browser.NewPage(startCtx)
	if err != nil {
		m.mu.Lock()
		if m.browser == browser {
			_ = browser.Close()
			m.browser = nil
		}
		m.mu.Unlock()
		return pageSlot{}, fmt.Errorf("create page: %w", err)
	}
	return pageSlot{page: page, generation: generation}, nil
}

func (m *browserManager) currentGeneration() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.generation
}

func (m *browserManager) browserStopped(generation uint64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return generation != m.generation || m.browser == nil || done(m.browser.Done())
}

func (m *browserManager) invalidate(generation uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if generation != m.generation {
		return
	}
	if m.browser != nil {
		_ = m.browser.Close()
	}
	m.browser = nil
}

func (m *browserManager) reset() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closeBrowser()
}

func (m *browserManager) close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return m.closeBrowser()
}

func (m *browserManager) closeBrowser() error {
	if m.browser == nil {
		return nil
	}
	err := m.browser.Close()
	m.browser = nil
	return err
}

func done(channel <-chan struct{}) bool {
	select {
	case <-channel:
		return true
	default:
		return false
	}
}
