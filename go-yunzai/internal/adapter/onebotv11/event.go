package onebotv11

import (
	"encoding/json"
	"fmt"

	"github.com/trss-yunzai/go-yunzai/internal/event"
	"github.com/trss-yunzai/go-yunzai/internal/message"
)

type wireEvent struct {
	Time          int64           `json:"time"`
	SelfID        int64           `json:"self_id"`
	PostType      string          `json:"post_type"`
	MessageType   string          `json:"message_type"`
	SubType       string          `json:"sub_type"`
	MessageID     int64           `json:"message_id"`
	UserID        int64           `json:"user_id"`
	GroupID       int64           `json:"group_id"`
	Message       json.RawMessage `json:"message"`
	Sender        event.Sender    `json:"sender"`
	NoticeType    string          `json:"notice_type"`
	RequestType   string          `json:"request_type"`
	MetaEventType string          `json:"meta_event_type"`
	TargetID      int64           `json:"target_id"`
	OperatorID    int64           `json:"operator_id"`
	Flag          string          `json:"flag"`
	Comment       string          `json:"comment"`
	Interval      int64           `json:"interval"`
	Status        map[string]any  `json:"status"`
}

func DecodeEvent(data []byte) (*event.Event, error) {
	var wire wireEvent
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, fmt.Errorf("decode OneBot event: %w", err)
	}
	if wire.PostType == "" {
		return nil, fmt.Errorf("decode OneBot event: post_type is required")
	}

	evt := &event.Event{
		Time:          wire.Time,
		SelfID:        wire.SelfID,
		PostType:      wire.PostType,
		MessageType:   wire.MessageType,
		SubType:       wire.SubType,
		MessageID:     wire.MessageID,
		UserID:        wire.UserID,
		GroupID:       wire.GroupID,
		Sender:        wire.Sender,
		NoticeType:    wire.NoticeType,
		RequestType:   wire.RequestType,
		MetaEventType: wire.MetaEventType,
		TargetID:      wire.TargetID,
		OperatorID:    wire.OperatorID,
		Flag:          wire.Flag,
		Comment:       wire.Comment,
		Interval:      wire.Interval,
		Status:        wire.Status,
	}
	if wire.PostType == "message" {
		if len(wire.Message) == 0 {
			return nil, fmt.Errorf("decode OneBot message: array message is required")
		}
		var segments []message.Segment
		if err := json.Unmarshal(wire.Message, &segments); err != nil {
			return nil, fmt.Errorf("decode OneBot array message: %w", err)
		}
		evt.Message = segments
	}
	return evt, nil
}
