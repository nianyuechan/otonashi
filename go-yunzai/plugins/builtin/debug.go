package builtin

import (
	"context"
	"fmt"
	"strings"

	"github.com/trss-yunzai/go-yunzai/internal/config"
	"github.com/trss-yunzai/go-yunzai/internal/event"
	"github.com/trss-yunzai/go-yunzai/internal/message"
	"github.com/trss-yunzai/go-yunzai/internal/render"
)

const RenderCommand = "#render"

// maxRenderImageBytes bounds the PNG payload sent through OneBot. A 32 MiB
// image becomes roughly 44 MiB after base64 encoding.
const maxRenderImageBytes = 8 << 20

// DebugHandler 用固定文本回复私聊和群消息。
type DebugHandler struct {
	sender   message.Sender
	reply    string
	renderer render.Renderer
}

func NewDebugHandler(sender message.Sender, reply string) *DebugHandler {
	return NewDebugHandlerWithRenderer(sender, nil, reply)
}

// NewDebugHandlerWithRenderer 在保留固定文本回复的同时，启用 #render 测试命令。
func NewDebugHandlerWithRenderer(sender message.Sender, renderer render.Renderer, reply string) *DebugHandler {
	if reply == "" {
		reply = config.DefaultDebugReply
	}
	return &DebugHandler{sender: sender, reply: reply, renderer: renderer}
}

func (h *DebugHandler) HandleEvent(ctx context.Context, evt *event.Event) error {
	if evt.PostType != "message" || (evt.MessageType != "private" && evt.MessageType != "group") {
		return nil
	}

	if h.renderer != nil && isRenderCommand(evt.Message) {
		return h.handleRender(ctx, evt)
	}

	outgoing := message.Outgoing{
		MessageType: evt.MessageType,
		UserID:      evt.UserID,
		GroupID:     evt.GroupID,
		Message:     []message.Segment{message.Text(h.reply)},
	}
	if _, err := h.sender.SendMessage(ctx, outgoing); err != nil {
		return fmt.Errorf("send debug reply: %w", err)
	}
	return nil
}

func (h *DebugHandler) handleRender(ctx context.Context, evt *event.Event) error {
	outcome := h.renderer.RenderWithFallback(ctx, render.Request{
		HTML: `<!doctype html><html><head><meta charset="utf-8"><style>body{font-family:sans-serif;padding:24px}h1{color:#2f6feb}</style></head><body><h1>go-yunzai render</h1><p>render pipeline is working</p></body></html>`,
		Options: render.RenderOptions{
			Width:             640,
			Height:            360,
			DeviceScaleFactor: 1,
			WaitUntil:         "network_idle",
		},
	})

	segments := []message.Segment{message.Text(outcome.Text)}
	if outcome.Image != nil && len(outcome.Image.Data) > 0 {
		if len(outcome.Image.Data) > maxRenderImageBytes {
			segments = []message.Segment{message.Text("渲染图片超过 8 MiB，已跳过发送")}
		} else {
			segments = []message.Segment{message.ImagePNG(outcome.Image.Data)}
		}
	}
	if segments[0].Type == "text" && strings.TrimSpace(segments[0].Data["text"].(string)) == "" {
		segments = []message.Segment{message.Text("图片暂时不可用")}
	}

	if _, err := h.sender.SendMessage(ctx, message.Outgoing{
		MessageType: evt.MessageType,
		UserID:      evt.UserID,
		GroupID:     evt.GroupID,
		Message:     segments,
	}); err != nil {
		return fmt.Errorf("send render reply: %w", err)
	}
	return nil
}

func isRenderCommand(segments []message.Segment) bool {
	var text strings.Builder
	for _, segment := range segments {
		if segment.Type != "text" {
			continue
		}
		value, ok := segment.Data["text"].(string)
		if ok {
			text.WriteString(value)
		}
	}
	return strings.TrimSpace(text.String()) == RenderCommand
}
