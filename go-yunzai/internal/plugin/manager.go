package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/trss-yunzai/go-yunzai/internal/event"
	"github.com/trss-yunzai/go-yunzai/internal/message"
	"github.com/trss-yunzai/go-yunzai/internal/pluginconfig"
	"github.com/trss-yunzai/go-yunzai/pkg/plugin/sdk"
)

// Manager owns plugin registration, lifecycle, and rule dispatch.
type Manager struct {
	lifecycleMu sync.Mutex
	mu          sync.RWMutex
	plugins     []*registeredPlugin
	sender      message.Sender
	logger      *slog.Logger
	run         *managerRun
	order       uint64
	policy      policyState
	stores      StoreProvider
	configs     *pluginconfig.Registry
	now         func() time.Time
	cooldownMu  sync.Mutex
	cooldowns   map[string]time.Time
}

// Options configures plugin admission and event filtering.
type Options struct {
	Policy sdk.Policy
	Now    func() time.Time
	Stores StoreProvider
	Config *pluginconfig.Registry
}

type policyState struct {
	masters             map[int64]map[int64]struct{}
	allowedCapabilities map[sdk.Capability]struct{}
	allowUsers          map[int64]struct{}
	denyUsers           map[int64]struct{}
	allowGroups         map[int64]struct{}
	denyGroups          map[int64]struct{}
	cooldown            time.Duration
}

type registeredPlugin struct {
	plugin     sdk.Plugin
	descriptor sdk.Descriptor
	rules      []registeredRule
	tasks      []registeredTask
	order      uint64
	accept     sdk.AcceptFunc
	run        *pluginRun
}

type registeredTask struct {
	task   sdk.Task
	handle sdk.TaskFunc
}

type registeredRule struct {
	rule    sdk.Rule
	pattern *regexp.Regexp
	handle  sdk.HandlerFunc
}

// New creates a plugin manager backed by the given message sender.
func New(sender message.Sender, logger *slog.Logger) *Manager {
	return NewWithOptions(sender, logger, Options{})
}

// NewWithOptions creates a manager with plugin policy and deterministic clock
// injection for cooldown tests.
func NewWithOptions(sender message.Sender, logger *slog.Logger, options Options) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	configs := options.Config
	if configs == nil {
		configs, _ = pluginconfig.New(nil)
	}
	return &Manager{
		sender:    sender,
		logger:    logger,
		policy:    newPolicyState(options.Policy),
		stores:    options.Stores,
		configs:   configs,
		now:       now,
		cooldowns: make(map[string]time.Time),
	}
}

// Register validates and registers a plugin before the event stream starts.
func (m *Manager) Register(p sdk.Plugin) error {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	m.mu.RLock()
	running := m.run != nil && !runFinished(m.run)
	m.mu.RUnlock()
	if running {
		return fmt.Errorf("plugin manager is running or stopping")
	}
	registered, err := m.prepare(p)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.plugins = append(m.plugins, registered)
	m.sortPlugins()
	return nil
}

// prepare is serialized by lifecycleMu. Plugin callbacks run without mu so
// existing event dispatch is unaffected by a slow registration callback.
func (m *Manager) prepare(p sdk.Plugin) (registered *registeredPlugin, err error) {
	defer func() {
		if recover() != nil {
			registered, err = nil, fmt.Errorf("plugin registration panicked")
		}
	}()
	if p == nil {
		return nil, fmt.Errorf("plugin is nil")
	}
	descriptor := p.Descriptor()
	if strings.TrimSpace(descriptor.ID) == "" {
		return nil, fmt.Errorf("plugin id is required")
	}
	if strings.TrimSpace(descriptor.Version) == "" {
		return nil, fmt.Errorf("plugin %q version is required", descriptor.ID)
	}
	if err := m.policy.validateCapabilities(descriptor.Capabilities); err != nil {
		return nil, fmt.Errorf("plugin %q: %w", descriptor.ID, err)
	}

	m.mu.RLock()
	for _, existing := range m.plugins {
		if existing.descriptor.ID == descriptor.ID {
			m.mu.RUnlock()
			return nil, fmt.Errorf("plugin %q is already registered", descriptor.ID)
		}
	}
	m.mu.RUnlock()

	descriptor.Events = append([]string(nil), descriptor.Events...)
	descriptor.Capabilities = append([]sdk.Capability(nil), descriptor.Capabilities...)
	registered = &registeredPlugin{plugin: p, descriptor: descriptor, order: m.order}
	m.order++
	router := &pluginRouter{rules: &registered.rules, tasks: &registered.tasks, accept: &registered.accept}
	if err := p.Register(router); err != nil {
		return nil, fmt.Errorf("register plugin %q: %w", descriptor.ID, err)
	}
	return registered, nil
}

// sortPlugins is called with mu held.
func (m *Manager) sortPlugins() {
	sort.SliceStable(m.plugins, func(i, j int) bool {
		if m.plugins[i].descriptor.Priority != m.plugins[j].descriptor.Priority {
			return m.plugins[i].descriptor.Priority < m.plugins[j].descriptor.Priority
		}
		return m.plugins[i].order < m.plugins[j].order
	})
}

func newPolicyState(policy sdk.Policy) policyState {
	masters := make(map[int64]map[int64]struct{}, len(policy.Masters))
	for account, users := range policy.Masters {
		masters[account] = toIntSet(users)
	}
	return policyState{
		masters:             masters,
		allowedCapabilities: toSet(policy.AllowedCapabilities),
		allowUsers:          toIntSet(policy.AllowUsers),
		denyUsers:           toIntSet(policy.DenyUsers),
		allowGroups:         toIntSet(policy.AllowGroups),
		denyGroups:          toIntSet(policy.DenyGroups),
		cooldown:            policy.Cooldown,
	}
}

func toSet(values []sdk.Capability) map[sdk.Capability]struct{} {
	result := make(map[sdk.Capability]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func toIntSet(values []int64) map[int64]struct{} {
	result := make(map[int64]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func (p policyState) validateCapabilities(capabilities []sdk.Capability) error {
	if len(p.allowedCapabilities) == 0 {
		return nil
	}
	for _, capability := range capabilities {
		if _, ok := p.allowedCapabilities[capability]; !ok {
			return fmt.Errorf("capability %q is not allowed by policy", capability)
		}
	}
	return nil
}

func (p policyState) allowsEvent(evt *event.Event) bool {
	if _, denied := p.denyUsers[evt.UserID]; denied && evt.UserID != 0 {
		return false
	}
	if evt.GroupID != 0 {
		if _, denied := p.denyGroups[evt.GroupID]; denied {
			return false
		}
	}
	if len(p.allowUsers) > 0 {
		if _, allowed := p.allowUsers[evt.UserID]; !allowed {
			return false
		}
	}
	if len(p.allowGroups) > 0 {
		if _, allowed := p.allowGroups[evt.GroupID]; !allowed {
			return false
		}
	}
	return true
}

func (m *Manager) takeCooldown(pluginID, ruleID string, evt *event.Event, duration time.Duration) bool {
	subject := fmt.Sprintf("a:%d:u:%d", evt.SelfID, evt.UserID)
	if evt.GroupID != 0 {
		subject = fmt.Sprintf("a:%d:g:%d:u:%d", evt.SelfID, evt.GroupID, evt.UserID)
	}
	key := pluginID + "\x00" + ruleID + "\x00" + subject
	now := m.now()
	m.cooldownMu.Lock()
	defer m.cooldownMu.Unlock()
	if until, ok := m.cooldowns[key]; ok && until.After(now) {
		return false
	}
	m.cooldowns[key] = now.Add(duration)
	return true
}

type pluginRouter struct {
	rules  *[]registeredRule
	tasks  *[]registeredTask
	accept *sdk.AcceptFunc
}

func (r *pluginRouter) Accept(handler sdk.AcceptFunc) error {
	if handler == nil || *r.accept != nil {
		return fmt.Errorf("accept requires one non-nil handler per plugin")
	}
	*r.accept = handler
	return nil
}

func (r *pluginRouter) Task(task sdk.Task, handler sdk.TaskFunc) error {
	if strings.TrimSpace(task.ID) == "" || task.Every < 0 || handler == nil {
		return fmt.Errorf("task requires an id, non-negative interval, and handler")
	}
	for _, existing := range *r.tasks {
		if existing.task.ID == task.ID {
			return fmt.Errorf("task %q is already registered", task.ID)
		}
	}
	*r.tasks = append(*r.tasks, registeredTask{task: task, handle: handler})
	return nil
}

func (r *pluginRouter) Rule(rule sdk.Rule, handler sdk.HandlerFunc) error {
	switch rule.Permission {
	case "", sdk.PermissionAll, sdk.PermissionMaster, sdk.PermissionOwner, sdk.PermissionAdmin:
	default:
		return fmt.Errorf("rule %q has unknown permission %q", rule.ID, rule.Permission)
	}
	if strings.TrimSpace(rule.ID) == "" {
		return fmt.Errorf("rule id is required")
	}
	if strings.TrimSpace(rule.Event) == "" {
		return fmt.Errorf("rule %q event is required", rule.ID)
	}
	if handler == nil {
		return fmt.Errorf("rule %q handler is nil", rule.ID)
	}
	for _, existing := range *r.rules {
		if existing.rule.ID == rule.ID {
			return fmt.Errorf("rule %q is already registered", rule.ID)
		}
	}
	var pattern *regexp.Regexp
	if rule.Pattern != "" {
		compiled, err := regexp.Compile(rule.Pattern)
		if err != nil {
			return fmt.Errorf("compile rule %q pattern: %w", rule.ID, err)
		}
		pattern = compiled
	}
	*r.rules = append(*r.rules, registeredRule{rule: rule, pattern: pattern, handle: handler})
	return nil
}

func matchesEvent(ruleEvent string, evt *event.Event) bool {
	if ruleEvent == "" {
		return true
	}
	if ruleEvent == evt.PostType {
		return true
	}
	return ruleEvent == evt.PostType+"."+evt.MessageType
}

func matchesPattern(pattern *regexp.Regexp, evt *sdk.Event) bool {
	if pattern == nil {
		return true
	}
	var text strings.Builder
	for _, segment := range evt.Message {
		if segment.Type != "text" {
			continue
		}
		if value, ok := segment.Data["text"].(string); ok {
			text.WriteString(value)
		}
	}
	return pattern.MatchString(text.String())
}

type senderAdapter struct {
	sender message.Sender
}

func (s senderAdapter) SendMessage(ctx context.Context, outgoing sdk.Outgoing) (sdk.SendResult, error) {
	if s.sender == nil {
		return sdk.SendResult{}, fmt.Errorf("message sender is unavailable")
	}
	segments := make([]message.Segment, len(outgoing.Message))
	for i, segment := range outgoing.Message {
		segments[i] = message.Segment{Type: segment.Type, Data: segment.Data}
	}
	result, err := s.sender.SendMessage(ctx, message.Outgoing{
		MessageType: outgoing.MessageType,
		UserID:      outgoing.UserID,
		GroupID:     outgoing.GroupID,
		Message:     segments,
	})
	if err != nil {
		return sdk.SendResult{}, err
	}
	return sdk.SendResult{MessageID: result.MessageID}, nil
}

func toSDKEvent(evt *event.Event) *sdk.Event {
	segments := make([]sdk.Segment, len(evt.Message))
	for i, segment := range evt.Message {
		segments[i] = sdk.Segment{Type: segment.Type, Data: segment.Data}
	}
	return &sdk.Event{
		Time:          evt.Time,
		SelfID:        evt.SelfID,
		PostType:      evt.PostType,
		MessageType:   evt.MessageType,
		SubType:       evt.SubType,
		MessageID:     evt.MessageID,
		UserID:        evt.UserID,
		GroupID:       evt.GroupID,
		Message:       segments,
		Sender:        sdk.SenderInfo{UserID: evt.Sender.UserID, Nickname: evt.Sender.Nickname, Card: evt.Sender.Card, Sex: evt.Sender.Sex, Age: evt.Sender.Age, Role: evt.Sender.Role},
		NoticeType:    evt.NoticeType,
		RequestType:   evt.RequestType,
		MetaEventType: evt.MetaEventType,
		TargetID:      evt.TargetID,
		OperatorID:    evt.OperatorID,
		Flag:          evt.Flag,
		Comment:       evt.Comment,
		Interval:      evt.Interval,
		Status:        evt.Status,
	}
}

func runtimeContext(evt *event.Event) sdk.RuntimeContext {
	return sdk.RuntimeContext{
		AccountID:   evt.SelfID,
		UserID:      evt.UserID,
		GroupID:     evt.GroupID,
		MessageID:   evt.MessageID,
		MessageType: evt.MessageType,
		SubType:     evt.SubType,
	}
}
