package renderer

import (
	"bytes"
	"context"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"go-render/pkg/render/api"
)

func TestChromedpSmoke(t *testing.T) {
	executable := os.Getenv("RENDER_SMOKE_CHROMIUM")
	if executable == "" {
		t.Skip("set RENDER_SMOKE_CHROMIUM to run the real Chromium smoke test")
	}
	registry := prometheus.NewRegistry()
	service := NewService(func(ctx context.Context) (Browser, error) {
		return NewChromedpBrowser(ctx, executable)
	}, Config{Concurrency: 2, QueueSize: 2, BrowserStartTimeout: 20 * time.Second}, testLogger(), NewMetrics(registry))
	startCtx, cancelStart := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancelStart()
	if err := service.Start(startCtx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := service.Close(ctx); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	}()

	req := api.Request{
		HTML: `<!doctype html><html><head><style>html,body{margin:0;background:#eef7ff}#ready{width:320px;height:200px;box-sizing:border-box;padding:24px;font:24px sans-serif;color:#123}</style></head><body><div id="ready">Go + Chromium</div></body></html>`,
		Options: api.Options{
			Width:             320,
			Height:            200,
			DeviceScaleFactor: 2,
			TimeoutMS:         15000,
			WaitUntil:         api.WaitNetworkIdle,
			WaitSelector:      "#ready",
		},
	}
	if err := req.Validate(20 * time.Second); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	start := time.Now()
	image, err := service.Render(context.Background(), req)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	elapsed := time.Since(start)
	decoded, err := png.DecodeConfig(bytes.NewReader(image))
	if err != nil {
		t.Fatalf("rendered image is not PNG: %v", err)
	}
	if decoded.Width != 640 || decoded.Height != 400 {
		t.Fatalf("image dimensions = %dx%d, want 640x400", decoded.Width, decoded.Height)
	}
	t.Logf("smoke render succeeded: duration=%s bytes=%d dimensions=%dx%d", elapsed, len(image), decoded.Width, decoded.Height)
}
