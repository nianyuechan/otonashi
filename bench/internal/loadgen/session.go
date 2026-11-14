package loadgen

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// session 负责一条 WebSocket 连接上的协议交互：接收被测机器人的动作请求并
// 应答，推送消息事件，并把回复动作与推送事件配对成延迟样本。
type session struct {
	cfg   Config
	conn  *websocket.Conn
	log   *slog.Logger
	gen   *generator
	msgID int64

	mu          sync.Mutex
	events      []EventRecord
	sentIndex   map[int64]int // seq -> events 下标
	sentAt      map[int64]int64
	actionCount int
	actionErrs  int
	writeMu     sync.Mutex
}

func newSession(cfg Config, conn *websocket.Conn, log *slog.Logger) *session {
	return &session{
		cfg: cfg, conn: conn, log: log, gen: newGenerator(cfg.Workload),
		sentIndex: make(map[int64]int), sentAt: make(map[int64]int64),
	}
}

// readLoop 读取连接直到出错或 ctx 结束。
func (s *session) readLoop(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		readCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		_, data, err := s.conn.Read(readCtx)
		cancel()
		if err != nil {
			return err
		}
		s.handleIncoming(data)
	}
}

type incomingAction struct {
	Action string          `json:"action"`
	Params json.RawMessage `json:"params"`
	Echo   string          `json:"echo"`
}

// flexInt 兼容 OneBot 实现把 ID 序列化为字符串或数字两种形式。
type flexInt int64

func (f *flexInt) UnmarshalJSON(data []byte) error {
	text := strings.Trim(string(data), `"`)
	if text == "" || text == "null" {
		*f = 0
		return nil
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		*f = 0
		return nil
	}
	*f = flexInt(value)
	return nil
}

type actionParams struct {
	MessageType string            `json:"message_type"`
	UserID      flexInt           `json:"user_id"`
	GroupID     flexInt           `json:"group_id"`
	Message     []protocolSegment `json:"message"`
}

type protocolSegment struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data"`
}

func (s *session) handleIncoming(data []byte) {
	var request incomingAction
	if err := json.Unmarshal(data, &request); err != nil || request.Echo == "" || request.Action == "" {
		// 被测机器人不会主动推送事件给负载器；忽略无法解析的数据帧。
		s.log.Debug("忽略数据帧", "bytes", len(data))
		return
	}

	payload := s.respond(request)
	if payload == nil {
		return
	}
	if err := s.write(payload); err != nil {
		s.mu.Lock()
		s.actionErrs++
		s.mu.Unlock()
		s.log.Warn("发送动作应答失败", "error", err)
	}
}

// respond 生成动作应答负载并更新配对计数；payload 为 nil 表示无需应答。
func (s *session) respond(request incomingAction) []byte {
	s.mu.Lock()
	s.actionCount++
	s.mu.Unlock()

	response := map[string]any{
		"status":  "ok",
		"retcode": 0,
		"echo":    request.Echo,
	}
	switch request.Action {
	case "send_msg", "send_private_msg", "send_group_msg":
		s.mu.Lock()
		s.msgID++
		messageID := s.msgID
		s.mu.Unlock()
		response["data"] = map[string]any{"message_id": messageID}

		var params actionParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			s.mu.Lock()
			s.actionErrs++
			s.mu.Unlock()
			s.log.Warn("回复动作参数无法解析", "error", err)
			break
		}
		text := ""
		for _, segment := range params.Message {
			if segment.Type == "text" {
				if value, ok := segment.Data["text"].(string); ok {
					text += value
				}
			}
		}
		s.recordReply(text, time.Now())
	case "get_login_info":
		response["data"] = map[string]any{"user_id": s.cfg.SelfID, "nickname": "bench-impl"}
	case "get_guild_service_profile":
		response["data"] = map[string]any{}
	case "get_online_clients":
		response["data"] = map[string]any{}
		response["clients"] = []any{}
	case "get_version_info":
		response["data"] = map[string]any{
			"app_name": "bench-impl", "app_version": "1.0.0", "protocol_version": "v11",
		}
	case "get_cookies":
		response["data"] = map[string]any{}
		response["cookies"] = ""
	case "get_csrf_token":
		response["data"] = map[string]any{}
		response["token"] = 0
	case "get_friend_list":
		response["data"] = []any{}
	case "get_group_list":
		response["data"] = []any{map[string]any{
			"group_id": s.cfg.GroupID, "group_name": "bench-group",
			"member_count": 2, "max_member_count": 200,
		}}
	case "get_group_member_list":
		response["data"] = []any{
			map[string]any{"user_id": s.cfg.SelfID, "nickname": "bench-impl", "card": "bench-impl", "role": "owner"},
			map[string]any{"user_id": s.cfg.UserID, "nickname": "bench-user", "card": "bench-user", "role": "member"},
		}
	default:
		response["data"] = map[string]any{}
	}

	payload, err := json.Marshal(response)
	if err != nil {
		s.mu.Lock()
		s.actionErrs++
		s.mu.Unlock()
		s.log.Error("编码动作应答失败", "error", err)
		return nil
	}
	return payload
}

// recordReply 用回复文本配对延迟；带序号的回复可精确配对，固定文本回复忽略。
func (s *session) recordReply(text string, at time.Time) {
	seq := extractSequence(text)
	if seq == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index, ok := s.sentIndex[seq]
	if !ok {
		s.actionErrs++
		s.log.Warn("收到未推送序号的回复", "seq", seq)
		return
	}
	if s.events[index].ReplyNS != 0 {
		s.actionErrs++
		s.log.Warn("同一序号收到重复回复", "seq", seq)
		return
	}
	start, ok := s.sentAt[seq]
	var latency time.Duration
	if ok {
		latency = clockDuration(start, clockNow())
	} else {
		// 兼容直接构造 EventRecord 的调用方和旧结果回放；push 路径总是写入
		// 高精度计时起点。
		latency = time.Duration(at.UnixNano() - s.events[index].SentNS)
	}
	if latency < 0 {
		latency = 0
	}
	delete(s.sentAt, seq)
	s.events[index].ReplyNS = at.UnixNano()
	s.events[index].LatencyMS = float64(latency) / 1e6
}

func (s *session) write(payload []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	writeCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return s.conn.Write(writeCtx, websocket.MessageText, payload)
}

// push 发送一条私聊消息事件并记录测量起点。基准 v1 只用私聊：旧版群消息
// 带 groupCD/singleCD/1 秒文本去重，而 Go 版尚未实现这些功能，混入会变成
// 功能差异对比而不是性能对比。
func (s *session) push(ctx context.Context, spec messageSpec, index int) error {
	at := time.Now()
	s.mu.Lock()
	s.msgID++
	record := EventRecord{
		Index: index, Seq: spec.Seq, Text: spec.Text, SentNS: at.UnixNano(),
		MessageType: "private",
	}
	s.events = append(s.events, record)
	eventIndex := len(s.events) - 1
	messageID := s.msgID
	if spec.Seq > 0 {
		s.sentIndex[spec.Seq] = eventIndex
		s.sentAt[spec.Seq] = clockNow()
	}
	s.mu.Unlock()

	event := map[string]any{
		"time":         at.Unix(),
		"self_id":      s.cfg.SelfID,
		"post_type":    "message",
		"message_type": "private",
		"message_id":   messageID,
		"user_id":      s.cfg.UserID,
		"sub_type":     "friend",
		"message": []map[string]any{{
			"type": "text",
			"data": map[string]any{"text": spec.Text},
		}},
		"raw_message": spec.Text,
		"sender": map[string]any{
			"user_id": s.cfg.UserID, "nickname": "bench-user",
			"sex": "unknown", "age": 18,
		},
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("编码事件失败: %w", err)
	}
	return s.write(payload)
}

// events 返回测量记录的拷贝。
func (s *session) eventRecords() []EventRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]EventRecord(nil), s.events...)
}

func (s *session) counters() (int, int, int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.actionCount, s.actionErrs, s.msgID
}
