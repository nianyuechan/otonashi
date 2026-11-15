package event

import (
	"context"

	"github.com/nianyuechan/otonashi/go-yunzai/internal/message"
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

// Sender 字段必须带 json 标签：OneBot 载荷使用 snake_case（sender.user_id），
// 无标签时编码/json 按字段名匹配不上，sender.user_id/role 等会永远解码为
// 零值（2026-11-15 由 Z2 真实写入基准采证：群内按用户隔离全部失效）。
type Sender struct {
	UserID   int64  `json:"user_id"`
	Nickname string `json:"nickname"`
	Card     string `json:"card"`
	Sex      string `json:"sex"`
	Age      int    `json:"age"`
	Role     string `json:"role"`
}

type Handler interface {
	HandleEvent(context.Context, *Event) error
}

type HandlerFunc func(context.Context, *Event) error

func (f HandlerFunc) HandleEvent(ctx context.Context, event *Event) error {
	return f(ctx, event)
}
