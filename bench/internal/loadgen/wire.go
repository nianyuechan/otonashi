package loadgen

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

// lifecycleEvent 构造旧版适配器 connect() 需要的 meta 事件。
func lifecycleEvent(cfg Config) []byte {
	payload, _ := json.Marshal(map[string]any{
		"time":            time.Now().Unix(),
		"self_id":         cfg.SelfID,
		"post_type":       "meta_event",
		"meta_event_type": "lifecycle",
		"sub_type":        "connect",
	})
	return payload
}

// listenAccept 在 server 模式下监听并接受一条连接（go-yunzai 正向客户端）。
func listenAccept(ctx context.Context, cfg Config, log *slog.Logger) (*websocket.Conn, error) {
	listener, err := newListener(ctx, cfg.Listen)
	if err != nil {
		return nil, err
	}
	defer listener.close()
	log.Info("等待被测机器人连入", "listen", cfg.Listen, "deadline", "120s")
	acceptCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	conn, err := listener.accept(acceptCtx)
	if err != nil {
		return nil, err
	}
	log.Info("被测机器人已连入")
	return conn, nil
}

// listener 用 net/http + coder/websocket 完成一次升级握手。
type listener struct {
	server   *http.Server
	accepted chan *websocket.Conn
	errors   chan error
}

func newListener(ctx context.Context, addr string) (*listener, error) {
	l := &listener{
		accepted: make(chan *websocket.Conn, 1),
		errors:   make(chan error, 2),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			select {
			case l.errors <- err:
			default:
			}
			return
		}
		select {
		case l.accepted <- conn:
		default:
			conn.CloseNow()
		}
	})
	l.server = &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	httpErr := make(chan error, 1)
	go func() { httpErr <- l.server.ListenAndServe() }()
	select {
	case err := <-httpErr:
		return nil, fmt.Errorf("监听 %s 失败: %w", addr, err)
	case <-time.After(300 * time.Millisecond):
	}
	return l, nil
}

func (l *listener) accept(ctx context.Context) (*websocket.Conn, error) {
	select {
	case conn := <-l.accepted:
		return conn, nil
	case err := <-l.errors:
		return nil, fmt.Errorf("升级握手失败: %w", err)
	case <-ctx.Done():
		return nil, fmt.Errorf("等待被测机器人连入超时: %w", ctx.Err())
	}
}

func (l *listener) close() {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = l.server.Shutdown(shutdownCtx)
	_ = l.server.Close()
}
