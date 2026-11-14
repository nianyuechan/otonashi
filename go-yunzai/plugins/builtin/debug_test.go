package builtin

import (
	"context"
	"errors"
	"testing"

	"github.com/trss-yunzai/go-yunzai/internal/event"
	"github.com/trss-yunzai/go-yunzai/internal/message"
	"github.com/trss-yunzai/go-yunzai/internal/render"
)

type captureSender struct {
	outgoing message.Outgoing
	err      error
}

func (s *captureSender) SendMessage(_ context.Context, outgoing message.Outgoing) (message.Result, error) {
	s.outgoing = outgoing
	return message.Result{}, s.err
}

type fakeRenderer struct {
	outcome render.Outcome
	request render.Request
}

func (r *fakeRenderer) RenderWithFallback(_ context.Context, request render.Request) render.Outcome {
	r.request = request
	return r.outcome
}

func TestDebugHandlerRenderCommandSendsImage(t *testing.T) {
	sender := &captureSender{}
	renderer := &fakeRenderer{outcome: render.Outcome{
		Image: &render.Image{Data: []byte("png-data")},
	}}
	handler := NewDebugHandlerWithRenderer(sender, renderer, "固定回复")

	err := handler.HandleEvent(context.Background(), &event.Event{
		PostType:    "message",
		MessageType: "group",
		GroupID:     2001,
		Message:     []message.Segment{message.Text(" #render ")},
	})
	if err != nil {
		t.Fatalf("HandleEvent() error = %v", err)
	}
	if renderer.request.Options.Width != 640 || renderer.request.Options.Height != 360 {
		t.Fatalf("unexpected render request: %+v", renderer.request)
	}
	if sender.outgoing.MessageType != "group" || sender.outgoing.GroupID != 2001 {
		t.Fatalf("unexpected outgoing target: %+v", sender.outgoing)
	}
	if len(sender.outgoing.Message) != 1 || sender.outgoing.Message[0].Type != "image" {
		t.Fatalf("unexpected outgoing message: %+v", sender.outgoing.Message)
	}
	if got := sender.outgoing.Message[0].Data["file"]; got != "base64://cG5nLWRhdGE=" {
		t.Fatalf("unexpected image file: %#v", got)
	}
}

func TestDebugHandlerRenderFailureSendsFallbackText(t *testing.T) {
	sender := &captureSender{}
	renderer := &fakeRenderer{outcome: render.Outcome{
		Text: "图片暂时不可用：渲染超时，请稍后重试。",
		Err:  errors.New("timeout"),
	}}
	handler := NewDebugHandlerWithRenderer(sender, renderer, "固定回复")

	if err := handler.HandleEvent(context.Background(), &event.Event{
		PostType:    "message",
		MessageType: "private",
		UserID:      1001,
		Message:     []message.Segment{message.Text(RenderCommand)},
	}); err != nil {
		t.Fatalf("HandleEvent() error = %v", err)
	}
	if len(sender.outgoing.Message) != 1 || sender.outgoing.Message[0].Type != "text" {
		t.Fatalf("unexpected fallback message: %+v", sender.outgoing.Message)
	}
	if sender.outgoing.Message[0].Data["text"] != renderer.outcome.Text {
		t.Fatalf("fallback text = %#v, want %q", sender.outgoing.Message[0].Data["text"], renderer.outcome.Text)
	}
}

func TestDebugHandlerNormalMessageKeepsFixedReply(t *testing.T) {
	sender := &captureSender{}
	renderer := &fakeRenderer{outcome: render.Outcome{Image: &render.Image{Data: []byte("unused")}}}
	handler := NewDebugHandlerWithRenderer(sender, renderer, "固定回复")

	if err := handler.HandleEvent(context.Background(), &event.Event{
		PostType:    "message",
		MessageType: "private",
		UserID:      1001,
		Message:     []message.Segment{message.Text("普通消息")},
	}); err != nil {
		t.Fatalf("HandleEvent() error = %v", err)
	}
	if len(sender.outgoing.Message) != 1 || sender.outgoing.Message[0].Type != "text" || sender.outgoing.Message[0].Data["text"] != "固定回复" {
		t.Fatalf("unexpected fixed reply: %+v", sender.outgoing.Message)
	}
	if renderer.request.HTML != "" {
		t.Fatal("renderer was called for a normal message")
	}
}
