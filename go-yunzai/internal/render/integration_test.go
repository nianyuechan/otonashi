package render

import (
	"context"
	"crypto/sha256"
	"os"
	"testing"
	"time"
)

func TestRenderIntegration(t *testing.T) {
	baseURL := os.Getenv("RENDER_INTEGRATION_URL")
	if baseURL == "" {
		t.Skip("未设置 RENDER_INTEGRATION_URL，跳过真实渲染联调")
	}
	client, err := NewClient(Options{
		URL:           baseURL,
		Timeout:       20 * time.Second,
		MaxRetries:    2,
		RetryBackoff:  100 * time.Millisecond,
		MaxConcurrent: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	request := Request{
		HTML: "<!doctype html><html><body><div id=\"ready\">go-yunzai + go-render</div></body></html>",
		Options: RenderOptions{
			Width:             320,
			Height:            200,
			DeviceScaleFactor: 2,
			TimeoutMS:         15000,
			WaitUntil:         "network_idle",
			WaitSelector:      "#ready",
		},
	}
	started := time.Now()
	image, err := client.Render(context.Background(), request)
	if err != nil {
		t.Fatalf("真实渲染联调失败: %v", err)
	}
	elapsed := time.Since(started)
	if image.Width != 640 || image.Height != 400 || len(image.Data) == 0 {
		t.Fatalf("unexpected real image: width=%d height=%d bytes=%d", image.Width, image.Height, len(image.Data))
	}
	t.Logf("real render succeeded: elapsed=%s elapsed_ms=%d bytes=%d dimensions=%dx%d sha256=%X", elapsed, elapsed.Milliseconds(), len(image.Data), image.Width, image.Height, sha256.Sum256(image.Data))
}
