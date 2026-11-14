package onebotv11

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/trss-yunzai/go-yunzai/internal/event"
	"github.com/trss-yunzai/go-yunzai/internal/message"
)

var (
	ErrNotConnected     = errors.New("onebot: not connected")
	ErrConnectionClosed = errors.New("onebot: connection closed")
)

type Dispatcher interface {
	Dispatch(context.Context, *event.Event)
}

type Options struct {
	URL               string
	AccessToken       string
	Handler           Dispatcher
	Logger            *slog.Logger
	RequestTimeout    time.Duration
	HeartbeatInterval time.Duration
	ReconnectInitial  time.Duration
	ReconnectMax      time.Duration
}

type Client struct {
	opts Options

	mu       sync.Mutex
	conn     *websocket.Conn
	pending  map[string]chan responseResult
	echo     atomic.Uint64
	eventCtx context.Context
}

type wireResponse struct {
	Status  string          `json:"status"`
	Retcode *int64          `json:"retcode"`
	Data    json.RawMessage `json:"data"`
	Echo    string          `json:"echo"`
	Message string          `json:"message"`
	Wording string          `json:"wording"`
}

type responseResult struct {
	response wireResponse
	err      error
}

type APIError struct {
	Status  string
	Retcode int64
	Message string
	Wording string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("onebot API failed: retcode=%d status=%s message=%s wording=%s", e.Retcode, e.Status, e.Message, e.Wording)
}

func NewClient(opts Options) (*Client, error) {
	if opts.URL == "" {
		return nil, fmt.Errorf("onebot URL is required")
	}
	if opts.AccessToken == "" {
		return nil, fmt.Errorf("onebot access token is required")
	}
	if opts.Handler == nil {
		return nil, fmt.Errorf("onebot event handler is required")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.RequestTimeout == 0 {
		opts.RequestTimeout = 10 * time.Second
	}
	if opts.HeartbeatInterval == 0 {
		opts.HeartbeatInterval = 20 * time.Second
	}
	if opts.ReconnectInitial == 0 {
		opts.ReconnectInitial = 500 * time.Millisecond
	}
	if opts.ReconnectMax == 0 {
		opts.ReconnectMax = 30 * time.Second
	}
	if opts.RequestTimeout < 0 || opts.HeartbeatInterval < 0 || opts.ReconnectInitial < 0 || opts.ReconnectMax < opts.ReconnectInitial {
		return nil, fmt.Errorf("invalid onebot timeouts")
	}
	return &Client{opts: opts, pending: make(map[string]chan responseResult)}, nil
}

// Run 维持唯一的 Universal 正向 WS 连接，直到 ctx 结束。
func (c *Client) Run(ctx context.Context) error {
	eventCtx, eventCancel := context.WithCancel(ctx)
	defer eventCancel()
	c.mu.Lock()
	c.eventCtx = eventCtx
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		if c.eventCtx == eventCtx {
			c.eventCtx = nil
		}
		c.mu.Unlock()
	}()
	delay := c.opts.ReconnectInitial
	for {
		if ctx.Err() != nil {
			return nil
		}

		header := make(http.Header)
		header.Set("Authorization", "Bearer "+c.opts.AccessToken)
		dialCtx, cancel := context.WithTimeout(ctx, c.opts.RequestTimeout)
		conn, _, err := websocket.Dial(dialCtx, c.opts.URL, &websocket.DialOptions{HTTPHeader: header})
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			c.opts.Logger.Warn("OneBot dial failed", "error", err, "retry_in", delay)
			if sleep(ctx, delay) != nil {
				return nil
			}
			delay = nextBackoff(delay, c.opts.ReconnectMax)
			continue
		}

		c.opts.Logger.Info("OneBot connected", "url", redactURL(c.opts.URL))
		c.attach(conn)
		connectedAt := time.Now()
		err = c.serve(ctx, conn)
		c.detach(conn)
		if time.Since(connectedAt) >= c.opts.ReconnectMax {
			delay = c.opts.ReconnectInitial
		}
		if ctx.Err() != nil {
			conn.Close(websocket.StatusNormalClosure, "shutdown")
			return nil
		}

		c.opts.Logger.Warn("OneBot connection lost", "error", err, "retry_in", delay)
		if sleep(ctx, delay) != nil {
			return nil
		}
		delay = nextBackoff(delay, c.opts.ReconnectMax)
	}
}

func (c *Client) attach(conn *websocket.Conn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.CloseNow()
	}
	c.conn = conn
}

func (c *Client) detach(conn *websocket.Conn) {
	c.mu.Lock()
	if c.conn == conn {
		c.conn = nil
	}
	pending := c.pending
	c.pending = make(map[string]chan responseResult)
	c.mu.Unlock()

	conn.CloseNow()
	for _, ch := range pending {
		ch <- responseResult{err: ErrConnectionClosed}
	}
}

func (c *Client) serve(ctx context.Context, conn *websocket.Conn) error {
	sessionCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		c.pingLoop(sessionCtx, conn)
	}()

	err := c.readLoop(sessionCtx, conn)
	cancel()
	wg.Wait()
	return err
}

func (c *Client) pingLoop(ctx context.Context, conn *websocket.Conn) {
	ticker := time.NewTicker(c.opts.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, c.opts.RequestTimeout)
			err := conn.Ping(pingCtx)
			cancel()
			if err != nil {
				conn.CloseNow()
				return
			}
		}
	}
}

func (c *Client) readLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}

		var response wireResponse
		if err := json.Unmarshal(data, &response); err == nil && response.Echo != "" && response.Retcode != nil {
			c.completeResponse(response)
			continue
		}

		evt, err := DecodeEvent(data)
		if err != nil {
			c.opts.Logger.Warn("ignore malformed OneBot event", "error", err)
			continue
		}
		c.opts.Handler.Dispatch(c.eventContext(ctx), evt)
	}
}

func (c *Client) eventContext(fallback context.Context) context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.eventCtx != nil {
		return c.eventCtx
	}
	return fallback
}

func (c *Client) completeResponse(response wireResponse) {
	c.mu.Lock()
	ch, ok := c.pending[response.Echo]
	if ok {
		delete(c.pending, response.Echo)
	}
	c.mu.Unlock()
	if ok {
		ch <- responseResult{response: response}
	}
}

type sendMessageParams struct {
	MessageType string            `json:"message_type"`
	UserID      int64             `json:"user_id,omitempty"`
	GroupID     int64             `json:"group_id,omitempty"`
	Message     []message.Segment `json:"message"`
}

type actionRequest struct {
	Action string            `json:"action"`
	Params sendMessageParams `json:"params"`
	Echo   string            `json:"echo"`
}

func (c *Client) SendMessage(ctx context.Context, outgoing message.Outgoing) (message.Result, error) {
	switch outgoing.MessageType {
	case "private":
		if outgoing.UserID == 0 {
			return message.Result{}, fmt.Errorf("private message requires user_id")
		}
	case "group":
		if outgoing.GroupID == 0 {
			return message.Result{}, fmt.Errorf("group message requires group_id")
		}
	default:
		return message.Result{}, fmt.Errorf("unsupported message_type %q", outgoing.MessageType)
	}

	echo := fmt.Sprintf("m1-%d", c.echo.Add(1))
	request := actionRequest{
		Action: "send_msg",
		Params: sendMessageParams{
			MessageType: outgoing.MessageType,
			UserID:      outgoing.UserID,
			GroupID:     outgoing.GroupID,
			Message:     outgoing.Message,
		},
		Echo: echo,
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return message.Result{}, fmt.Errorf("encode send_msg: %w", err)
	}

	ch := make(chan responseResult, 1)
	c.mu.Lock()
	conn := c.conn
	if conn == nil {
		c.mu.Unlock()
		return message.Result{}, ErrNotConnected
	}
	c.pending[echo] = ch
	c.mu.Unlock()

	requestCtx, cancel := context.WithTimeout(ctx, c.opts.RequestTimeout)
	defer cancel()
	if err := conn.Write(requestCtx, websocket.MessageText, payload); err != nil {
		c.removePending(echo, ch)
		conn.CloseNow()
		return message.Result{}, fmt.Errorf("write send_msg: %w", err)
	}

	select {
	case <-requestCtx.Done():
		c.removePending(echo, ch)
		return message.Result{}, requestCtx.Err()
	case result := <-ch:
		if result.err != nil {
			return message.Result{}, result.err
		}
		response := result.response
		if *response.Retcode != 0 {
			return message.Result{}, &APIError{
				Status:  response.Status,
				Retcode: *response.Retcode,
				Message: response.Message,
				Wording: response.Wording,
			}
		}
		var data struct {
			MessageID int64 `json:"message_id"`
		}
		if len(response.Data) != 0 {
			if err := json.Unmarshal(response.Data, &data); err != nil {
				return message.Result{}, fmt.Errorf("decode send_msg response: %w", err)
			}
		}
		return message.Result{MessageID: data.MessageID}, nil
	}
}

func (c *Client) removePending(echo string, expected chan responseResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ch, ok := c.pending[echo]; ok && ch == expected {
		delete(c.pending, echo)
	}
}

func sleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func nextBackoff(current, max time.Duration) time.Duration {
	if current >= max || current > max/2 {
		return max
	}
	return current * 2
}

// redactURL keeps only scheme, host and path so credentials in userinfo or
// query parameters never reach logs.
func redactURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "<invalid-url>"
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}
