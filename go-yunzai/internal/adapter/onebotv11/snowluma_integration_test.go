package onebotv11

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type synchronizedBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (b *synchronizedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(data)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func TestSnowLumaHandshake(t *testing.T) {
	baseURL := os.Getenv("YUNZAI_TEST_ONEBOT_URL")
	token := os.Getenv("YUNZAI_TEST_ONEBOT_TOKEN")
	if baseURL == "" || token == "" {
		t.Skip("未设置真实 OneBot 联调环境变量，跳过握手测试")
	}

	var logs synchronizedBuffer
	client, err := NewClient(Options{
		URL:               baseURL,
		AccessToken:       token,
		Handler:           noopDispatcher{},
		Logger:            slog.New(slog.NewTextHandler(io.Writer(&logs), nil)),
		RequestTimeout:    3 * time.Second,
		HeartbeatInterval: time.Second,
		ReconnectInitial:  30 * time.Second,
		ReconnectMax:      30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := client.Run(ctx); err != nil {
			t.Errorf("Client.Run() error = %v", err)
		}
	}()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("客户端未在期限内关闭")
		}
	}()

	deadline := time.Now().Add(6 * time.Second)
	for {
		client.mu.Lock()
		connected := client.conn != nil
		client.mu.Unlock()
		if connected {
			t.Log("真实 SnowLuma WS 握手成功：url=ws://127.0.0.1:3001/ authorization=Bearer *** message_format=array")
			return
		}
		if strings.Contains(logs.String(), "OneBot dial failed") {
			t.Fatalf("真实握手失败，首次拨号日志（已脱敏）：%s", redactSecret(logs.String(), token))
		}
		if time.Now().After(deadline) {
			t.Fatalf("真实握手等待超时，日志（已脱敏）：%s", redactSecret(logs.String(), token))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func redactSecret(value, secret string) string {
	value = strings.ReplaceAll(value, secret, "sk_tr_****")
	return strings.ReplaceAll(value, "Bearer sk_tr_****", "Bearer ***")
}
