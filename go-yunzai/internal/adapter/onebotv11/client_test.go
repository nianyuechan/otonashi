package onebotv11

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/trss-yunzai/go-yunzai/internal/dispatch"
	"github.com/trss-yunzai/go-yunzai/internal/event"
	"github.com/trss-yunzai/go-yunzai/internal/message"
	"github.com/trss-yunzai/go-yunzai/internal/render"
	"github.com/trss-yunzai/go-yunzai/plugins/builtin"
)

type fakeAction struct {
	Action string `json:"action"`
	Params struct {
		MessageType string            `json:"message_type"`
		UserID      int64             `json:"user_id"`
		GroupID     int64             `json:"group_id"`
		Message     []message.Segment `json:"message"`
	} `json:"params"`
	Echo string `json:"echo"`
}

type fakeOneBotServer struct {
	t     *testing.T
	token string

	server       *httptest.Server
	connected    chan int64
	unauthorized chan struct{}
	actions      chan fakeAction

	connections      atomic.Int64
	nextMessageID    atomic.Int64
	nextRetcode      atomic.Int64
	suppressResponse atomic.Bool
	dropFirstAction  atomic.Bool

	mu      sync.Mutex
	current *websocket.Conn
}

func newFakeOneBotServer(t *testing.T, token string) *fakeOneBotServer {
	t.Helper()
	fake := &fakeOneBotServer{
		t:            t,
		token:        token,
		connected:    make(chan int64, 16),
		unauthorized: make(chan struct{}, 16),
		actions:      make(chan fakeAction, 32),
	}
	fake.nextMessageID.Store(70000)
	fake.server = httptest.NewServer(http.HandlerFunc(fake.serveHTTP))
	t.Cleanup(fake.close)
	return fake
}

func (s *fakeOneBotServer) url() string {
	return "ws" + strings.TrimPrefix(s.server.URL, "http")
}

func (s *fakeOneBotServer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+s.token {
		select {
		case s.unauthorized <- struct{}{}:
		default:
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		s.t.Errorf("websocket.Accept() error = %v", err)
		return
	}
	connectionNumber := s.connections.Add(1)
	s.mu.Lock()
	s.current = conn
	s.mu.Unlock()
	s.connected <- connectionNumber
	defer func() {
		s.mu.Lock()
		if s.current == conn {
			s.current = nil
		}
		s.mu.Unlock()
		conn.CloseNow()
	}()

	for {
		_, data, err := conn.Read(context.Background())
		if err != nil {
			return
		}
		var action fakeAction
		if err := json.Unmarshal(data, &action); err != nil {
			s.t.Errorf("decode client action: %v", err)
			continue
		}
		s.actions <- action
		if s.dropFirstAction.CompareAndSwap(true, false) {
			conn.CloseNow()
			return
		}
		if s.suppressResponse.Load() {
			continue
		}

		retcode := s.nextRetcode.Load()
		status := "ok"
		if retcode != 0 {
			status = "failed"
		}
		response := map[string]any{
			"status":  status,
			"retcode": retcode,
			"data":    map[string]any{"message_id": s.nextMessageID.Add(1)},
			"echo":    action.Echo,
			"message": "synthetic response",
			"wording": "synthetic response",
		}
		if err := writeWSJSON(conn, response); err != nil {
			return
		}
	}
}

func writeWSJSON(conn *websocket.Conn, value any) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, data)
}

func (s *fakeOneBotServer) sendEvent(t *testing.T, value any) {
	t.Helper()
	s.mu.Lock()
	conn := s.current
	s.mu.Unlock()
	if conn == nil {
		t.Fatal("fake server has no active connection")
	}
	if err := writeWSJSON(conn, value); err != nil {
		t.Fatalf("send fake event: %v", err)
	}
}

func (s *fakeOneBotServer) close() {
	s.mu.Lock()
	conn := s.current
	s.mu.Unlock()
	if conn != nil {
		conn.CloseNow()
	}
	s.server.Close()
}

type noopDispatcher struct{}

func (noopDispatcher) Dispatch(context.Context, *event.Event) {}

type fakeRenderService struct {
	outcome render.Outcome
}

func (f fakeRenderService) RenderWithFallback(context.Context, render.Request) render.Outcome {
	return f.outcome
}

func newTestClient(t *testing.T, url, token string, handler Dispatcher) *Client {
	t.Helper()
	client, err := NewClient(Options{
		URL:               url,
		AccessToken:       token,
		Handler:           handler,
		Logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		RequestTimeout:    150 * time.Millisecond,
		HeartbeatInterval: 25 * time.Millisecond,
		ReconnectInitial:  20 * time.Millisecond,
		ReconnectMax:      80 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}

func runClient(t *testing.T, client *Client) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := client.Run(ctx); err != nil {
			t.Errorf("Client.Run() error = %v", err)
		}
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("Client.Run() did not stop")
		}
	})
	return cancel
}

func waitConnection(t *testing.T, fake *fakeOneBotServer, want int64) {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case got := <-fake.connected:
			if got == want {
				return
			}
		case <-timer.C:
			t.Fatalf("connection %d not established", want)
		}
	}
}

func waitClientReady(t *testing.T, fake *fakeOneBotServer, client *Client, connection int64) {
	t.Helper()
	waitConnection(t, fake, connection)
	deadline := time.Now().Add(time.Second)
	for {
		client.mu.Lock()
		ready := client.conn != nil
		client.mu.Unlock()
		if ready {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("client has not registered the accepted connection")
		}
		time.Sleep(time.Millisecond)
	}
}

func receiveAction(t *testing.T, fake *fakeOneBotServer) fakeAction {
	t.Helper()
	select {
	case action := <-fake.actions:
		return action
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for send_msg")
		return fakeAction{}
	}
}

func TestOfflineMessageRoundTrip(t *testing.T) {
	fake := newFakeOneBotServer(t, "round-trip-token")
	dispatcher := dispatch.New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	client := newTestClient(t, fake.url(), "round-trip-token", dispatcher)
	const reply = "离线闭环回复"
	dispatcher.Register(builtin.NewDebugHandler(client, reply))
	runClient(t, client)
	waitClientReady(t, fake, client, 1)

	fake.sendEvent(t, map[string]any{
		"time":         1,
		"self_id":      9000,
		"post_type":    "message",
		"message_type": "private",
		"sub_type":     "friend",
		"message_id":   101,
		"user_id":      1001,
		"message": []map[string]any{
			{"type": "text", "data": map[string]any{"text": "hello"}},
			{"type": "unknown_future_segment", "data": map[string]any{"value": "preserved"}},
		},
		"sender":        map[string]any{"user_id": 1001, "nickname": "tester"},
		"unknown_field": "tolerated",
	})
	private := receiveAction(t, fake)
	if private.Action != "send_msg" || private.Echo == "" {
		t.Fatalf("unexpected private action: %+v", private)
	}
	if private.Params.MessageType != "private" || private.Params.UserID != 1001 || private.Params.GroupID != 0 {
		t.Fatalf("unexpected private target: %+v", private.Params)
	}
	assertDebugReply(t, private, reply)

	fake.sendEvent(t, map[string]any{
		"post_type":    "message",
		"message_type": "group",
		"sub_type":     "normal",
		"message_id":   102,
		"self_id":      9000,
		"user_id":      1001,
		"group_id":     2001,
		"message":      []map[string]any{{"type": "text", "data": map[string]any{"text": "group hello"}}},
	})
	group := receiveAction(t, fake)
	if group.Params.MessageType != "group" || group.Params.GroupID != 2001 {
		t.Fatalf("unexpected group target: %+v", group.Params)
	}
	assertDebugReply(t, group, reply)

	fake.sendEvent(t, map[string]any{"post_type": "notice", "notice_type": "notify", "sub_type": "poke", "self_id": 9000, "user_id": 1001, "target_id": 9000})
	fake.sendEvent(t, map[string]any{"post_type": "request", "request_type": "friend", "user_id": 1001, "flag": "synthetic"})
	fake.sendEvent(t, map[string]any{"post_type": "meta_event", "meta_event_type": "heartbeat", "self_id": 9000, "status": map[string]any{"online": true}})
	select {
	case extra := <-fake.actions:
		t.Fatalf("non-message event produced an extra reply: %+v", extra)
	case <-time.After(80 * time.Millisecond):
	}
}

func TestOfflineRenderImageRoundTrip(t *testing.T) {
	fake := newFakeOneBotServer(t, "render-round-trip-token")
	dispatcher := dispatch.New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	client := newTestClient(t, fake.url(), "render-round-trip-token", dispatcher)
	pngData := makeTestPNG(t)
	dispatcher.Register(builtin.NewDebugHandlerWithRenderer(client, fakeRenderService{
		outcome: render.Outcome{Image: &render.Image{Data: pngData, Width: 1, Height: 1}},
	}, "固定回复"))
	runClient(t, client)
	waitClientReady(t, fake, client, 1)

	fake.sendEvent(t, map[string]any{
		"post_type":    "message",
		"message_type": "private",
		"user_id":      1001,
		"message":      []map[string]any{{"type": "text", "data": map[string]any{"text": builtin.RenderCommand}}},
	})
	action := receiveAction(t, fake)
	if action.Action != "send_msg" || action.Params.MessageType != "private" || action.Params.UserID != 1001 {
		t.Fatalf("unexpected render action: %+v", action)
	}
	if len(action.Params.Message) != 1 || action.Params.Message[0].Type != "image" {
		t.Fatalf("unexpected render message: %+v", action.Params.Message)
	}
	file, ok := action.Params.Message[0].Data["file"].(string)
	if !ok || !strings.HasPrefix(file, "base64://") {
		t.Fatalf("unexpected image file: %#v", action.Params.Message[0].Data["file"])
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(file, "base64://"))
	if err != nil {
		t.Fatalf("decode image file: %v", err)
	}
	if !bytes.Equal(decoded, pngData) {
		t.Fatalf("decoded image differs: got %d bytes, want %d", len(decoded), len(pngData))
	}
}

func makeTestPNG(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 47, G: 111, B: 235, A: 255})
	if err := png.Encode(&data, img); err != nil {
		t.Fatalf("png.Encode() error = %v", err)
	}
	return data.Bytes()
}

func assertDebugReply(t *testing.T, action fakeAction, want string) {
	t.Helper()
	if len(action.Params.Message) != 1 || action.Params.Message[0].Type != "text" || action.Params.Message[0].Data["text"] != want {
		t.Fatalf("unexpected reply segments: %+v", action.Params.Message)
	}
}

func TestSendMessageDecodesEchoResponseAndRetcode(t *testing.T) {
	fake := newFakeOneBotServer(t, "response-token")
	client := newTestClient(t, fake.url(), "response-token", noopDispatcher{})
	runClient(t, client)
	waitClientReady(t, fake, client, 1)

	result, err := client.SendMessage(context.Background(), message.Outgoing{
		MessageType: "group",
		GroupID:     2001,
		Message:     []message.Segment{message.Text("direct")},
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	if result.MessageID <= 70000 {
		t.Fatalf("synthetic message_id not decoded: %d", result.MessageID)
	}
	action := receiveAction(t, fake)
	if action.Echo != "m1-1" {
		t.Fatalf("unexpected echo %q", action.Echo)
	}

	fake.nextRetcode.Store(1404)
	_, err = client.SendMessage(context.Background(), message.Outgoing{
		MessageType: "private",
		UserID:      1001,
		Message:     []message.Segment{message.Text("failure")},
	})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Retcode != 1404 {
		t.Fatalf("SendMessage() error = %v, want APIError retcode 1404", err)
	}
	receiveAction(t, fake)
}

func TestRequestTimeoutCleansPendingEcho(t *testing.T) {
	fake := newFakeOneBotServer(t, "timeout-token")
	fake.suppressResponse.Store(true)
	client := newTestClient(t, fake.url(), "timeout-token", noopDispatcher{})
	client.opts.RequestTimeout = 50 * time.Millisecond
	runClient(t, client)
	waitClientReady(t, fake, client, 1)

	_, err := client.SendMessage(context.Background(), message.Outgoing{
		MessageType: "private",
		UserID:      1001,
		Message:     []message.Segment{message.Text("timeout")},
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("SendMessage() error = %v, want deadline exceeded", err)
	}
	receiveAction(t, fake)
	waitPendingCount(t, client, 0)
}

func TestAbruptDisconnectCleansPendingAndReconnects(t *testing.T) {
	fake := newFakeOneBotServer(t, "reconnect-token")
	fake.dropFirstAction.Store(true)
	client := newTestClient(t, fake.url(), "reconnect-token", noopDispatcher{})
	runClient(t, client)
	waitClientReady(t, fake, client, 1)

	_, err := client.SendMessage(context.Background(), message.Outgoing{
		MessageType: "group",
		GroupID:     2001,
		Message:     []message.Segment{message.Text("drop")},
	})
	if !errors.Is(err, ErrConnectionClosed) {
		t.Fatalf("SendMessage() error = %v, want ErrConnectionClosed", err)
	}
	receiveAction(t, fake)
	waitPendingCount(t, client, 0)
	waitClientReady(t, fake, client, 2)

	result, err := client.SendMessage(context.Background(), message.Outgoing{
		MessageType: "private",
		UserID:      1001,
		Message:     []message.Segment{message.Text("after reconnect")},
	})
	if err != nil {
		t.Fatalf("SendMessage() after reconnect error = %v", err)
	}
	if result.MessageID <= 70000 {
		t.Fatalf("missing response after reconnect: %+v", result)
	}
	receiveAction(t, fake)
}

func waitPendingCount(t *testing.T, client *Client, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		client.mu.Lock()
		got := len(client.pending)
		client.mu.Unlock()
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pending echo count = %d, want %d", got, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestHandshakeRejectsWrongBearer(t *testing.T) {
	fake := newFakeOneBotServer(t, "expected-token")
	client := newTestClient(t, fake.url(), "wrong-token", noopDispatcher{})
	cancel := runClient(t, client)
	select {
	case <-fake.unauthorized:
	case <-time.After(2 * time.Second):
		t.Fatal("fake server did not reject the wrong bearer token")
	}
	cancel()
	if got := fake.connections.Load(); got != 0 {
		t.Fatalf("unauthorized client established %d WS connections", got)
	}
}

func TestNextBackoffIsExponentialAndCapped(t *testing.T) {
	if got := nextBackoff(20*time.Millisecond, 80*time.Millisecond); got != 40*time.Millisecond {
		t.Fatalf("nextBackoff() = %v, want 40ms", got)
	}
	if got := nextBackoff(40*time.Millisecond, 80*time.Millisecond); got != 80*time.Millisecond {
		t.Fatalf("nextBackoff() = %v, want 80ms", got)
	}
	if got := nextBackoff(80*time.Millisecond, 80*time.Millisecond); got != 80*time.Millisecond {
		t.Fatalf("nextBackoff() = %v, want cap 80ms", got)
	}
}

type contextCaptureDispatcher struct {
	started  chan context.Context
	finished chan error
}

func (d *contextCaptureDispatcher) Dispatch(ctx context.Context, _ *event.Event) {
	d.started <- ctx
	time.Sleep(100 * time.Millisecond)
	d.finished <- ctx.Err()
}

func TestDisconnectDoesNotCancelInFlightEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		conn, err := websocket.Accept(w, req, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		eventFrame := map[string]any{
			"post_type": "message", "message_type": "private", "self_id": 10,
			"user_id": 20, "message_id": 1,
			"message": []map[string]any{{"type": "text", "data": map[string]any{"text": "hello"}}},
		}
		data, _ := json.Marshal(eventFrame)
		if err := conn.Write(context.Background(), websocket.MessageText, data); err != nil {
			return
		}
		conn.CloseNow()
	}))
	defer server.Close()

	dispatcher := &contextCaptureDispatcher{started: make(chan context.Context, 1), finished: make(chan error, 1)}
	client, err := NewClient(Options{
		URL: "ws" + strings.TrimPrefix(server.URL, "http"), AccessToken: "test-token",
		Handler: dispatcher, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		RequestTimeout: time.Second, HeartbeatInterval: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = client.Run(ctx) }()
	select {
	case <-dispatcher.started:
	case <-time.After(2 * time.Second):
		t.Fatal("event was not dispatched")
	}
	select {
	case err := <-dispatcher.finished:
		if err != nil {
			t.Fatalf("disconnect canceled the in-flight event: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("dispatched event did not finish")
	}
}

func TestRedactURLRemovesCredentials(t *testing.T) {
	cases := map[string]string{
		"ws://user:pass@127.0.0.1:3001/path?access_token=secret#frag": "ws://127.0.0.1:3001/path",
		"ws://127.0.0.1:3001/?token=secret":                           "ws://127.0.0.1:3001/",
	}
	for raw, want := range cases {
		if got := redactURL(raw); got != want {
			t.Fatalf("redactURL(%q) = %q, want %q", raw, got, want)
		}
	}
}
