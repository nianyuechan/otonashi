package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/trss-yunzai/go-yunzai/pkg/plugin/sdk"
)

// handleNote proves durable state using one note per account/conversation/user.
// Values are never included in operational errors or success acknowledgments.
func handleNote(ctx context.Context, c *sdk.Context) (sdk.Result, error) {
	if c.Event.MessageType != "private" && c.Event.MessageType != "group" {
		return sdk.Stop, nil
	}
	var text strings.Builder
	for _, segment := range c.Event.Message {
		if segment.Type == "text" {
			if value, ok := segment.Data["text"].(string); ok {
				text.WriteString(value)
			}
		}
	}
	command := strings.TrimSpace(strings.TrimPrefix(text.String(), "#note"))
	key := fmt.Sprintf("note:a:%d:t:%s:g:%d:u:%d", c.Runtime.AccountID, c.Runtime.MessageType, c.Runtime.GroupID, c.Runtime.UserID)
	var err error
	reply := "用法：#note set 内容 / #note / #note delete"
	if c.Store == nil {
		err = sdk.ErrStoreUnavailable
	} else {
		switch {
		case command == "":
			var value []byte
			value, err = c.Store.Get(ctx, key)
			if errors.Is(err, sdk.ErrKeyNotFound) {
				err, reply = nil, "还没有记事"
			} else if err == nil {
				reply = string(value)
			}
		case command == "delete":
			err = c.Store.Delete(ctx, key)
			reply = "记事已删除"
		case strings.HasPrefix(command, "set "):
			value := strings.TrimSpace(strings.TrimPrefix(command, "set "))
			limit := 4096
			if c.Config != nil {
				if raw, ok := c.Config.Get("note_max_bytes"); ok {
					var configured int
					if json.Unmarshal(raw, &configured) == nil && configured > 0 && configured <= 4096 {
						limit = configured
					}
				}
			}
			if value == "" || len(value) > limit {
				reply = fmt.Sprintf("记事内容须为 1 至 %d 字节", limit)
			} else {
				err = c.Store.Set(ctx, key, []byte(value), 0)
				reply = "记事已保存，重启后仍可读取"
			}
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			return sdk.Stop, ctx.Err()
		}
		_, replyErr := c.Reply(ctx, sdk.Segment{Type: "text", Data: map[string]any{"text": "记事存储暂时不可用，请稍后重试"}})
		return sdk.Stop, errors.Join(fmt.Errorf("note storage: %w", err), replyErr)
	}
	_, err = c.Reply(ctx, sdk.Segment{Type: "text", Data: map[string]any{"text": reply}})
	return sdk.Stop, err
}
