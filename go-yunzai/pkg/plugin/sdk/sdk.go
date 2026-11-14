// Package sdk contains the stable contract exposed to Go plugins.
package sdk

import (
	"context"
	"errors"
	"time"
)

var ErrNotRunning = errors.New("plugin runtime is not running")

// Capability is a named permission requested by a plugin.
type Capability string

const (
	CapabilityMessages Capability = "messages"
	CapabilityRender   Capability = "render"
)

// Policy controls which plugin capabilities and event subjects may run.
// Empty allow lists mean unrestricted access; deny lists take precedence.
type Policy struct {
	// Masters maps an account id to its trusted owner user ids.
	Masters             map[int64][]int64
	AllowedCapabilities []Capability
	AllowUsers          []int64
	DenyUsers           []int64
	AllowGroups         []int64
	DenyGroups          []int64
	Cooldown            time.Duration
}

// Descriptor identifies a plugin and controls its dispatch order.
type Descriptor struct {
	ID           string
	Version      string
	Priority     int
	Events       []string
	Capabilities []Capability
}

// Plugin is the lifecycle contract for an in-process plugin.
type Plugin interface {
	Descriptor() Descriptor
	Register(Router) error
	Start(context.Context) error
	Stop(context.Context) error
}

// Router collects rules, an optional accept hook, and tasks during Register.
type Router interface {
	Rule(Rule, HandlerFunc) error
	Accept(AcceptFunc) error
	Task(Task, TaskFunc) error
}

// Task runs once in the background when Every is zero, or at the given
// interval. The first periodic invocation runs after one interval. Each task
// runs serially and must return when its context is canceled.
type Task struct {
	ID    string
	Every time.Duration
}

type TaskFunc func(context.Context, *TaskContext) error

type TaskContext struct {
	PluginID string
	Sender   Sender
	Store    KVStore
	Config   ConfigView
}

// RegexEngine identifies the regular expression implementation used by a rule.
type RegexEngine uint8

const (
	RegexRE2 RegexEngine = iota
)

// Rule describes one event matching rule.
type Rule struct {
	ID         string
	Event      string
	Pattern    string
	Engine     RegexEngine
	Cooldown   time.Duration
	Permission Permission
}

// Result controls whether the next rule in the current plugin is evaluated.
type Result uint8

const (
	Stop Result = iota
	Next
)

// HandlerFunc handles a matched rule.
type HandlerFunc func(context.Context, *Context) (Result, error)

// Context contains the event and capabilities available to a handler.
type Context struct {
	Event       *Event
	Sender      Sender
	Runtime     RuntimeContext
	Session     Session
	Permissions Permissions
	Store       KVStore
	Config      ConfigView
}

// Session registers a one-shot continuation for the next message from the
// same account, user, message type, and group, scoped to this plugin. Set
// replaces an existing continuation; timeouts must be positive. A continuation
// runs before accepts and rules: Stop skips this plugin's accept and rules,
// Next falls through. Other plugins still receive the event.
// Expired sessions and all sessions at shutdown are discarded.
type Session interface {
	Set(timeout time.Duration, handler HandlerFunc) error
	Clear()
}

// Reply sends a message to the current private conversation or group.
func (c *Context) Reply(ctx context.Context, segments ...Segment) (SendResult, error) {
	if c == nil || c.Event == nil || c.Sender == nil {
		return SendResult{}, errors.New("reply requires an event and message sender")
	}
	message := Outgoing{MessageType: c.Event.MessageType, Message: segments}
	switch c.Event.MessageType {
	case "private":
		if c.Event.UserID <= 0 {
			return SendResult{}, errors.New("reply requires a positive user id")
		}
		message.UserID = c.Event.UserID
	case "group":
		if c.Event.GroupID <= 0 {
			return SendResult{}, errors.New("reply requires a positive group id")
		}
		message.GroupID = c.Event.GroupID
	default:
		return SendResult{}, errors.New("reply requires a private or group message")
	}
	return c.Sender.SendMessage(ctx, message)
}

// RuntimeContext provides stable account, subject, and message identifiers
// without requiring plugins to reconstruct them from protocol fields.
type RuntimeContext struct {
	AccountID   int64
	UserID      int64
	GroupID     int64
	MessageID   int64
	MessageType string
	SubType     string
}

// Event is the public, protocol-neutral event shape exposed to plugins.
type Event struct {
	Time          int64
	SelfID        int64
	PostType      string
	MessageType   string
	SubType       string
	MessageID     int64
	UserID        int64
	GroupID       int64
	Message       []Segment
	Sender        SenderInfo
	NoticeType    string
	RequestType   string
	MetaEventType string
	TargetID      int64
	OperatorID    int64
	Flag          string
	Comment       string
	Interval      int64
	Status        map[string]any
}

// SenderInfo contains the sender fields common to message events.
type SenderInfo struct {
	UserID   int64
	Nickname string
	Card     string
	Sex      string
	Age      int
	Role     string
}

// Segment is an open message segment. Unknown segment types remain available.
type Segment struct {
	Type string
	Data map[string]any
}

// Outgoing is a message sent by a plugin.
type Outgoing struct {
	MessageType string
	UserID      int64
	GroupID     int64
	Message     []Segment
}

// SendResult contains the protocol message identifier returned by the adapter.
type SendResult struct {
	MessageID int64
}

// Sender sends a message through the configured adapter.
type Sender interface {
	SendMessage(context.Context, Outgoing) (SendResult, error)
}

// Clock is reserved for deterministic time injection in later SDK revisions.
type Clock interface {
	Now() time.Time
}
