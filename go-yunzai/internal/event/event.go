package event

import (
	"context"

	"github.com/trss-yunzai/go-yunzai/internal/message"
)

// Event 是 M1 各类 OneBot v11 事件的规范化表示。
type Event struct {
	Time          int64
	SelfID        int64
	PostType      string
	MessageType   string
	SubType       string
	MessageID     int64
	UserID        int64
	GroupID       int64
	Message       []message.Segment
	Sender        Sender
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

type Sender struct {
	UserID   int64
	Nickname string
	Card     string
	Sex      string
	Age      int
	Role     string
}

type Handler interface {
	HandleEvent(context.Context, *Event) error
}

type HandlerFunc func(context.Context, *Event) error

func (f HandlerFunc) HandleEvent(ctx context.Context, event *Event) error {
	return f(ctx, event)
}
