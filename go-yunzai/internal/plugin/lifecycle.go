package plugin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/trss-yunzai/go-yunzai/pkg/plugin/sdk"
)

const DefaultShutdownTimeout = 5 * time.Second

type managerRun struct {
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	done     chan struct{}
	ready    bool
	stopping bool
	stopErr  error
}

// Admission flags are protected by Manager.mu. Completion channels publish
// stopErr and prevent a new generation from overlapping unfinished cleanup.
type pluginRun struct {
	ctx            context.Context
	cancel         context.CancelFunc
	sessions       *sessionStore
	wg             sync.WaitGroup
	done           chan struct{}
	ready          bool
	started        bool
	stopping       bool
	cleanupStarted bool
	remove         bool
	stopErr        error
}

func (m *Manager) newPluginRun(ctx context.Context) *pluginRun {
	ctx, cancel := context.WithCancel(ctx)
	return &pluginRun{ctx: ctx, cancel: cancel, sessions: newSessionStore(m.now), done: make(chan struct{})}
}

// Start runs hooks in priority order. No tasks or events are admitted until
// every hook succeeds. Lifecycle hooks must cooperate with cancellation.
func (m *Manager) Start(ctx context.Context) error {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	if m.run != nil && !m.run.stopping {
		err := m.run.ctx.Err()
		m.mu.Unlock()
		return err
	}
	if m.run != nil && !runFinished(m.run) {
		m.mu.Unlock()
		return fmt.Errorf("plugin manager is still stopping")
	}
	runCtx, cancel := context.WithCancel(ctx)
	run := &managerRun{ctx: runCtx, cancel: cancel, done: make(chan struct{})}
	m.run = run
	plugins := append([]*registeredPlugin(nil), m.plugins...)
	for _, p := range plugins {
		p.run = m.newPluginRun(runCtx)
	}
	m.mu.Unlock()
	for _, p := range plugins {
		err := callLifecycle(p.plugin.Start, p.run.ctx)
		m.mu.Lock()
		p.run.started = err == nil
		m.mu.Unlock()
		if err == nil {
			err = runCtx.Err()
		}
		if err != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), DefaultShutdownTimeout)
			defer cleanupCancel()
			m.beginStop(run, plugins, cleanupCtx)
			return errors.Join(fmt.Errorf("start plugin %q: %w", p.descriptor.ID, err), waitForStop(cleanupCtx, run))
		}
	}
	m.mu.Lock()
	if err := runCtx.Err(); err != nil {
		m.mu.Unlock()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), DefaultShutdownTimeout)
		defer cleanupCancel()
		m.beginStop(run, plugins, cleanupCtx)
		return errors.Join(err, waitForStop(cleanupCtx, run))
	}
	for _, p := range plugins {
		m.activate(p)
	}
	run.ready = true
	m.mu.Unlock()
	return nil
}

// Load admits an already constructed Go plugin while the manager is running.
// The operation context only controls startup; a successful plugin belongs to
// the manager's lifetime. This is not a native shared-library loader.
func (m *Manager) Load(ctx context.Context, plugin sdk.Plugin) error {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.RLock()
	run := m.run
	running := run != nil && run.ready && !run.stopping && run.ctx.Err() == nil
	m.mu.RUnlock()
	if !running {
		return sdk.ErrNotRunning
	}
	p, err := m.prepare(plugin)
	if err != nil {
		return err
	}
	m.mu.Lock()
	p.run = m.newPluginRun(run.ctx)
	m.plugins = append(m.plugins, p)
	m.sortPlugins()
	m.mu.Unlock()
	stopCaller := context.AfterFunc(ctx, p.run.cancel)
	err = callLifecycle(p.plugin.Start, p.run.ctx)
	detached := stopCaller()
	m.mu.Lock()
	p.run.started = err == nil
	if err == nil {
		err = errors.Join(ctx.Err(), p.run.ctx.Err())
		if !detached && err == nil {
			err = context.Canceled
		}
	}
	if err == nil {
		m.activate(p)
		m.mu.Unlock()
		return nil
	}
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), DefaultShutdownTimeout)
	defer cleanupCancel()
	m.beginPluginStop(p, cleanupCtx, true)
	m.mu.Unlock()
	return errors.Join(fmt.Errorf("start plugin %q: %w", p.descriptor.ID, err), waitForPluginStop(cleanupCtx, p.run))
}

// Unload stops admission, cancels this plugin's handlers and tasks, invalidates
// sessions, then drains and stops it. Missing ids are harmless. After a timeout,
// cleanup continues and the id stays reserved until it completes.
func (m *Manager) Unload(ctx context.Context, id string) error {
	ctx, cancel := shutdownContext(ctx)
	defer cancel()
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("plugin id is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.lifecycleMu.Lock()
	m.mu.Lock()
	if m.run != nil && m.run.stopping && !runFinished(m.run) {
		m.mu.Unlock()
		m.lifecycleMu.Unlock()
		return fmt.Errorf("plugin manager is stopping")
	}
	var found *registeredPlugin
	for _, p := range m.plugins {
		if p.descriptor.ID == id {
			found = p
			break
		}
	}
	if found == nil || found.run == nil {
		if found != nil {
			m.removePlugin(found)
		}
		m.mu.Unlock()
		m.lifecycleMu.Unlock()
		return nil
	}
	pr := found.run
	m.beginPluginStop(found, ctx, true)
	m.mu.Unlock()
	m.lifecycleMu.Unlock()
	return waitForPluginStop(ctx, pr)
}

func (m *Manager) Stop(ctx context.Context) error {
	ctx, cancel := shutdownContext(ctx)
	defer cancel()
	m.lifecycleMu.Lock()
	m.mu.RLock()
	run := m.run
	plugins := append([]*registeredPlugin(nil), m.plugins...)
	m.mu.RUnlock()
	if run != nil {
		m.beginStop(run, plugins, ctx)
	}
	m.lifecycleMu.Unlock()
	if run == nil {
		return nil
	}
	return waitForStop(ctx, run)
}

func shutdownContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, DefaultShutdownTimeout)
}

// activate, quiesce, beginPluginStop, and removePlugin require mu held.
func (m *Manager) activate(p *registeredPlugin) {
	for _, task := range p.tasks {
		p.run.wg.Add(1)
		go m.runTask(p.run, p, task)
	}
	p.run.ready = true
}

func quiesce(pr *pluginRun) {
	pr.stopping = true
	pr.cancel()
	pr.sessions.close()
}

func (m *Manager) beginPluginStop(p *registeredPlugin, ctx context.Context, remove bool) {
	pr := p.run
	pr.remove = pr.remove || remove
	if channelClosed(pr.done) {
		if pr.remove {
			m.removePlugin(p)
		}
		return
	}
	if pr.cleanupStarted {
		return
	}
	quiesce(pr)
	pr.cleanupStarted = true
	go func() {
		pr.wg.Wait()
		var err error
		if pr.started {
			if stopErr := callLifecycle(p.plugin.Stop, ctx); stopErr != nil {
				err = fmt.Errorf("stop plugin %q: %w", p.descriptor.ID, stopErr)
			}
		}
		m.mu.Lock()
		pr.stopErr = err
		m.clearCooldowns(p.descriptor.ID)
		if pr.remove {
			m.removePlugin(p)
		}
		close(pr.done)
		m.mu.Unlock()
		if err != nil {
			m.logger.Error("plugin cleanup failed", "plugin", p.descriptor.ID, "error", err)
		}
	}()
}

func (m *Manager) removePlugin(p *registeredPlugin) {
	for i, candidate := range m.plugins {
		if candidate == p {
			m.plugins = append(m.plugins[:i], m.plugins[i+1:]...)
			return
		}
	}
}

func (m *Manager) clearCooldowns(id string) {
	m.cooldownMu.Lock()
	defer m.cooldownMu.Unlock()
	for key := range m.cooldowns {
		if strings.HasPrefix(key, id+"\x00") {
			delete(m.cooldowns, key)
		}
	}
}

// lifecycleMu is held by the caller. All plugins stop admission immediately;
// after event drain, remaining stop hooks run in reverse priority order.
func (m *Manager) beginStop(run *managerRun, plugins []*registeredPlugin, ctx context.Context) {
	m.mu.Lock()
	if run.stopping {
		m.mu.Unlock()
		return
	}
	run.stopping = true
	run.cancel()
	for _, p := range plugins {
		quiesce(p.run)
	}
	m.mu.Unlock()
	go func() {
		run.wg.Wait()
		// Drain every plugin before releasing shared dependencies in Stop hooks.
		for _, p := range plugins {
			p.run.wg.Wait()
		}
		var stopErr error
		for i := len(plugins) - 1; i >= 0; i-- {
			p := plugins[i]
			m.mu.Lock()
			m.beginPluginStop(p, ctx, false)
			m.mu.Unlock()
			<-p.run.done
			stopErr = errors.Join(stopErr, p.run.stopErr)
		}
		run.stopErr = stopErr
		close(run.done)
	}()
}

func runFinished(run *managerRun) bool { return channelClosed(run.done) }

func channelClosed(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func waitForStop(ctx context.Context, run *managerRun) error {
	if err := waitDone(ctx, run.done); err != nil {
		return err
	}
	return run.stopErr
}

func waitForPluginStop(ctx context.Context, run *pluginRun) error {
	if err := waitDone(ctx, run.done); err != nil {
		return err
	}
	return run.stopErr
}

func waitDone(ctx context.Context, done <-chan struct{}) error {
	if channelClosed(done) {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func callLifecycle(fn func(context.Context) error, ctx context.Context) (err error) {
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("plugin lifecycle panicked")
		}
	}()
	return fn(ctx)
}

func (m *Manager) runTask(run *pluginRun, plugin *registeredPlugin, task registeredTask) {
	defer run.wg.Done()
	invoke := func() {
		if run.ctx.Err() != nil {
			return
		}
		taskCtx := &sdk.TaskContext{
			PluginID: plugin.descriptor.ID, Sender: senderAdapter{sender: m.sender},
			Config: m.configs.Snapshot().View(plugin.descriptor.ID, 0, 0),
		}
		if declaresStorage(plugin.descriptor.Capabilities) {
			taskCtx.Store = m.storeFor(plugin.descriptor.ID, run)
		}
		if err := callTask(task.handle, run.ctx, taskCtx); err != nil && run.ctx.Err() == nil {
			m.logger.Error("plugin task failed", "plugin", plugin.descriptor.ID, "task", task.task.ID, "error", err)
		}
	}
	if task.task.Every == 0 {
		invoke()
		return
	}
	ticker := time.NewTicker(task.task.Every)
	defer ticker.Stop()
	for {
		select {
		case <-run.ctx.Done():
			return
		case <-ticker.C:
			invoke()
		}
	}
}

func callTask(fn sdk.TaskFunc, ctx context.Context, taskCtx *sdk.TaskContext) (err error) {
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("plugin task panicked")
		}
	}()
	return fn(ctx, taskCtx)
}
