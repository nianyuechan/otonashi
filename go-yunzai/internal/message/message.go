package message

import (
	"context"
	"encoding/base64"
)

// Segment 是 OneBot array 消息段。Data 保持开放，
// 以保留协议端未来新增的消息段类型。
type Segment struct {
	Type string         `json:"type" yaml:"type"`
	Data map[string]any `json:"data" yaml:"data"`
}

func Text(text string) Segment {
	return Segment{Type: "text", Data: map[string]any{"text": text}}
}

// ImagePNG 将 PNG 字节编码成 OneBot v11 array 消息段。
// OneBot 的图片 file 字段使用 base64:// 前缀标识内联内容。
func ImagePNG(data []byte) Segment {
	return Segment{
		Type: "image",
		Data: map[string]any{
			"file": "base64://" + base64.StdEncoding.EncodeToString(data),
		},
	}
}

// Outgoing 描述 M1 支持的单个 send_msg 动作。
type Outgoing struct {
	MessageType string
	UserID      int64
	GroupID     int64
	Message     []Segment
}

type Result struct {
	MessageID int64
}

// Sender 由核心适配器实现，使插件无需接触 WS 连接和 API 响应细节。
type Sender interface {
	SendMessage(context.Context, Outgoing) (Result, error)
}
