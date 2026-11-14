package plugin

import (
	"context"
	"fmt"

	"github.com/trss-yunzai/go-yunzai/internal/event"
	"github.com/trss-yunzai/go-yunzai/internal/pluginconfig"
	"github.com/trss-yunzai/go-yunzai/pkg/plugin/sdk"
)

type eventPlugin struct {
	plugin *registeredPlugin
	run    *pluginRun
}

// HandleEvent runs sessions, accepts, then ordinary rules. Each invocation is
// admitted against its plugin generation, so unloading also invalidates event
// snapshots captured before the unload began.
func (m *Manager) HandleEvent(ctx context.Context, evt *event.Event) error {
	if evt == nil {
		return nil
	}
	m.mu.RLock()
	run := m.run
	if run == nil || !run.ready || run.stopping || run.ctx.Err() != nil {
		m.mu.RUnlock()
		return nil
	}
	run.wg.Add(1)
	plugins := make([]eventPlugin, 0, len(m.plugins))
	for _, p := range m.plugins {
		if p.run != nil && p.run.ready && !p.run.stopping && descriptorMatches(p.descriptor, evt) {
			plugins = append(plugins, eventPlugin{plugin: p, run: p.run})
		}
	}
	m.mu.RUnlock()
	defer run.wg.Done()
	ctx, cancel := context.WithCancel(ctx)
	stopCancel := context.AfterFunc(run.ctx, cancel)
	defer stopCancel()
	defer cancel()
	if !m.policy.allowsEvent(evt) {
		return nil
	}
	permissions := m.policy.permissions(evt)
	settings := m.configs.Snapshot()
	var firstErr error
	record := func(p eventPlugin, stage string, err error) {
		if err == nil || p.run.ctx.Err() != nil {
			return
		}
		m.logger.Error("plugin handler failed", "plugin", p.plugin.descriptor.ID, "stage", stage, "error", err)
		if firstErr == nil {
			firstErr = fmt.Errorf("plugin %q %s: %w", p.plugin.descriptor.ID, stage, err)
		}
	}
	skipped := make(map[*pluginRun]bool)
	if evt.PostType == "message" {
		for _, p := range plugins {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			m.withPlugin(ctx, run, p, evt, permissions, settings, func(ctx context.Context, c *sdk.Context) {
				session := c.Session.(scopedSession)
				if continuation := p.run.sessions.take(session.key); continuation != nil {
					result, err := callHandler(continuation, ctx, c)
					record(p, "session", err)
					skipped[p.run] = err == nil && result == sdk.Stop
				}
			})
		}
	}
	for _, p := range plugins {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if skipped[p.run] || p.plugin.accept == nil {
			continue
		}
		decision := sdk.AcceptNextPlugin
		m.withPlugin(ctx, run, p, evt, permissions, settings, func(ctx context.Context, c *sdk.Context) {
			result, err := callAccept(p.plugin.accept, ctx, c)
			record(p, "accept", err)
			if err == nil && ctx.Err() == nil {
				decision = result
			}
		})
		if decision == sdk.AcceptReturn {
			return firstErr
		}
		if decision == sdk.AcceptBreak {
			break
		}
	}
	for _, p := range plugins {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if skipped[p.run] {
			continue
		}
		denied := false
		m.withPlugin(ctx, run, p, evt, permissions, settings, func(ctx context.Context, c *sdk.Context) {
			for _, rule := range p.plugin.rules {
				if ctx.Err() != nil {
					return
				}
				if !matchesEvent(rule.rule.Event, evt) || !matchesPattern(rule.pattern, c.Event) {
					continue
				}
				if text := permissionDenial(rule.rule.Permission, evt, permissions); text != "" {
					denied = true
					_, err := c.Reply(ctx, sdk.Segment{Type: "text", Data: map[string]any{"text": text}})
					record(p, "permission reply", err)
					return
				}
				cooldown := rule.rule.Cooldown
				if cooldown <= 0 {
					cooldown = m.policy.cooldown
				}
				if cooldown > 0 && !m.takeCooldown(p.plugin.descriptor.ID, rule.rule.ID, evt, cooldown) {
					continue
				}
				result, err := callHandler(rule.handle, ctx, c)
				record(p, "rule "+rule.rule.ID, err)
				if result == sdk.Stop {
					return
				}
			}
		})
		// The legacy permission check replies once and terminates the event.
		if denied {
			return firstErr
		}
	}
	return firstErr
}

func (m *Manager) withPlugin(ctx context.Context, run *managerRun, p eventPlugin, evt *event.Event, permissions sdk.Permissions, settings *pluginconfig.Snapshot, fn func(context.Context, *sdk.Context)) {
	m.mu.RLock()
	pr := p.run
	if m.run != run || run.stopping || p.plugin.run != pr || !pr.ready || pr.stopping || pr.ctx.Err() != nil {
		m.mu.RUnlock()
		return
	}
	pr.wg.Add(1)
	m.mu.RUnlock()
	defer pr.wg.Done()
	ctx, cancel := context.WithCancel(ctx)
	stopCancel := context.AfterFunc(pr.ctx, cancel)
	defer stopCancel()
	defer cancel()
	if pr.ctx.Err() != nil {
		cancel()
	}
	if ctx.Err() != nil {
		return
	}
	publicEvent := toSDKEvent(evt)
	fn(ctx, &sdk.Context{
		Event: publicEvent, Runtime: runtimeContext(evt), Permissions: permissions,
		Sender: senderAdapter{sender: m.sender}, Session: pr.sessions.scope(p.plugin.descriptor.ID, publicEvent),
		Store: m.storeFor(p.plugin.descriptor.ID, pr), Config: settings.View(p.plugin.descriptor.ID, evt.SelfID, evt.GroupID),
	})
}

func descriptorMatches(d sdk.Descriptor, evt *event.Event) bool {
	if len(d.Events) == 0 {
		return true
	}
	for _, kind := range d.Events {
		if matchesEvent(kind, evt) {
			return true
		}
	}
	return false
}

func callHandler(fn sdk.HandlerFunc, ctx context.Context, c *sdk.Context) (result sdk.Result, err error) {
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("plugin handler panicked")
		}
	}()
	return fn(ctx, c)
}

func callAccept(fn sdk.AcceptFunc, ctx context.Context, c *sdk.Context) (result sdk.AcceptDecision, err error) {
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("plugin accept panicked")
		}
	}()
	result, err = fn(ctx, c)
	if err == nil && result > sdk.AcceptReturn {
		err = fmt.Errorf("invalid accept decision")
	}
	return result, err
}
