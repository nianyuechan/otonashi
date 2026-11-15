package builtin

import (
	"context"
	"time"

	"github.com/nianyuechan/otonashi/go-yunzai/internal/event"
	"github.com/nianyuechan/otonashi/go-yunzai/internal/message"
	"github.com/nianyuechan/otonashi/go-yunzai/internal/render"
	"github.com/nianyuechan/otonashi/go-yunzai/pkg/plugin/sdk"
)

// DebugPlugin exposes the existing M1 debug handler through the public plugin
// lifecycle and rule contract.
type DebugPlugin struct {
	handler *DebugHandler
}

// NewDebugPlugin creates the built-in debug plugin.
func NewDebugPlugin(sender message.Sender, renderer render.Renderer, reply string) sdk.Plugin {
	return &DebugPlugin{handler: NewDebugHandlerWithRenderer(sender, renderer, reply)}
}

func (p *DebugPlugin) Descriptor() sdk.Descriptor {
	return sdk.Descriptor{
		ID:           "builtin.debug",
		Version:      "0.1.0",
		Priority:     100,
		Events:       []string{"message"},
		Capabilities: []sdk.Capability{sdk.CapabilityMessages, sdk.CapabilityRender, sdk.CapabilityStorage},
	}
}

func (p *DebugPlugin) Register(router sdk.Router) error {
	if err := router.Rule(sdk.Rule{
		ID: "builtin.debug.note", Event: "message", Pattern: `(?s)^#note(?:\s|$)`,
	}, handleNote); err != nil {
		return err
	}
	if err := router.Rule(sdk.Rule{
		ID: "builtin.debug.render", Event: "message", Pattern: `^\s*#render\s*$`, Permission: sdk.PermissionMaster,
	}, func(ctx context.Context, pluginCtx *sdk.Context) (sdk.Result, error) {
		if pluginCtx == nil || pluginCtx.Event == nil {
			return sdk.Stop, nil
		}
		return sdk.Stop, p.handler.HandleEvent(ctx, fromSDKEvent(pluginCtx.Event))
	}); err != nil {
		return err
	}
	if err := router.Rule(sdk.Rule{
		ID: "builtin.debug.session", Event: "message", Pattern: `^#session$`, Permission: sdk.PermissionMaster,
	}, func(ctx context.Context, pluginCtx *sdk.Context) (sdk.Result, error) {
		if err := pluginCtx.Session.Set(30*time.Second, func(ctx context.Context, next *sdk.Context) (sdk.Result, error) {
			_, err := next.Reply(ctx, sdk.Segment{Type: "text", Data: map[string]any{"text": "[go-yunzai] 会话验证完成"}})
			return sdk.Stop, err
		}); err != nil {
			return sdk.Stop, err
		}
		_, err := pluginCtx.Reply(ctx, sdk.Segment{Type: "text", Data: map[string]any{"text": "[go-yunzai] 请在 30 秒内发送下一条消息"}})
		if err != nil {
			pluginCtx.Session.Clear()
		}
		return sdk.Stop, err
	}); err != nil {
		return err
	}
	return router.Rule(sdk.Rule{
		ID:    "builtin.debug.message",
		Event: "message",
	}, func(ctx context.Context, pluginCtx *sdk.Context) (sdk.Result, error) {
		if pluginCtx == nil || pluginCtx.Event == nil {
			return sdk.Stop, nil
		}
		return sdk.Stop, p.handler.HandleEvent(ctx, fromSDKEvent(pluginCtx.Event))
	})
}

func (p *DebugPlugin) Start(context.Context) error { return nil }

func (p *DebugPlugin) Stop(context.Context) error { return nil }

func fromSDKEvent(evt *sdk.Event) *event.Event {
	segments := make([]message.Segment, len(evt.Message))
	for i, segment := range evt.Message {
		segments[i] = message.Segment{Type: segment.Type, Data: cloneSegmentData(segment.Data)}
	}
	return &event.Event{
		Time:          evt.Time,
		SelfID:        evt.SelfID,
		PostType:      evt.PostType,
		MessageType:   evt.MessageType,
		SubType:       evt.SubType,
		MessageID:     evt.MessageID,
		UserID:        evt.UserID,
		GroupID:       evt.GroupID,
		Message:       segments,
		Sender:        event.Sender{UserID: evt.Sender.UserID, Nickname: evt.Sender.Nickname, Card: evt.Sender.Card, Sex: evt.Sender.Sex, Age: evt.Sender.Age, Role: evt.Sender.Role},
		NoticeType:    evt.NoticeType,
		RequestType:   evt.RequestType,
		MetaEventType: evt.MetaEventType,
		TargetID:      evt.TargetID,
		OperatorID:    evt.OperatorID,
		Flag:          evt.Flag,
		Comment:       evt.Comment,
		Interval:      evt.Interval,
		Status:        cloneSegmentData(evt.Status),
	}
}

func cloneSegmentData(data map[string]any) map[string]any {
	if data == nil {
		return nil
	}
	clone := make(map[string]any, len(data))
	for key, value := range data {
		clone[key] = value
	}
	return clone
}
