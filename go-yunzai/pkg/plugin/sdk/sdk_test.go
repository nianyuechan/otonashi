package sdk

import (
	"context"
	"errors"
	"testing"
)

type captureSender struct {
	message Outgoing
	err     error
}

func (s *captureSender) SendMessage(_ context.Context, message Outgoing) (SendResult, error) {
	s.message = message
	return SendResult{MessageID: 42}, s.err
}

func TestReplyRoutesToCurrentConversation(t *testing.T) {
	for _, kind := range []string{"private", "group"} {
		t.Run(kind, func(t *testing.T) {
			sender := &captureSender{}
			c := &Context{Event: &Event{MessageType: kind, UserID: 11, GroupID: 22}, Sender: sender}
			result, err := c.Reply(context.Background(), Segment{Type: "text", Data: map[string]any{"text": "reply"}})
			if err != nil || result.MessageID != 42 {
				t.Fatalf("Reply = %+v, %v", result, err)
			}
			if sender.message.MessageType != kind || len(sender.message.Message) != 1 || sender.message.Message[0].Data["text"] != "reply" {
				t.Fatalf("reply = %+v", sender.message)
			}
			if (kind == "private" && (sender.message.UserID != 11 || sender.message.GroupID != 0)) || (kind == "group" && (sender.message.GroupID != 22 || sender.message.UserID != 0)) {
				t.Fatalf("reply target = %+v", sender.message)
			}
		})
	}
}

func TestReplyRejectsInvalidContextAndPreservesSenderError(t *testing.T) {
	for _, c := range []*Context{nil, {}, {Event: &Event{}}, {Event: &Event{MessageType: "private"}, Sender: &captureSender{}}, {Event: &Event{MessageType: "group"}, Sender: &captureSender{}}} {
		if _, err := c.Reply(context.Background()); err == nil {
			t.Fatal("Reply accepted invalid context")
		}
	}
	want := errors.New("adapter failure")
	c := &Context{Event: &Event{MessageType: "private", UserID: 11}, Sender: &captureSender{err: want}}
	if _, err := c.Reply(context.Background()); !errors.Is(err, want) {
		t.Fatalf("Reply lost adapter error: %v", err)
	}
}
